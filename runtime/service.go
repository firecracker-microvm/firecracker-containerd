// Copyright 2018-2019 Amazon.com, Inc. or its affiliates. All Rights Reserved.
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
	"unsafe"

	// disable gosec check for math/rand. We just need a random starting
	// place to start looking for CIDs; no need for cryptographically
	// secure randomness
	"math/rand" // #nosec

	"github.com/containerd/containerd/errdefs"
	"github.com/containerd/containerd/events"
	"github.com/containerd/containerd/events/exchange"
	"github.com/containerd/containerd/log"
	"github.com/containerd/containerd/namespaces"
	"github.com/containerd/containerd/pkg/ttrpcutil"
	"github.com/containerd/containerd/runtime/v2/shim"
	taskAPI "github.com/containerd/containerd/runtime/v2/task"
	"github.com/containerd/fifo"
	"github.com/containerd/ttrpc"
	"github.com/firecracker-microvm/firecracker-go-sdk"
	models "github.com/firecracker-microvm/firecracker-go-sdk/client/models"
	"github.com/gofrs/uuid"
	ptypes "github.com/gogo/protobuf/types"
	"github.com/golang/protobuf/ptypes/empty"
	"github.com/hashicorp/go-multierror"
	"github.com/pkg/errors"
	"github.com/sirupsen/logrus"
	"golang.org/x/sys/unix"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/firecracker-microvm/firecracker-containerd/eventbridge"
	"github.com/firecracker-microvm/firecracker-containerd/internal"
	"github.com/firecracker-microvm/firecracker-containerd/internal/bundle"
	fcShim "github.com/firecracker-microvm/firecracker-containerd/internal/shim"
	"github.com/firecracker-microvm/firecracker-containerd/internal/vm"
	"github.com/firecracker-microvm/firecracker-containerd/proto"
	fccontrolTtrpc "github.com/firecracker-microvm/firecracker-containerd/proto/service/fccontrol/ttrpc"
)

func init() {
	rand.Seed(time.Now().UnixNano())
}

const (
	defaultVsockPort        = 10789
	minVsockIOPort          = uint32(11000)
	firecrackerStartTimeout = 5 * time.Second

	// StartEventName is the topic published to when a VM starts
	StartEventName = "/firecracker-vm/start"

	// StopEventName is the topic published to when a VM stops
	StopEventName = "/firecracker-vm/stop"
)

var (
	// type assertions
	_ taskAPI.TaskService = &service{}
	_ shim.Init           = NewService

	sysCall = syscall.Syscall
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

	config *Config

	// vmReady is closed once CreateVM has been successfully called
	vmReady                  chan struct{}
	vmStartOnce              sync.Once
	agentClient              taskAPI.TaskService
	eventBridgeClient        eventbridge.Getter
	stubDriveHandler         stubDriveHandler
	exitAfterAllTasksDeleted bool // exit the VM and shim when all tasks are deleted

	machine          *firecracker.Machine
	machineConfig    *firecracker.Config
	machineCID       uint32
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
	config, err := LoadConfig("")
	if err != nil {
		return nil, err
	}

	if !config.Debug {
		opts, err := shimOpts(shimCtx)
		if err != nil {
			return nil, err
		}

		config.Debug = opts.Debug
	}

	namespace, ok := namespaces.Namespace(shimCtx)
	if !ok {
		namespace = namespaces.Default
	}

	var shimDir vm.Dir
	vmID := os.Getenv(internal.VMIDEnvVarKey)
	logger := log.G(shimCtx)
	if vmID != "" {
		logger = logger.WithField("vmID", vmID)

		shimDir, err = vm.ShimDir(namespace, vmID)
		if err != nil {
			return nil, errors.Wrap(err, "invalid shim directory")
		}
	}

	if config.Debug {
		logrus.SetLevel(logrus.DebugLevel)
		logger.Logger.SetLevel(logrus.DebugLevel)
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

		config: config,

		vmReady: make(chan struct{}),
	}

	s.stubDriveHandler = newStubDriveHandler(s.shimDir.RootPath(), logger)
	s.startEventForwarders(remotePublisher)

	err = s.serveFCControl()
	if err != nil {
		err = errors.Wrap(err, "failed to start fccontrol server")
		s.logger.WithError(err).Error()
		return nil, err
	}

	return s, nil
}

