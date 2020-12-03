// Copyright Amazon.com, Inc. or its affiliates. All Rights Reserved.
//
// Licensed under the Apache License, Version 2.0 (the "License"). You may
// not use this file except in compliance with the License. A copy of the
// License is located at
//
//	http://aws.amazon.com/apache2.0/
//
// or in the "license" file accompanying this file. This file is distributed
// on an "AS IS" BASIS, WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either
// express or implied. See the License for the specific language governing
// permissions and limitations under the License.

package main

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"net"
	"os"
	"runtime/debug"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	// disable gosec check for math/rand. We just need a random starting
	// place to start looking for CIDs; no need for cryptographically
	// secure randomness
	"math/rand" // #nosec

	"github.com/containerd/containerd/errdefs"
	"github.com/containerd/containerd/events/exchange"
	"github.com/containerd/containerd/log"
	"github.com/containerd/containerd/namespaces"
	"github.com/containerd/containerd/pkg/ttrpcutil"
	"github.com/containerd/containerd/runtime/v2/shim"
	taskAPI "github.com/containerd/containerd/runtime/v2/task"
	"github.com/containerd/fifo"
	"github.com/containerd/ttrpc"
	"github.com/firecracker-microvm/firecracker-go-sdk"
	"github.com/firecracker-microvm/firecracker-go-sdk/client/models"
	"github.com/gofrs/uuid"
	ptypes "github.com/gogo/protobuf/types"
	"github.com/golang/protobuf/ptypes/empty"
	"github.com/hashicorp/go-multierror"
	"github.com/pkg/errors"
	"github.com/sirupsen/logrus"
	"golang.org/x/sys/unix"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/firecracker-microvm/firecracker-containerd/config"
	"github.com/firecracker-microvm/firecracker-containerd/eventbridge"
	"github.com/firecracker-microvm/firecracker-containerd/internal"
	"github.com/firecracker-microvm/firecracker-containerd/internal/bundle"
	fcShim "github.com/firecracker-microvm/firecracker-containerd/internal/shim"
	"github.com/firecracker-microvm/firecracker-containerd/internal/vm"
	"github.com/firecracker-microvm/firecracker-containerd/proto"
	drivemount "github.com/firecracker-microvm/firecracker-containerd/proto/service/drivemount/ttrpc"
	fccontrolTtrpc "github.com/firecracker-microvm/firecracker-containerd/proto/service/fccontrol/ttrpc"
)

func init() {
	rand.Seed(time.Now().UnixNano())
}

const (
	defaultVsockPort = 10789
	minVsockIOPort   = uint32(11000)

	// vmReadyTimeout is used to control the time all requests wait a Go channel (vmReady) before calling
	// Firecracker's API server. The channel is closed once the VM starts.
	vmReadyTimeout = 5 * time.Second

	defaultCreateVMTimeout     = 20 * time.Second
	defaultStopVMTimeout       = 5 * time.Second
	defaultShutdownTimeout     = 5 * time.Second
	defaultVSockConnectTimeout = 5 * time.Second

	jailerStopTimeout = 3 * time.Second

	// StartEventName is the topic published to when a VM starts
	StartEventName = "/firecracker-vm/start"

	// StopEventName is the topic published to when a VM stops
	StopEventName = "/firecracker-vm/stop"
)

var (
	// type assertions
	_ taskAPI.TaskService = &service{}
	_ shim.Init           = NewService
)

// implements shimapi
type service struct {
	taskManager   vm.TaskManager
	eventExchange *exchange.Exchange
	namespace     string

	logger *logrus.Entry

	// Normally, it's ill-advised to store a context object in a struct. However,
	// in this case there appears to be little choice. Containerd provides the
	// context and cancel from their shim setup code to our NewService function.
	// It is expected to live for the lifetime of the shim process and canceled
	// when the shim is shutting down.
	//
	// The problem is that sometimes service methods, such as CreateVM, require
	// a context scoped to the lifetime of the shim process but they are only provided
	// a context scoped to the lifetime of the individual request, not the shimCtx.
	//
	// shimCtx should thus only be used by service methods that need to provide
	// a context that will be canceled only when the shim is shutting down and
	// cleanup should happen.
	//
	// This approach is also taken by containerd's current reference runc shim
	// v2 implementation
	shimCtx    context.Context
	shimCancel func()

	vmID    string
	shimDir vm.Dir

	config *config.Config

	// vmReady is closed once CreateVM has been successfully called
	vmReady                  chan struct{}
	vmStartOnce              sync.Once
	agentClient              taskAPI.TaskService
	eventBridgeClient        eventbridge.Getter
	driveMountClient         drivemount.DriveMounterService
	jailer                   jailer
	containerStubHandler     *StubDriveHandler
	driveMountStubs          []MountableStubDrive
	exitAfterAllTasksDeleted bool // exit the VM and shim when all tasks are deleted

	cleanupErr  error
	cleanupOnce sync.Once

	machine          *firecracker.Machine
	machineConfig    *firecracker.Config
	vsockIOPortCount uint32
	vsockPortMu      sync.Mutex
}

func shimOpts(shimCtx context.Context) (*shim.Opts, error) {
	opts, ok := shimCtx.Value(shim.OptsKey{}).(shim.Opts)
	if !ok {
		return nil, errors.New("failed to parse containerd shim opts from context")
	}

	return &opts, nil
}