func (s *service) startEventForwarders(remotePublisher events.Publisher) {
	// Republish each event received on our exchange to the provided remote publisher.
	// TODO ideally we would be forwarding events instead of re-publishing them, which would
	// preserve the events' original timestamps and namespaces. However, as of this writing,
	// the containerd v2 runtime model only provides a shim with a publisher, not a forwarder.
	go func() {
		err := <-eventbridge.Republish(s.shimCtx, s.eventExchange, remotePublisher)
		if err != nil && err != context.Canceled {
			s.logger.WithError(err).Error("error while republishing events")
		}
	}()

	// Once the VM is ready, also start forwarding events from it to our exchange
	go func() {
		<-s.vmReady
		err := <-eventbridge.Attach(s.shimCtx, s.eventBridgeClient, s.eventExchange)
		if err != nil && err != context.Canceled {
			s.logger.WithError(err).Error("error while forwarding events from VM agent")
		}
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

func (s *service) StartShim(shimCtx context.Context, containerID, containerdBinary, containerdAddress string) (string, error) {
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

		log.Info("will start a single-task VM since no VMID has been provided")
	} else {
		log.Info("will start a persistent VM")
	}

	client, err := ttrpcutil.NewClient(containerdAddress + ".ttrpc")
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

	return fcShim.SocketAddress(shimCtx, s.vmID)
}

func logPanicAndDie(logger *logrus.Entry) {
	if err := recover(); err != nil {
		logger.WithError(err.(error)).Fatalf("panic: %s", string(debug.Stack()))
	}
}

func (s *service) generateExtraData(jsonBytes []byte, driveID *string, options *ptypes.Any) (*proto.ExtraData, error) {
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
		DriveID:     firecracker.StringValue(driveID),
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
	case <-time.After(firecrackerStartTimeout):
		return status.Error(codes.DeadlineExceeded, "timed out waiting for VM start")
	}
}

// CreateVM will attempt to create the VM as specified in the provided request, but only on the first request
// received. Any subsequent requests will be ignored and get an AlreadyExists error response.
func (s *service) CreateVM(requestCtx context.Context, request *proto.CreateVMRequest) (*empty.Empty, error) {
	defer logPanicAndDie(s.logger)

	var (
		err       error
		createRan bool
	)

	s.vmStartOnce.Do(func() {
		err = s.createVM(requestCtx, request)
		createRan = true
	})

	if !createRan {
		return nil, status.Error(codes.AlreadyExists, "shim cannot create VM more than once")
	}

	// If we failed to create the VM, we have no point in existing anymore, so shutdown
	if err != nil {
		s.shimCancel()
		err = errors.Wrap(err, "failed to create VM")
		s.logger.WithError(err).Error()
		return nil, err
	}

	// creating the VM succeeded, setup monitors and publish events to celebrate
	err = s.publishVMStart()
	if err != nil {
		s.logger.WithError(err).Error("failed to publish start VM event")
	}

	go s.monitorVMExit()

	// let all the other methods know that the VM is ready for tasks
	close(s.vmReady)
	return &empty.Empty{}, nil
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

	s.logger.Info("creating new VM")

	s.machineCID, vsockFd, err = findNextAvailableVsockCID(requestCtx)
	if err != nil {
		return errors.Wrapf(err, "failed to find vsock for VM")
	}

	s.machineConfig, err = s.buildVMConfiguration(request)
	if err != nil {
		return errors.Wrapf(err, "failed to build VM configuration")
	}

	cmd := firecracker.VMCommandBuilder{}.
		WithBin(s.config.FirecrackerBinaryPath).
		WithSocketPath(s.shimDir.FirecrackerSockPath()).
		Build(s.shimCtx) // shimCtx so the VM process is only killed when the shim shuts down

	// use shimCtx so the VM is killed when the shim shuts down
	s.machine, err = firecracker.NewMachine(s.shimCtx, *s.machineConfig,
		firecracker.WithLogger(s.logger), firecracker.WithProcessRunner(cmd))
	if err != nil {
		return errors.Wrapf(err, "failed to create new machine instance")
	}

	// Close the vsock FD before starting the machine so firecracker doesn't get EADDRINUSE.
	// This technically leaves us vulnerable to race conditions, but given the small time
	// window and the fact that we choose a random CID from a 32bit range, the chances are
	// infinitesimal. This will also no longer be an issue when firecracker implements its
	// new vsock model.
	err = vsockFd.Close()
	if err != nil {
		return errors.Wrapf(err, "failed to close vsock")
	}

	if err = s.machine.Start(s.shimCtx); err != nil {
		return errors.Wrapf(err, "failed to start the VM")
	}

	s.logger.Info("calling agent")
	conn, err := vm.VSockDial(requestCtx, s.logger, s.machineCID, defaultVsockPort)
	if err != nil {
		return errors.Wrapf(err, "failed to dial the VM over vsock")
	}

	rpcClient := ttrpc.NewClient(conn, ttrpc.WithOnClose(func() { _ = conn.Close() }))
	s.agentClient = taskAPI.NewTaskClient(rpcClient)
	s.eventBridgeClient = eventbridge.NewGetterClient(rpcClient)
	s.exitAfterAllTasksDeleted = request.ExitAfterAllTasksDeleted

	s.logger.Info("successfully started the VM")

	return nil
}