// NewService creates new runtime shim.
func NewService(shimCtx context.Context, id string, remotePublisher shim.Publisher, shimCancel func()) (shim.Shim, error) {
	cfg, err := config.LoadConfig("")
	if err != nil {
		return nil, err
	}

	opts, err := shimOpts(shimCtx)
	if err != nil {
		return nil, err
	}

	cfg.DebugHelper.ShimDebug = opts.Debug

	namespace, ok := namespaces.Namespace(shimCtx)
	if !ok {
		namespace = namespaces.Default
	}

	var shimDir vm.Dir
	vmID := os.Getenv(internal.VMIDEnvVarKey)
	logger := log.G(shimCtx)
	if vmID != "" {
		logger = logger.WithField("vmID", vmID)

		shimDir, err = vm.ShimDir(cfg.ShimBaseDir, namespace, vmID)
		if err != nil {
			return nil, errors.Wrap(err, "invalid shim directory")
		}
	}

	logrusLevel, ok := cfg.DebugHelper.GetFirecrackerContainerdLogLevel()
	if ok {
		logrus.SetLevel(logrusLevel)
		logger.Logger.SetLevel(logrusLevel)
	}

	s := &service{
		taskManager:   vm.NewTaskManager(shimCtx, logger),
		eventExchange: exchange.NewExchange(),
		namespace:     namespace,

		logger:     logger,
		shimCtx:    shimCtx,
		shimCancel: shimCancel,

		vmID:    vmID,
		shimDir: shimDir,

		config: cfg,

		vmReady: make(chan struct{}),
		jailer:  newNoopJailer(shimCtx, logger, shimDir),
	}

	s.startEventForwarders(remotePublisher)

	err = s.serveFCControl()
	if err != nil {
		err = errors.Wrap(err, "failed to start fccontrol server")
		s.logger.WithError(err).Error()
		return nil, err
	}

	return s, nil
}

func (s *service) startEventForwarders(remotePublisher shim.Publisher) {
	ns, ok := namespaces.Namespace(s.shimCtx)
	if !ok {
		s.logger.Error("failed to fetch the namespace from the context")
	}
	ctx := namespaces.WithNamespace(context.Background(), ns)

	// Republish each event received on our exchange to the provided remote publisher.
	// TODO ideally we would be forwarding events instead of re-publishing them, which would
	// preserve the events' original timestamps and namespaces. However, as of this writing,
	// the containerd v2 runtime model only provides a shim with a publisher, not a forwarder.
	republishCh := eventbridge.Republish(ctx, s.eventExchange, remotePublisher)

	go func() {
		<-s.vmReady

		// Once the VM is ready, also start forwarding events from it to our exchange
		attachCh := eventbridge.Attach(ctx, s.eventBridgeClient, s.eventExchange)

		err := <-attachCh
		if err != nil && err != context.Canceled {
			s.logger.WithError(err).Error("error while forwarding events from VM agent")
		}

		err = <-republishCh
		if err != nil && err != context.Canceled {
			s.logger.WithError(err).Error("error while republishing events")
		}

		remotePublisher.Close()
	}()
}

// TODO we have to create separate listeners for the fccontrol service and shim service because
// containerd does not currently expose the shim server for us to register the fccontrol service with too.
// This is likely addressable through some relatively small upstream contributions; the following is a stop-gap
// solution until that time.
func (s *service) serveFCControl() error {
	// If the fccontrol socket was configured, setup the fccontrol server
	fcSocketFDEnvVal := os.Getenv(internal.FCSocketFDEnvKey)
	if fcSocketFDEnvVal == "" {
		// if there's no socket, we don't need to serve the API (this must be a shim start or shim delete call)
		return nil
	}

	fcServer, err := ttrpc.NewServer(ttrpc.WithServerHandshaker(ttrpc.UnixSocketRequireSameUser()))
	if err != nil {
		return err
	}

	socketFD, err := strconv.Atoi(fcSocketFDEnvVal)
	if err != nil {
		err = errors.Wrap(err, "failed to parse fccontrol socket FD value")
		s.logger.WithError(err).Error()
		return err
	}

	fccontrolTtrpc.RegisterFirecrackerService(fcServer, s)
	fcListener, err := net.FileListener(os.NewFile(uintptr(socketFD), "fccontrol"))
	if err != nil {
		return err
	}

	go func() {
		defer fcListener.Close()
		err := fcServer.Serve(s.shimCtx, fcListener)
		if err != nil && !strings.Contains(err.Error(), "use of closed network connection") {
			s.logger.WithError(err).Error("fccontrol ttrpc server error")
		}
	}()

	return nil
}

func (s *service) StartShim(shimCtx context.Context, containerID, containerdBinary, containerdAddress, containerdTTRPCAddress string) (string, error) {
	// In the shim start routine, we can assume that containerd provided a "log" FIFO in the current working dir.
	// We have to use that instead of stdout/stderr because containerd reads the stdio pipes of shim start to get
	// either the shim address or the error returned here.
	logFifo, err := fifo.OpenFifo(shimCtx, "log", unix.O_WRONLY, 0200)
	if err != nil {
		return "", err
	}

	logrus.SetOutput(logFifo)

	log := log.G(shimCtx).WithField("task_id", containerID)
	log.Debug("StartShim")

	// If we are running a shim start routine, we can safely assume our current working
	// directory is the bundle directory
	cwd, err := os.Getwd()
	if err != nil {
		return "", errors.Wrap(err, "failed to get current working directory")
	}
	bundleDir := bundle.Dir(cwd)

	// Since we're running a shim start routine, we need to determine the vmID for the incoming
	// container. Start by looking at the container's OCI annotations
	s.vmID, err = bundleDir.OCIConfig().VMID()
	if err != nil {
		return "", err
	}

	var exitAfterAllTasksDeleted bool
	containerCount := 0

	if s.vmID == "" {
		// If here, no VMID has been provided by the client for this container, so auto-generate a new one.
		// This results in a default behavior of running each container in its own VM if not otherwise
		// specified by the client.
		uuid, err := uuid.NewV4()
		if err != nil {
			return "", errors.Wrap(err, "failed to generate UUID for VMID")
		}
		s.vmID = uuid.String()

		// This request is handled by a short-lived shim process to find its control socket.
		// A long-running shim process won't have the request. So, setting s.logger doesn't affect others.
		log = log.WithField("vmID", s.vmID)

		// If the client didn't specify a VMID, this is a single-task VM and should thus exit after this
		// task is deleted
		containerCount = 1
		exitAfterAllTasksDeleted = true
	}

	client, err := ttrpcutil.NewClient(containerdTTRPCAddress)
	if err != nil {
		return "", err
	}

	fcControlClient := fccontrolTtrpc.NewFirecrackerClient(client.Client())

	_, err = fcControlClient.CreateVM(shimCtx, &proto.CreateVMRequest{
		VMID:                     s.vmID,
		ExitAfterAllTasksDeleted: exitAfterAllTasksDeleted,
		ContainerCount:           int32(containerCount),
	})
	if err != nil {
		errStatus, ok := status.FromError(err)
		// ignore AlreadyExists errors, that just means the shim is already up and running
		if !ok || errStatus.Code() != codes.AlreadyExists {
			return "", errors.Wrap(err, "unexpected error from CreateVM")
		}
	}

	// The shim cannot support traditional -version/-v flag because
	// - shim.Run() will call flag.Parse(). So our main cannot call flag.Parse() before that.
	// - -address is required and NewService() won't be called if the flag is missing.
	// So we log the version informaion here instead
	str := ""
	if exitAfterAllTasksDeleted {
		str = " The VM will be torn down after serving a single task."
	}
	log.WithField("vmID", s.vmID).Infof("successfully started shim (git commit: %s).%s", revision, str)

	return fcShim.SocketAddress(shimCtx, s.vmID)
}