// StopVM will shutdown the firecracker VM and start this shim's shutdown procedure. If the VM has not been
// created yet and the timeout is hit waiting for it to exist, an error will be returned but the shim will
// continue to shutdown.
func (s *service) StopVM(requestCtx context.Context, request *proto.StopVMRequest) (_ *empty.Empty, err error) {
	defer logPanicAndDie(s.logger)
	// If something goes wrong here, just shut down ungracefully. This eliminates some scenarios that would result
	// in the user being unable to shut down the VM.
	defer func() {
		if err != nil {
			s.logger.WithError(err).Error("StopVM error, shim is shutting down ungracefully")
			s.shimCancel()
		}
	}()

	err = s.waitVMReady()
	if err != nil {
		return nil, err
	}

	// The graceful shutdown logic, including stopping the VM, is centralized in Shutdown. We set "Now" to true
	// to ensure that the service will actually shutdown even if there are still containers being managed.
	_, err = s.Shutdown(requestCtx, &taskAPI.ShutdownRequest{Now: true})
	if err != nil {
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

	return &proto.GetVMInfoResponse{
		VMID:            s.vmID,
		ContextID:       s.machineCID,
		SocketPath:      s.machineConfig.SocketPath,
		LogFifoPath:     s.machineConfig.LogFifo,
		MetricsFifoPath: s.machineConfig.MetricsFifo,
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

	s.logger.Info("updating VM metadata")
	if err := s.machine.SetMetadata(requestCtx, request.Metadata); err != nil {
		err = errors.Wrap(err, "failed to set VM metadata")
		s.logger.WithError(err).Error()
		return nil, err
	}

	return &empty.Empty{}, nil
}

func (s *service) createStubDrives(stubDriveCount int) ([]models.Drive, error) {
	paths, err := s.stubDriveHandler.StubDrivePaths(stubDriveCount)
	if err != nil {
		return nil, errors.Wrap(err, "failed to retrieve stub drive paths")
	}

	stubDrives := make([]models.Drive, 0, stubDriveCount)
	for i, path := range paths {
		stubDrives = append(stubDrives, models.Drive{
			DriveID:      firecracker.String(fmt.Sprintf("stub%d", i)),
			IsReadOnly:   firecracker.Bool(false),
			PathOnHost:   firecracker.String(path),
			IsRootDevice: firecracker.Bool(false),
		})
	}

	return stubDrives, nil
}

func (s *service) buildVMConfiguration(req *proto.CreateVMRequest) (*firecracker.Config, error) {
	logger := s.logger.WithField("cid", s.machineCID)

	cfg := firecracker.Config{
		SocketPath:   s.shimDir.FirecrackerSockPath(),
		VsockDevices: []firecracker.VsockDevice{{Path: "root", CID: s.machineCID}},
		LogFifo:      s.shimDir.FirecrackerLogFifoPath(),
		MetricsFifo:  s.shimDir.FirecrackerMetricsFifoPath(),
		MachineCfg:   machineConfigurationFromProto(s.config, req.MachineCfg),
		LogLevel:     s.config.LogLevel,
		Debug:        s.config.Debug,
	}

	logger.Debugf("using socket path: %s", cfg.SocketPath)

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

	// Drives configuration
	containerCount := int(req.ContainerCount)
	if containerCount < 1 {
		// containerCount should always be positive so that at least one container
		// can run inside the VM. This makes the assumption that a task is going
		// to be run, and to do that at least one container is needed.
		containerCount = 1
	}

	// Create stub drives first and let stub driver handler manage the drives
	stubDrives, err := s.createStubDrives(containerCount)
	if err != nil {
		return nil, errors.Wrap(err, "failed to create stub drives")
	}
	s.stubDriveHandler.SetDrives(stubDrives)

	var driveBuilder firecracker.DrivesBuilder
	// Create non-stub drives
	if root := req.RootDrive; root != nil {
		driveBuilder = firecracker.NewDrivesBuilder(root.PathOnHost)
	} else {
		driveBuilder = firecracker.NewDrivesBuilder(s.config.RootDrive)
	}

	for _, drive := range req.AdditionalDrives {
		driveBuilder = addDriveFromProto(driveBuilder, drive)
	}

	// a micro VM must know all drives
	// nolint: gocritic
	cfg.Drives = append(stubDrives, driveBuilder.Build()...)

	// Setup network interfaces

	for _, ni := range req.NetworkInterfaces {
		cfg.NetworkInterfaces = append(cfg.NetworkInterfaces, networkConfigFromProto(ni))
	}

	return &cfg, nil
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

	bundleDir := bundle.Dir(request.Bundle)
	err = s.shimDir.CreateBundleLink(request.ID, bundleDir)
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

	var driveID *string
	for _, mnt := range request.Rootfs {
		driveID, err = s.stubDriveHandler.PatchStubDrive(requestCtx, s.machine, mnt.Source)
		if err != nil {
			if err == ErrDrivesExhausted {
				return nil, errors.Wrapf(errdefs.ErrUnavailable, "no remaining stub drives to be used")
			}
			return nil, errors.Wrapf(err, "failed to patch stub drive")
		}
	}

	ociConfigBytes, err := bundleDir.OCIConfig().Bytes()
	if err != nil {
		return nil, err
	}

	extraData, err := s.generateExtraData(ociConfigBytes, driveID, request.Options)
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

	var ioConnectorSet vm.IOProxy

	if vm.IsAgentOnlyIO(request.Stdout, logger) {
		ioConnectorSet = vm.NewNullIOProxy()
	} else {
		var stdinConnectorPair *vm.IOConnectorPair
		if request.Stdin != "" {
			stdinConnectorPair = &vm.IOConnectorPair{
				ReadConnector:  vm.FIFOConnector(request.Stdin),
				WriteConnector: vm.VSockDialConnector(s.machineCID, extraData.StdinPort),
			}
		}

		var stdoutConnectorPair *vm.IOConnectorPair
		if request.Stdout != "" {
			stdoutConnectorPair = &vm.IOConnectorPair{
				ReadConnector:  vm.VSockDialConnector(s.machineCID, extraData.StdoutPort),
				WriteConnector: vm.FIFOConnector(request.Stdout),
			}
		}

		var stderrConnectorPair *vm.IOConnectorPair
		if request.Stderr != "" {
			stderrConnectorPair = &vm.IOConnectorPair{
				ReadConnector:  vm.VSockDialConnector(s.machineCID, extraData.StderrPort),
				WriteConnector: vm.FIFOConnector(request.Stderr),
			}
		}

		ioConnectorSet = vm.NewIOConnectorProxy(stdinConnectorPair, stdoutConnectorPair, stderrConnectorPair)
	}

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

	// Otherwise, delete the container
	dir, err := s.shimDir.BundleLink(req.ID)
	if err != nil {
		return nil, errors.Wrapf(err, "failed to find the bundle directory of the container: %s", req.ID)
	}

	_, err = os.Stat(dir.RootPath())
	if os.IsNotExist(err) {
		return nil, errors.Wrapf(err, "failed to find the bundle directory of the container: %s", dir.RootPath())
	}

	if err = os.Remove(dir.RootPath()); err != nil {
		return nil, errors.Wrapf(err, "failed to remove the bundle directory of the container: %s", dir.RootPath())
	}

	return resp, nil
}

// Exec an additional process inside the container
func (s *service) Exec(requestCtx context.Context, req *taskAPI.ExecProcessRequest) (*ptypes.Empty, error) {
	defer logPanicAndDie(log.G(requestCtx))
	logger := s.logger.WithField("task_id", req.ID).WithField("exec_id", req.ExecID)
	logger.Debug("exec")

	// no OCI config bytes or DriveID to provide for Exec, just leave those fields empty
	extraData, err := s.generateExtraData(nil, nil, req.Spec)
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

	var ioConnectorSet vm.IOProxy

	if vm.IsAgentOnlyIO(req.Stdout, logger) {
		ioConnectorSet = vm.NewNullIOProxy()
	} else {
		var stdinConnectorPair *vm.IOConnectorPair
		if req.Stdin != "" {
			stdinConnectorPair = &vm.IOConnectorPair{
				ReadConnector:  vm.FIFOConnector(req.Stdin),
				WriteConnector: vm.VSockDialConnector(s.machineCID, extraData.StdinPort),
			}
		}

		var stdoutConnectorPair *vm.IOConnectorPair
		if req.Stdout != "" {
			stdoutConnectorPair = &vm.IOConnectorPair{
				ReadConnector:  vm.VSockDialConnector(s.machineCID, extraData.StdoutPort),
				WriteConnector: vm.FIFOConnector(req.Stdout),
			}
		}

		var stderrConnectorPair *vm.IOConnectorPair
		if req.Stderr != "" {
			stderrConnectorPair = &vm.IOConnectorPair{
				ReadConnector:  vm.VSockDialConnector(s.machineCID, extraData.StderrPort),
				WriteConnector: vm.FIFOConnector(req.Stderr),
			}
		}

		ioConnectorSet = vm.NewIOConnectorProxy(stdinConnectorPair, stdoutConnectorPair, stderrConnectorPair)
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

// Shutdown will attempt a graceful shutdown of the shim+VM. The shutdown procedure will only actually take
// place if "Now" was set to true OR the VM started successfully, all tasks have been deleted and we were
// told to shutdown when all tasks were deleted. Otherwise the call is just ignored.
//
// Shutdown can be called from a few code paths:
// * If StopVM is called by the user (in which case "Now" is set to true)
// * After any task is deleted via containerd's API (containerd calls on behalf of the user)
// * After any task Create call returns an error (containerd calls on behalf of the user)
// Shutdown is not directly exposed to containerd clients.
func (s *service) Shutdown(requestCtx context.Context, req *taskAPI.ShutdownRequest) (*ptypes.Empty, error) {
	defer logPanicAndDie(log.G(requestCtx))
	s.logger.WithFields(logrus.Fields{"task_id": req.ID, "now": req.Now}).Debug("shutdown")

	shouldShutdown := req.Now || s.exitAfterAllTasksDeleted && s.taskManager.ShutdownIfEmpty()
	if !shouldShutdown {
		return &ptypes.Empty{}, nil
	}

	// cancel the shim context no matter what, which will result in the VM getting a SIGKILL (if not already
	// dead from graceful shutdown) and the shim process itself to begin exiting
	defer s.shimCancel()

	s.logger.Info("stopping the VM")

	var shutdownErr error

	_, err := s.agentClient.Shutdown(requestCtx, req)
	if err != nil {
		shutdownErr = multierror.Append(shutdownErr, errors.Wrap(err, "failed to shutdown VM Agent"))
	}

	err = s.machine.StopVMM()
	if err != nil {
		shutdownErr = multierror.Append(shutdownErr, errors.Wrap(err, "failed to gracefully stop VM"))
	}

	err = os.RemoveAll(s.shimDir.RootPath())
	if err != nil {
		shutdownErr = multierror.Append(shutdownErr, errors.Wrapf(err, "failed to remove VM dir %q during shutdown", s.shimDir.RootPath()))
	}

	if shutdownErr != nil {
		s.logger.WithError(shutdownErr).Error()
		return nil, shutdownErr
	}

	s.logger.Info("successfully stopped the VM")
	return &ptypes.Empty{}, nil
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

// findNextAvailableVsockCID finds first available vsock context ID.
// It uses VHOST_VSOCK_SET_GUEST_CID ioctl which allows some CID ranges to be statically reserved in advance.
// The ioctl fails with EADDRINUSE if cid is already taken and with EINVAL if the CID is invalid.
// Taken from https://bugzilla.redhat.com/show_bug.cgi?id=1291851
func findNextAvailableVsockCID(requestCtx context.Context) (cid uint32, file *os.File, err error) {
	const (
		// Corresponds to VHOST_VSOCK_SET_GUEST_CID in vhost.h
		ioctlVsockSetGuestCID = uintptr(0x4008AF60)
		// 0, 1 and 2 are reserved CIDs, see http://man7.org/linux/man-pages/man7/vsock.7.html
		minCID          = 3
		maxCID          = math.MaxUint32
		cidRange        = maxCID - minCID
		vsockDevicePath = "/dev/vhost-vsock"
	)

	defer func() {
		// close the vsock file if anything bad happens
		if err != nil && file != nil {
			file.Close()
		}
	}()

	file, err = os.OpenFile(vsockDevicePath, syscall.O_RDWR, 0600)
	if err != nil {
		return 0, nil, errors.Wrap(err, "failed to open vsock device")
	}

	// Start at a random ID to minimize chances of conflicts when shims are racing each other here
	start := rand.Intn(cidRange)
	for n := 0; n < cidRange; n++ {
		cid := minCID + ((start + n) % cidRange)
		select {
		case <-requestCtx.Done():
			return 0, nil, requestCtx.Err()
		default:
			_, _, err = sysCall(
				unix.SYS_IOCTL,
				file.Fd(),
				ioctlVsockSetGuestCID,
				uintptr(unsafe.Pointer(&cid)))

			switch err {
			case unix.Errno(0):
				return uint32(cid), file, nil
			case unix.EADDRINUSE:
				// ID is already taken, try next one
				continue
			default:
				// Fail if we get an error we don't expect
				return 0, nil, err
			}
		}
	}

	return 0, nil, errors.New("couldn't find any available vsock context id")
}

func (s *service) monitorVMExit() {
	// once the VM shuts down, the shim should too
	defer s.shimCancel()

	// Block until the VM exits
	waitErr := s.machine.Wait(s.shimCtx)
	if waitErr != nil && waitErr != context.Canceled {
		s.logger.WithError(waitErr).Error("error returned from VM wait")
	}

	publishErr := s.publishVMStop()
	if publishErr != nil {
		s.logger.WithError(publishErr).Error("failed to publish stop VM event")
	}
}