func logPanicAndDie(logger *logrus.Entry) {
	if err := recover(); err != nil {
		logger.WithError(err.(error)).Fatalf("panic: %s", string(debug.Stack()))
	}
}

func (s *service) generateExtraData(jsonBytes []byte, options *ptypes.Any) (*proto.ExtraData, error) {
	var opts *ptypes.Any
	if options != nil {
		// Copy values of existing options over
		valCopy := make([]byte, len(options.Value))
		copy(valCopy, options.Value)
		opts = &ptypes.Any{
			TypeUrl: options.TypeUrl,
			Value:   valCopy,
		}
	}

	return &proto.ExtraData{
		JsonSpec:    jsonBytes,
		RuncOptions: opts,
		StdinPort:   s.nextVSockPort(),
		StdoutPort:  s.nextVSockPort(),
		StderrPort:  s.nextVSockPort(),
	}, nil
}

// assumes caller has s.startVMMutex
func (s *service) nextVSockPort() uint32 {
	s.vsockPortMu.Lock()
	defer s.vsockPortMu.Unlock()

	port := minVsockIOPort + s.vsockIOPortCount
	if port == math.MaxUint32 {
		// given we use 3 ports per container, there would need to
		// be about 1431652098 containers spawned in this VM for
		// this to actually happen in practice.
		panic("overflow of vsock ports")
	}

	s.vsockIOPortCount++
	return port
}

func (s *service) waitVMReady() error {
	select {
	case <-s.vmReady:
		return nil
	case <-time.After(vmReadyTimeout):
		return status.Error(codes.DeadlineExceeded, "timed out waiting for VM start")
	}
}

// CreateVM will attempt to create the VM as specified in the provided request, but only on the first request
// received. Any subsequent requests will be ignored and get an AlreadyExists error response.
func (s *service) CreateVM(requestCtx context.Context, request *proto.CreateVMRequest) (*proto.CreateVMResponse, error) {
	defer logPanicAndDie(s.logger)

	timeout := defaultCreateVMTimeout
	if request.TimeoutSeconds > 0 {
		timeout = time.Duration(request.TimeoutSeconds) * time.Second
	}
	ctxWithTimeout, cancel := context.WithTimeout(requestCtx, timeout)
	defer cancel()

	var (
		err       error
		createRan bool
		resp      proto.CreateVMResponse
	)

	s.vmStartOnce.Do(func() {
		err = s.createVM(ctxWithTimeout, request)
		createRan = true
	})
	if !createRan {
		return nil, status.Error(codes.AlreadyExists, "shim cannot create VM more than once")
	}

	// If we failed to create the VM, we have no point in existing anymore, so shutdown
	if err != nil {
		s.shimCancel()

		s.logger.WithError(err).Error("failed to create VM")

		if errors.Cause(err) == context.DeadlineExceeded {
			return nil, status.Errorf(codes.DeadlineExceeded, "VM %q didn't start within %s: %s", request.VMID, timeout, err)
		}
		return nil, errors.Wrap(err, "failed to create VM")
	}

	// creating the VM succeeded, setup monitors and publish events to celebrate
	err = s.publishVMStart()
	if err != nil {
		s.logger.WithError(err).Error("failed to publish start VM event")
	}

	go s.monitorVMExit()

	// let all the other methods know that the VM is ready for tasks
	close(s.vmReady)

	resp.VMID = s.vmID
	resp.MetricsFifoPath = s.machineConfig.MetricsFifo
	resp.LogFifoPath = s.machineConfig.LogFifo
	resp.SocketPath = s.shimDir.FirecrackerSockPath()
	if c, ok := s.jailer.(cgroupPather); ok {
		resp.CgroupPath = c.CgroupPath()
	}

	return &resp, nil
}

func (s *service) publishVMStart() error {
	return s.eventExchange.Publish(s.shimCtx, StartEventName, &proto.VMStart{VMID: s.vmID})
}

func (s *service) publishVMStop() error {
	return s.eventExchange.Publish(s.shimCtx, StopEventName, &proto.VMStop{VMID: s.vmID})
}

func (s *service) createVM(requestCtx context.Context, request *proto.CreateVMRequest) (err error) {
	var vsockFd *os.File
	defer func() {
		if vsockFd != nil {
			vsockFd.Close()
		}
	}()

	namespace, ok := namespaces.Namespace(s.shimCtx)
	if !ok {
		namespace = namespaces.Default
	}

	dir, err := vm.ShimDir(s.config.ShimBaseDir, namespace, s.vmID)
	if err != nil {
		return err
	}

	s.logger.Info("creating new VM")
	s.jailer, err = newJailer(s.shimCtx, s.logger, dir.RootPath(), s, request)
	if err != nil {
		return errors.Wrap(err, "failed to create jailer")
	}

	s.machineConfig, err = s.buildVMConfiguration(request)
	if err != nil {
		return errors.Wrapf(err, "failed to build VM configuration")
	}

	opts := []firecracker.Opt{}

	if v, ok := s.config.DebugHelper.GetFirecrackerSDKLogLevel(); ok {
		logger := log.G(s.shimCtx)
		logger.Logger.SetLevel(v)
		opts = append(opts, firecracker.WithLogger(logger))
	}
	relVSockPath, err := s.jailer.JailPath().FirecrackerVSockRelPath()
	if err != nil {
		return errors.Wrapf(err, "failed to get relative path to firecracker vsock")
	}

	jailedOpts, err := s.jailer.BuildJailedMachine(s.config, s.machineConfig, s.vmID)
	if err != nil {
		return errors.Wrap(err, "failed to build jailed machine options")
	}
	opts = append(opts, jailedOpts...)

	// In the event that a noop jailer is used, we will pass in the shim context
	// and have the SDK construct a new machine using that context. Otherwise, a
	// custom process runner will be provided via options which will stomp over
	// the shim context that was provided here.
	s.machine, err = firecracker.NewMachine(s.shimCtx, *s.machineConfig, opts...)
	if err != nil {
		return errors.Wrapf(err, "failed to create new machine instance")
	}

	if err = s.machine.Start(s.shimCtx); err != nil {
		return errors.Wrapf(err, "failed to start the VM")
	}

	s.logger.Info("calling agent")
	conn, err := vm.VSockDial(requestCtx, s.logger, relVSockPath, defaultVsockPort)
	if err != nil {
		return errors.Wrapf(err, "failed to dial the VM over vsock")
	}

	rpcClient := ttrpc.NewClient(conn, ttrpc.WithOnClose(func() { _ = conn.Close() }))
	s.agentClient = taskAPI.NewTaskClient(rpcClient)
	s.eventBridgeClient = eventbridge.NewGetterClient(rpcClient)
	s.driveMountClient = drivemount.NewDriveMounterClient(rpcClient)
	s.exitAfterAllTasksDeleted = request.ExitAfterAllTasksDeleted

	err = s.mountDrives(requestCtx)
	if err != nil {
		return err
	}

	s.logger.Info("successfully started the VM")
	return nil
}

func (s *service) mountDrives(requestCtx context.Context) error {
	for _, stubDrive := range s.driveMountStubs {
		err := stubDrive.PatchAndMount(requestCtx, s.machine, s.driveMountClient)
		if err != nil {
			return errors.Wrapf(err, "failed to patch drive mount stub")
		}
	}
	return nil
}

// StopVM will shutdown the VMM. Unlike Shutdown, this method is exposed to containerd clients.
// If the VM has not been created yet and the timeout is hit waiting for it to exist, an error will be returned
// but the shim will continue to shutdown.
func (s *service) StopVM(requestCtx context.Context, request *proto.StopVMRequest) (_ *empty.Empty, err error) {
	defer logPanicAndDie(s.logger)
	s.logger.WithFields(logrus.Fields{"timeout_seconds": request.TimeoutSeconds}).Debug("StopVM")

	timeout := defaultStopVMTimeout
	if request.TimeoutSeconds > 0 {
		timeout = time.Duration(request.TimeoutSeconds) * time.Second
	}

	err = s.waitVMReady()
	if err != nil {
		return nil, err
	}

	if err = s.shutdown(requestCtx, timeout, &taskAPI.ShutdownRequest{Now: true}); err != nil {
		return nil, err
	}
	return &empty.Empty{}, nil
}

// GetVMInfo returns metadata for the VM being managed by this shim. If the VM has not been created yet, this
// method will wait for up to a hardcoded timeout for it to exist, returning an error if the timeout is reached.
func (s *service) GetVMInfo(requestCtx context.Context, request *proto.GetVMInfoRequest) (*proto.GetVMInfoResponse, error) {
	defer logPanicAndDie(s.logger)

	err := s.waitVMReady()
	if err != nil {
		s.logger.WithError(err).Error()
		return nil, err
	}

	cgroupPath := ""
	if c, ok := s.jailer.(cgroupPather); ok {
		cgroupPath = c.CgroupPath()
	}

	return &proto.GetVMInfoResponse{
		VMID:            s.vmID,
		SocketPath:      s.shimDir.FirecrackerSockPath(),
		LogFifoPath:     s.machineConfig.LogPath,
		MetricsFifoPath: s.machineConfig.MetricsPath,
		CgroupPath:      cgroupPath,
	}, nil
}

// SetVMMetadata will update the VM being managed by this shim with the provided metadata. If the VM has not been created yet, this
// method will wait for up to a hardcoded timeout for it to exist, returning an error if the timeout is reached.
func (s *service) SetVMMetadata(requestCtx context.Context, request *proto.SetVMMetadataRequest) (*empty.Empty, error) {
	defer logPanicAndDie(s.logger)

	err := s.waitVMReady()
	if err != nil {
		s.logger.WithError(err).Error()
		return nil, err
	}

	s.logger.Info("setting VM metadata")
	jayson := json.RawMessage(request.Metadata)
	if err := s.machine.SetMetadata(requestCtx, jayson); err != nil {
		err = errors.Wrap(err, "failed to set VM metadata")
		s.logger.WithError(err).Error()
		return nil, err
	}

	return &empty.Empty{}, nil
}

// UpdateVMMetadata updates the VM being managed by this shim with the provided metadata patch.
// If the vm has not been created yet, this method will wait for up to the hardcoded timeout for it
// to exist, returning an error if the timeout is reached.
func (s *service) UpdateVMMetadata(requestCtx context.Context, request *proto.UpdateVMMetadataRequest) (*empty.Empty, error) {

	defer logPanicAndDie(s.logger)

	err := s.waitVMReady()
	if err != nil {
		s.logger.WithError(err).Error()
		return nil, err
	}

	s.logger.Info("updating VM metadata")
	jayson := json.RawMessage(request.Metadata)
	if err := s.machine.UpdateMetadata(requestCtx, jayson); err != nil {
		err = errors.Wrap(err, "failed to update VM metadata")
		s.logger.WithError(err).Error()
		return nil, err
	}

	return &empty.Empty{}, nil
}

// GetVMMetadata returns the metadata for the vm managed by this shim..
// If the vm has not been created yet, this method will wait for up to the hardcoded timeout for it
// to exist, returning an error if the timeout is reached.
func (s *service) GetVMMetadata(requestCtx context.Context, request *proto.GetVMMetadataRequest) (*proto.GetVMMetadataResponse, error) {

	defer logPanicAndDie(s.logger)

	err := s.waitVMReady()
	if err != nil {
		s.logger.WithError(err).Error()
		return nil, err
	}

	s.logger.Info("Get VM metadata")
	var metadata json.RawMessage
	if err := s.machine.GetMetadata(requestCtx, &metadata); err != nil {
		err = errors.Wrap(err, "failed to get VM metadata")
		s.logger.WithError(err).Error()
		return nil, err
	}

	return &proto.GetVMMetadataResponse{Metadata: string(metadata)}, nil
}

func (s *service) buildVMConfiguration(req *proto.CreateVMRequest) (*firecracker.Config, error) {
	for _, driveMount := range req.DriveMounts {
		// Verify the request specified an absolute path for the source/dest of drives.
		// Otherwise, users can implicitly rely on the CWD of this shim or agent.
		if !strings.HasPrefix(driveMount.HostPath, "/") || !strings.HasPrefix(driveMount.VMPath, "/") {
			return nil, errors.Errorf("driveMount %s contains relative path", driveMount.String())
		}
	}

	relSockPath, err := s.shimDir.FirecrackerSockRelPath()
	if err != nil {
		return nil, errors.Wrapf(err, "failed to get relative path to firecracker api socket")
	}

	relVSockPath, err := s.jailer.JailPath().FirecrackerVSockRelPath()
	if err != nil {
		return nil, errors.Wrapf(err, "failed to get relative path to firecracker vsock")
	}

	cfg := firecracker.Config{
		SocketPath: relSockPath,
		VsockDevices: []firecracker.VsockDevice{{
			Path: relVSockPath,
			ID:   "agent_api",
		}},
		MachineCfg: machineConfigurationFromProto(s.config, req.MachineCfg),
		LogLevel:   s.config.DebugHelper.GetFirecrackerLogLevel(),
		VMID:       s.vmID,
	}

	logPath := s.shimDir.FirecrackerLogFifoPath()
	if req.LogFifoPath != "" {
		logPath = req.LogFifoPath
	}
	err = syscall.Mkfifo(logPath, 0700)
	if err != nil {
		return nil, err
	}

	metricsPath := s.shimDir.FirecrackerMetricsFifoPath()
	if req.MetricsFifoPath != "" {
		metricsPath = req.MetricsFifoPath
	}
	err = syscall.Mkfifo(metricsPath, 0700)
	if err != nil {
		return nil, err
	}

	// The Config struct has LogFifo and MetricsFifo, but they will be deprecated since
	// Firecracker doesn't have the corresponding fields anymore.
	cfg.LogPath = logPath
	cfg.MetricsPath = metricsPath

	if req.JailerConfig != nil {
		cfg.NetNS = req.JailerConfig.NetNS
	}

	s.logger.Debugf("using socket path: %s", cfg.SocketPath)

	// Kernel configuration

	if val := req.KernelArgs; val != "" {
		cfg.KernelArgs = val
	} else {
		cfg.KernelArgs = s.config.KernelArgs
	}

	if val := req.KernelImagePath; val != "" {
		cfg.KernelImagePath = val
	} else {
		cfg.KernelImagePath = s.config.KernelImagePath
	}

	cfg.Drives = s.buildRootDrive(req)

	// Drives configuration
	containerCount := int(req.ContainerCount)
	if containerCount < 1 {
		// containerCount should always be positive so that at least one container
		// can run inside the VM. This makes the assumption that a task is going
		// to be run, and to do that at least one container is needed.
		containerCount = 1
	}

	s.containerStubHandler, err = CreateContainerStubs(
		&cfg, s.jailer, containerCount, s.logger)
	if err != nil {
		return nil, errors.Wrapf(err, "failed to create container stub drives")
	}

	s.driveMountStubs, err = CreateDriveMountStubs(
		&cfg, s.jailer, req.DriveMounts, s.logger)
	if err != nil {
		return nil, errors.Wrapf(err, "failed to create drive mount stub drives")
	}

	// If no value for NetworkInterfaces was specified (not even an empty but non-nil list) and
	// the runtime config specifies a default list, use those defaults
	if req.NetworkInterfaces == nil {
		for _, ni := range s.config.DefaultNetworkInterfaces {
			niCopy := ni // we don't want to allow any further calls to modify structs in s.config.DefaultNetworkInterfaces
			req.NetworkInterfaces = append(req.NetworkInterfaces, &niCopy)
		}
	}

	for _, ni := range req.NetworkInterfaces {
		netCfg, err := networkConfigFromProto(ni, s.vmID)
		if err != nil {
			return nil, errors.Wrapf(err, "failed to convert network config %+v", ni)
		}

		cfg.NetworkInterfaces = append(cfg.NetworkInterfaces, *netCfg)
	}

	return &cfg, nil
}

func (s *service) buildRootDrive(req *proto.CreateVMRequest) []models.Drive {
	var builder firecracker.DrivesBuilder

	if input := req.RootDrive; input != nil {
		builder = builder.WithRootDrive(input.HostPath,
			firecracker.WithReadOnly(!input.IsWritable),
			firecracker.WithPartuuid(input.Partuuid),
			withRateLimiterFromProto(input.RateLimiter))
	} else {
		builder = builder.WithRootDrive(s.config.RootDrive, firecracker.WithReadOnly(true))
	}

	return builder.Build()
}

func (s *service) newIOProxy(logger *logrus.Entry, stdin, stdout, stderr string, extraData *proto.ExtraData) (vm.IOProxy, error) {
	var ioConnectorSet vm.IOProxy

	relVSockPath, err := s.jailer.JailPath().FirecrackerVSockRelPath()
	if err != nil {
		return nil, errors.Wrapf(err, "failed to get relative path to firecracker vsock")
	}

	if vm.IsAgentOnlyIO(stdout, logger) {
		ioConnectorSet = vm.NewNullIOProxy()
	} else {
		var stdinConnectorPair *vm.IOConnectorPair
		if stdin != "" {
			stdinConnectorPair = &vm.IOConnectorPair{
				ReadConnector:  vm.ReadFIFOConnector(stdin),
				WriteConnector: vm.VSockDialConnector(defaultVSockConnectTimeout, relVSockPath, extraData.StdinPort),
			}
		}

		var stdoutConnectorPair *vm.IOConnectorPair
		if stdout != "" {
			stdoutConnectorPair = &vm.IOConnectorPair{
				ReadConnector:  vm.VSockDialConnector(defaultVSockConnectTimeout, relVSockPath, extraData.StdoutPort),
				WriteConnector: vm.WriteFIFOConnector(stdout),
			}
		}

		var stderrConnectorPair *vm.IOConnectorPair
		if stderr != "" {
			stderrConnectorPair = &vm.IOConnectorPair{
				ReadConnector:  vm.VSockDialConnector(defaultVSockConnectTimeout, relVSockPath, extraData.StderrPort),
				WriteConnector: vm.WriteFIFOConnector(stderr),
			}
		}

		ioConnectorSet = vm.NewIOConnectorProxy(stdinConnectorPair, stdoutConnectorPair, stderrConnectorPair)
	}
	return ioConnectorSet, nil
}

func (s *service) Create(requestCtx context.Context, request *taskAPI.CreateTaskRequest) (*taskAPI.CreateTaskResponse, error) {
	logger := s.logger.WithField("task_id", request.ID)
	defer logPanicAndDie(logger)

	err := s.waitVMReady()
	if err != nil {
		logger.WithError(err).Error()
		return nil, err
	}

	logger.WithFields(logrus.Fields{
		"bundle":     request.Bundle,
		"terminal":   request.Terminal,
		"stdin":      request.Stdin,
		"stdout":     request.Stdout,
		"stderr":     request.Stderr,
		"checkpoint": request.Checkpoint,
	}).Debug("creating task")

	hostBundleDir := bundle.Dir(request.Bundle)
	vmBundleDir := bundle.VMBundleDir(request.ID)

	err = s.shimDir.CreateBundleLink(request.ID, hostBundleDir)
	if err != nil {
		err = errors.Wrap(err, "failed to create VM dir bundle link")
		logger.WithError(err).Error()
		return nil, err
	}

	err = s.shimDir.CreateAddressLink(request.ID)
	if err != nil {
		err = errors.Wrap(err, "failed to create shim address symlink")
		logger.WithError(err).Error()
		return nil, err
	}

	// We don't support a rootfs with multiple mounts, only one mount can be exposed to the
	// vm per-container
	if len(request.Rootfs) != 1 {
		return nil, errors.Errorf("can only support rootfs with exactly one mount: %+v", request.Rootfs)
	}
	rootfsMnt := request.Rootfs[0]

	err = s.containerStubHandler.Reserve(requestCtx, request.ID,
		rootfsMnt.Source, vmBundleDir.RootfsPath(), "ext4", nil, s.driveMountClient, s.machine)
	if err != nil {
		err = errors.Wrapf(err, "failed to get stub drive for task %q", request.ID)
		logger.WithError(err).Error()
		return nil, err
	}

	ociConfigBytes, err := hostBundleDir.OCIConfig().Bytes()
	if err != nil {
		return nil, err
	}

	extraData, err := s.generateExtraData(ociConfigBytes, request.Options)
	if err != nil {
		err = errors.Wrap(err, "failed to generate extra data")
		logger.WithError(err).Error()
		return nil, err
	}

	request.Options, err = ptypes.MarshalAny(extraData)
	if err != nil {
		err = errors.Wrap(err, "failed to marshal extra data")
		logger.WithError(err).Error()
		return nil, err
	}

	ioConnectorSet, err := s.newIOProxy(logger, request.Stdin, request.Stdout, request.Stderr, extraData)
	if err != nil {
		return nil, err
	}

	// override the request with the bundle dir that should be used inside the VM
	request.Bundle = vmBundleDir.RootPath()

	// The rootfs is mounted via a MountDrive call, so unset Rootfs in the request.
	// We unfortunately can't rely on just having the runc shim inside the VM do
	// the mount for us because we sometimes need to do mount retries due to our
	// requirement of patching stub drives
	request.Rootfs = nil

	resp, err := s.taskManager.CreateTask(requestCtx, request, s.agentClient, ioConnectorSet)
	if err != nil {
		err = errors.Wrap(err, "failed to create task")
		logger.WithError(err).Error()
		return nil, err
	}

	return resp, nil
}

func (s *service) Start(requestCtx context.Context, req *taskAPI.StartRequest) (*taskAPI.StartResponse, error) {
	defer logPanicAndDie(log.G(requestCtx))

	log.G(requestCtx).WithFields(logrus.Fields{"task_id": req.ID, "exec_id": req.ExecID}).Debug("start")
	resp, err := s.agentClient.Start(requestCtx, req)
	if err != nil {
		return nil, err
	}

	return resp, nil
}

func (s *service) Delete(requestCtx context.Context, req *taskAPI.DeleteRequest) (*taskAPI.DeleteResponse, error) {
	defer logPanicAndDie(log.G(requestCtx))
	logger := log.G(requestCtx).WithFields(logrus.Fields{"task_id": req.ID, "exec_id": req.ExecID})

	logger.Debug("delete")

	resp, err := s.taskManager.DeleteProcess(requestCtx, req, s.agentClient)
	if err != nil {
		return nil, err
	}

	// Only delete a process as like runc when there is ExecID
	// https://github.com/containerd/containerd/blob/f3e148b1ccf268450c87427b5dbb6187db3d22f1/runtime/v2/runc/container.go#L320
	if req.ExecID != "" {
		return resp, nil
	}
	var result *multierror.Error
	// Trying to release stub drive for further reuse
	err = s.containerStubHandler.Release(requestCtx, req.ID, s.driveMountClient, s.machine)
	if err != nil {
		result = multierror.Append(result, errors.Wrapf(err, "failed to release stub drive for container: %s", req.ID))
	}

	// Otherwise, delete the container
	dir, err := s.shimDir.BundleLink(req.ID)
	if err != nil {
		result = multierror.Append(result, errors.Wrapf(err, "failed to find the bundle directory of the container: %s", req.ID))
	}

	_, err = os.Stat(dir.RootPath())
	if os.IsNotExist(err) {
		result = multierror.Append(result, errors.Wrapf(err, "failed to find the bundle directory of the container: %s", dir.RootPath()))
	}

	if err = os.Remove(dir.RootPath()); err != nil {
		result = multierror.Append(result, errors.Wrapf(err, "failed to remove the bundle directory of the container: %s", dir.RootPath()))
	}

	return resp, result.ErrorOrNil()
}

// Exec an additional process inside the container
func (s *service) Exec(requestCtx context.Context, req *taskAPI.ExecProcessRequest) (*ptypes.Empty, error) {
	defer logPanicAndDie(log.G(requestCtx))
	logger := s.logger.WithField("task_id", req.ID).WithField("exec_id", req.ExecID)
	logger.Debug("exec")

	// no OCI config bytes to provide for Exec, just leave those fields empty
	extraData, err := s.generateExtraData(nil, req.Spec)
	if err != nil {
		err = errors.Wrap(err, "failed to generate extra data")
		logger.WithError(err).Error()
		return nil, err
	}

	req.Spec, err = ptypes.MarshalAny(extraData)
	if err != nil {
		err = errors.Wrap(err, "failed to marshal extra data")
		logger.WithError(err).Error()
		return nil, err
	}

	ioConnectorSet, err := s.newIOProxy(logger, req.Stdin, req.Stdout, req.Stderr, extraData)
	if err != nil {
		return nil, err
	}

	resp, err := s.taskManager.ExecProcess(requestCtx, req, s.agentClient, ioConnectorSet)
	if err != nil {
		return nil, err
	}

	return resp, nil
}

// ResizePty of a process
func (s *service) ResizePty(requestCtx context.Context, req *taskAPI.ResizePtyRequest) (*ptypes.Empty, error) {
	defer logPanicAndDie(log.G(requestCtx))

	log.G(requestCtx).WithFields(logrus.Fields{"task_id": req.ID, "exec_id": req.ExecID}).Debug("resize_pty")
	resp, err := s.agentClient.ResizePty(requestCtx, req)
	if err != nil {
		return nil, err
	}

	return resp, nil
}

// State returns runtime state information for a process
func (s *service) State(requestCtx context.Context, req *taskAPI.StateRequest) (*taskAPI.StateResponse, error) {
	defer logPanicAndDie(log.G(requestCtx))

	log.G(requestCtx).WithFields(logrus.Fields{"task_id": req.ID, "exec_id": req.ExecID}).Debug("state")
	resp, err := s.agentClient.State(requestCtx, req)
	if err != nil {
		return nil, err
	}

	return resp, nil
}

// Pause the container
func (s *service) Pause(requestCtx context.Context, req *taskAPI.PauseRequest) (*ptypes.Empty, error) {
	defer logPanicAndDie(log.G(requestCtx))

	log.G(requestCtx).WithField("task_id", req.ID).Debug("pause")
	resp, err := s.agentClient.Pause(requestCtx, req)
	if err != nil {
		return nil, err
	}

	return resp, nil
}

// Resume the container
func (s *service) Resume(requestCtx context.Context, req *taskAPI.ResumeRequest) (*ptypes.Empty, error) {
	defer logPanicAndDie(log.G(requestCtx))

	log.G(requestCtx).WithField("task_id", req.ID).Debug("resume")
	resp, err := s.agentClient.Resume(requestCtx, req)
	if err != nil {
		return nil, err
	}

	return resp, nil
}

// Kill a process with the provided signal
func (s *service) Kill(requestCtx context.Context, req *taskAPI.KillRequest) (*ptypes.Empty, error) {
	defer logPanicAndDie(log.G(requestCtx))

	log.G(requestCtx).WithFields(logrus.Fields{"task_id": req.ID, "exec_id": req.ExecID}).Debug("kill")
	resp, err := s.agentClient.Kill(requestCtx, req)
	if err != nil {
		return nil, err
	}
	return resp, nil
}

// Pids returns all pids inside the container
func (s *service) Pids(requestCtx context.Context, req *taskAPI.PidsRequest) (*taskAPI.PidsResponse, error) {
	defer logPanicAndDie(log.G(requestCtx))

	log.G(requestCtx).WithField("task_id", req.ID).Debug("pids")
	resp, err := s.agentClient.Pids(requestCtx, req)
	if err != nil {
		return nil, err
	}

	return resp, nil
}

// CloseIO of a process
func (s *service) CloseIO(requestCtx context.Context, req *taskAPI.CloseIORequest) (*ptypes.Empty, error) {
	defer logPanicAndDie(log.G(requestCtx))

	log.G(requestCtx).WithFields(logrus.Fields{"task_id": req.ID, "exec_id": req.ExecID}).Debug("close_io")
	resp, err := s.agentClient.CloseIO(requestCtx, req)
	if err != nil {
		return nil, err
	}

	return resp, nil
}

// Checkpoint the container
func (s *service) Checkpoint(requestCtx context.Context, req *taskAPI.CheckpointTaskRequest) (*ptypes.Empty, error) {
	defer logPanicAndDie(log.G(requestCtx))

	log.G(requestCtx).WithFields(logrus.Fields{"task_id": req.ID, "path": req.Path}).Info("checkpoint")
	resp, err := s.agentClient.Checkpoint(requestCtx, req)
	if err != nil {
		return nil, err
	}

	return resp, nil
}

// Connect returns shim information such as the shim's pid
func (s *service) Connect(requestCtx context.Context, req *taskAPI.ConnectRequest) (*taskAPI.ConnectResponse, error) {
	defer logPanicAndDie(log.G(requestCtx))

	// Since task_pid inside the micro VM wouldn't make sense for clients,
	// we intentionally return ErrNotImplemented instead of forwarding that to the guest-side shim.
	// https://github.com/firecracker-microvm/firecracker-containerd/issues/210
	log.G(requestCtx).WithField("task_id", req.ID).Error(`"connect" is not implemented by the shim`)

	return nil, errdefs.ErrNotImplemented
}

// Shutdown will shutdown of the VMM. Unlike StopVM, this method is only exposed to containerd itself.
//
// The shutdown procedure will only actually take place if "Now" was set to true OR
// the VM started successfully, all tasks have been deleted and we were told to shutdown when all tasks were deleted.
// Otherwise the call is just ignored.
//
// containerd calls this API on behalf of the user in the following cases:
// * After any task is deleted via containerd's API
// * After any task Create call returns an error
func (s *service) Shutdown(requestCtx context.Context, req *taskAPI.ShutdownRequest) (*ptypes.Empty, error) {
	defer logPanicAndDie(log.G(requestCtx))
	s.logger.WithFields(logrus.Fields{"task_id": req.ID, "now": req.Now}).Debug("Shutdown")

	shouldShutdown := req.Now || s.exitAfterAllTasksDeleted && s.taskManager.ShutdownIfEmpty()
	if !shouldShutdown {
		return &ptypes.Empty{}, nil
	}

	if err := s.shutdown(requestCtx, defaultShutdownTimeout, req); err != nil {
		return &ptypes.Empty{}, err
	}

	return &ptypes.Empty{}, nil
}

func (s *service) shutdown(
	requestCtx context.Context,
	timeout time.Duration,
	req *taskAPI.ShutdownRequest,
) error {
	s.logger.Info("stopping the VM")

	go func() {
		s.shutdownLoop(requestCtx, timeout, req)
	}()

	var result *multierror.Error
	if err := s.machine.Wait(context.Background()); err != nil {
		result = multierror.Append(result, err)
	}
	if err := s.cleanup(); err != nil {
		result = multierror.Append(result, err)
	}

	if err := result.ErrorOrNil(); err != nil {
		return status.Error(codes.Internal, fmt.Sprintf("the VMM was killed forcibly: %v", err))
	}
	return nil
}

// shutdownLoop sends multiple different shutdown requests to stop the VMM.
// 1) send a request to the in-VM agent, which is presumed to cause the VM to begin a reboot.
// 2) stop the VM through jailer#Stop(). The signal should be visible from the VMM (e.g. SIGTERM)
// 3) stop the VM through cancelling the associated context. The signal would not be visible from the VMM (e.g. SIGKILL)
func (s *service) shutdownLoop(
	requestCtx context.Context,
	timeout time.Duration,
	req *taskAPI.ShutdownRequest,
) {
	actions := []struct {
		name     string
		shutdown func() error
		timeout  time.Duration
	}{
		{
			name: "send a request to the in-VM agent",
			shutdown: func() error {
				_, err := s.agentClient.Shutdown(requestCtx, req)
				if err != nil {
					return err
				}
				return nil
			},
			timeout: timeout,
		},
		{
			name: "stop the jailer by SIGTERM",
			shutdown: func() error {
				return s.jailer.Stop(false)
			},
			timeout: jailerStopTimeout,
		},
		{
			name: "stop the jailer by SIGKILL",
			shutdown: func() error {
				return s.jailer.Stop(true)
			},
			timeout: jailerStopTimeout,
		},
	}

	for _, action := range actions {
		pid, err := s.machine.PID()
		if pid == 0 && err != nil {
			break // we have nothing to kill
		}

		s.logger.Debug(action.name)
		err = action.shutdown()
		if err != nil {
			// if sending an request doesn't succeed, don't wait and carry on.
			s.logger.WithError(err).Errorf("failed to %s", action.name)
		} else {
			time.Sleep(action.timeout)
		}
	}
}

func (s *service) Stats(requestCtx context.Context, req *taskAPI.StatsRequest) (*taskAPI.StatsResponse, error) {
	defer logPanicAndDie(log.G(requestCtx))
	log.G(requestCtx).WithField("task_id", req.ID).Debug("stats")

	resp, err := s.agentClient.Stats(requestCtx, req)
	if err != nil {
		return nil, err
	}

	return resp, nil
}

// Update a running container
func (s *service) Update(requestCtx context.Context, req *taskAPI.UpdateTaskRequest) (*ptypes.Empty, error) {
	defer logPanicAndDie(log.G(requestCtx))
	log.G(requestCtx).WithField("task_id", req.ID).Debug("update")

	resp, err := s.agentClient.Update(requestCtx, req)
	if err != nil {
		return nil, err
	}

	return resp, nil
}

// Wait for a process to exit
func (s *service) Wait(requestCtx context.Context, req *taskAPI.WaitRequest) (*taskAPI.WaitResponse, error) {
	defer logPanicAndDie(log.G(requestCtx))
	log.G(requestCtx).WithFields(logrus.Fields{"task_id": req.ID, "exec_id": req.ExecID}).Debug("wait")

	resp, err := s.agentClient.Wait(requestCtx, req)
	if err != nil {
		return nil, err
	}

	return resp, nil
}

func (s *service) Cleanup(requestCtx context.Context) (*taskAPI.DeleteResponse, error) {
	defer logPanicAndDie(log.G(requestCtx))

	err := s.waitVMReady()
	if err != nil {
		s.logger.WithError(err).Error()
		return nil, err
	}

	log.G(requestCtx).Debug("cleanup")
	// Destroy VM/etc here?
	// copied from runcs impl, nothing to cleanup atm
	return &taskAPI.DeleteResponse{
		ExitedAt:   time.Now(),
		ExitStatus: 128 + uint32(unix.SIGKILL),
	}, nil
}

// cleanup resources
func (s *service) cleanup() error {
	s.cleanupOnce.Do(func() {
		var result *multierror.Error
		// we ignore the error here due to cleanup will only succeed if the jailing
		// process was killed via SIGKILL
		if err := s.jailer.Close(); err != nil {
			result = multierror.Append(result, err)
			s.logger.WithError(err).Error("failed to close jailer")
		}

		if err := s.publishVMStop(); err != nil {
			result = multierror.Append(result, err)
			s.logger.WithError(err).Error("failed to publish stop VM event")
		}

		// once the VM shuts down, the shim should too
		s.shimCancel()

		s.cleanupErr = result.ErrorOrNil()
	})
	return s.cleanupErr
}

// monitorVMExit watches the VM and cleanup resources when it terminates.
func (s *service) monitorVMExit() {
	// Block until the VM exits
	if err := s.machine.Wait(s.shimCtx); err != nil && err != context.Canceled {
		s.logger.WithError(err).Error("error returned from VM wait")
	}

	if err := s.cleanup(); err != nil {
		s.logger.WithError(err).Error("failed to clean up the VM")
	}
}
