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

	"github.com/containerd/containerd"
	eventsAPI "github.com/containerd/containerd/api/events"
	"github.com/containerd/containerd/cio"
	"github.com/containerd/containerd/events"
	"github.com/containerd/containerd/events/exchange"
	"github.com/containerd/containerd/log"
	"github.com/containerd/containerd/namespaces"
	"github.com/containerd/containerd/runtime"
	"github.com/containerd/containerd/runtime/v2/shim"
	taskAPI "github.com/containerd/containerd/runtime/v2/task"
	"github.com/containerd/fifo"
	"github.com/containerd/ttrpc"
	"github.com/containerd/typeurl"
	"github.com/firecracker-microvm/firecracker-go-sdk"
	models "github.com/firecracker-microvm/firecracker-go-sdk/client/models"
	"github.com/gofrs/uuid"
	ptypes "github.com/gogo/protobuf/types"
	"github.com/golang/protobuf/ptypes/empty"
	"github.com/mdlayher/vsock"
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
	fccontrolGrpc "github.com/firecracker-microvm/firecracker-containerd/proto/service/fccontrol/grpc"
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
	taskManager vm.TaskManager

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

	vmID   string
	config *Config

	// vmReady is closed once CreateVM has been successfully called
	vmReady           chan struct{}
	vmStartOnce       sync.Once
	rootfsOnce        sync.Once // TODO remove once stub drives are merged
	agentClient       taskAPI.TaskService
	eventBridgeClient eventbridge.Getter

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

	vmID := os.Getenv(internal.VMIDEnvVarKey)
	logger := log.G(shimCtx)
	if vmID != "" {
		logger = logger.WithField("vmID", vmID)
	}

	s := &service{
		taskManager: vm.NewTaskManager(logger),

		eventExchange: exchange.NewExchange(),
		namespace:     namespace,

		logger:     logger,
		shimCtx:    shimCtx,
		shimCancel: shimCancel,

		vmID:   vmID,
		config: config,

		vmReady: make(chan struct{}),
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

	log.G(shimCtx).WithField("id", containerID).Debug("StartShim")

	// If we are running a shim start routine, we can safely assume our current working
	// directory is the bundle directory
	cwd, err := os.Getwd()
	if err != nil {
		return "", errors.Wrap(err, "failed to get current working directory")
	}
	bundleDir := bundle.Dir(cwd)

	var exitAfterAllTasksGone bool

	// Since we're running a shim start routine, we need to determine the vmID for the incoming
	// container. Start by looking at the container's OCI annotations
	s.vmID, err = bundleDir.OCIConfig().VMID()
	if err != nil {
		return "", err
	}

	if s.vmID == "" {
		// If here, no VMID has been provided by the client for this container, so auto-generate a new one.
		// This results in a default behavior of running each container in its own VM if not otherwise
		// specified by the client.
		uuid, err := uuid.NewV4()
		if err != nil {
			return "", errors.Wrap(err, "failed to generate UUID for VMID")
		}

		s.vmID = uuid.String()

		// If the client didn't specify a VMID, the VM should exit after this task is gone
		exitAfterAllTasksGone = true
	}

	client, err := containerd.New(containerdAddress)
	if err != nil {
		return "", err
	}

	fcControlClient := fccontrolGrpc.NewFirecrackerClient(client.Conn())

	_, err = fcControlClient.CreateVM(shimCtx, &proto.CreateVMRequest{
		VMID:                  s.vmID,
		ExitAfterAllTasksGone: exitAfterAllTasksGone,
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

func parseCreateTaskOpts(opts *ptypes.Any) (*proto.FirecrackerConfig, *ptypes.Any, error) {
	cfg, err := typeurl.UnmarshalAny(opts)
	if err != nil {
		return nil, nil, errors.Wrapf(err, "unmarshaling task create request options")
	}
	// We've verified that this is a valid prototype at this point of time.
	// Check if it's the FirecrackerConfig type
	firecrackerConfig, ok := cfg.(*proto.FirecrackerConfig)
	if ok {
		// We've verified that the proto message was of FirecrackerConfig type.
		// Get runc options based on what was set in FirecrackerConfig.
		return firecrackerConfig, firecrackerConfig.RuncOptions, nil
	}
	// This is a valid proto message, but is not FirecrackerConfig type.
	// Treat the message as runc opts
	return nil, opts, nil
}

func (s *service) generateExtraData(bundleDir bundle.Dir, options *ptypes.Any) (*proto.ExtraData, error) {
	// Add the bundle/config.json to the request so it can be recreated
	// inside the vm:
	// Read bundle json
	jsonBytes, err := bundleDir.OCIConfig().Bytes()
	if err != nil {
		return nil, err
	}

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
	go s.monitorTaskExit(request.ExitAfterAllTasksGone)

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
		WithSocketPath(s.shimDir().FirecrackerSockPath()).
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

	if err = s.machine.Start(requestCtx); err != nil {
		return errors.Wrapf(err, "failed to start the VM")
	}

	s.logger.Info("calling agent")
	conn, err := dialVsock(requestCtx, s.machineCID, defaultVsockPort)
	if err != nil {
		return errors.Wrapf(err, "failed to dial the VM over vsock")
	}

	rpcClient := ttrpc.NewClient(conn, ttrpc.WithOnClose(func() { _ = conn.Close() }))
	s.agentClient = taskAPI.NewTaskClient(rpcClient)
	s.eventBridgeClient = eventbridge.NewGetterClient(rpcClient)

	s.logger.Info("successfully started the VM")

	return nil
}

// StopVM will shutdown the firecracker VM and start this shim's shutdown procedure. If the VM has not been
// created yet, an error will be returned but the shim will continue to shutdown.
func (s *service) StopVM(requestCtx context.Context, request *proto.StopVMRequest) (*empty.Empty, error) {
	defer logPanicAndDie(s.logger)

	// Shutdown the shim after this method is called, whether the clean StopVMM was successful or not.
	// If StopVMM didn't work, the cancel results in the VM cmd context being cancelled, sending SIGKILL
	// to the VM process.
	defer s.shimCancel()

	s.logger.Info("stopping the VM")

	err := s.machine.StopVMM()
	if err != nil {
		err = errors.Wrap(err, "failed to gracefully stop VM")
		s.logger.WithError(err).Error()
		return nil, err
	}

	err = os.RemoveAll(s.shimDir().RootPath())
	if err != nil {
		err = errors.Wrap(err, "failed to remove VM dir during shutdown")
		s.logger.WithField("path", s.shimDir().RootPath()).WithError(err).Error()
		return nil, err
	}

	s.logger.Info("successfully stopped the VM")
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

func (s *service) shimDir() vm.Dir {
	return vm.ShimDir(s.namespace, s.vmID)
}

func (s *service) buildVMConfiguration(req *proto.CreateVMRequest) (*firecracker.Config, error) {
	logger := s.logger.WithField("cid", s.machineCID)

	cfg := firecracker.Config{
		SocketPath:   s.shimDir().FirecrackerSockPath(),
		VsockDevices: []firecracker.VsockDevice{{Path: "root", CID: s.machineCID}},
		LogFifo:      s.shimDir().FirecrackerLogFifoPath(),
		MetricsFifo:  s.shimDir().FirecrackerMetricsFifoPath(),
		MachineCfg:   machineConfigurationFromProto(req.MachineCfg),
	}

	logger.Debugf("using socket path: %s", cfg.SocketPath)

	// Kernel configuration

	if val := req.KernelArgs; val != "" {
		cfg.KernelArgs = val
	} else {
		cfg.KernelArgs = defaultKernelArgs
	}

	if val := req.KernelImagePath; val != "" {
		cfg.KernelImagePath = val
	} else {
		cfg.KernelImagePath = defaultKernelPath
	}

	// Drives configuration

	var driveBuilder firecracker.DrivesBuilder
	if root := req.RootDrive; root != nil {
		driveBuilder = firecracker.NewDrivesBuilder(root.PathOnHost)
	} else {
		driveBuilder = firecracker.NewDrivesBuilder(defaultRootfsPath)
	}

	// TODO: Reserve fake drives here (https://github.com/firecracker-microvm/firecracker-containerd/pull/154)
	// Right now, just a single hardcoded stub drive is allocated
	driveBuilder = driveBuilder.AddDrive("/dev/null", false, func(drive *models.Drive) {
		drive.IsRootDevice = firecracker.Bool(false)
		drive.DriveID = firecracker.String("containerRootfs")
	})

	for _, drive := range req.AdditionalDrives {
		driveBuilder = addDriveFromProto(driveBuilder, drive)
	}

	cfg.Drives = driveBuilder.Build()

	// Setup network interfaces

	for _, ni := range req.NetworkInterfaces {
		cfg.NetworkInterfaces = append(cfg.NetworkInterfaces, networkConfigFromProto(ni))
	}

	return &cfg, nil
}

func (s *service) Create(requestCtx context.Context, request *taskAPI.CreateTaskRequest) (*taskAPI.CreateTaskResponse, error) {
	logger := s.logger.WithField("containerID", request.ID)

	err := s.waitVMReady()
	if err != nil {
		logger.WithError(err).Error()
		return nil, err
	}

	bundleDir := bundle.Dir(request.Bundle)
	err = s.shimDir().CreateBundleLink(request.ID, bundleDir)
	if err != nil {
		err = errors.Wrap(err, "failed to create VM dir bundle link")
		logger.WithError(err).Error()
		return nil, err
	}

	// TODO replace with a FIFO created by the plugin
	_, err = os.Stat(s.shimDir().LogFifoPath())
	if os.IsNotExist(err) {
		err = s.shimDir().CreateShimLogFifoLink(request.ID)
		if err != nil {
			err = errors.Wrap(err, "failed to create shim log fifo symlink")
			logger.WithError(err).Error()
			return nil, err
		}

		fifo, err := s.shimDir().OpenLogFifo(requestCtx)
		if err != nil {
			err = errors.Wrap(err, "failed to open shim log fifo")
			logger.WithError(err).Error()
			return nil, err
		}

		logrus.SetOutput(fifo)
	} else if err != nil {
		err = errors.Wrap(err, "failed to stat log fifo path")
		logger.WithError(err).Error()
		return nil, err
	}

	defer logPanicAndDie(log.G(requestCtx))

	logger.WithFields(logrus.Fields{
		"bundle":     request.Bundle,
		"terminal":   request.Terminal,
		"stdin":      request.Stdin,
		"stdout":     request.Stdout,
		"stderr":     request.Stderr,
		"checkpoint": request.Checkpoint,
	}).Debug("creating task")

	err = s.shimDir().CreateAddressLink(request.ID)
	if err != nil {
		err = errors.Wrap(err, "failed to create shim address symlink")
		logger.WithError(err).Error()
		return nil, err
	}

	// TODO replace with proper drive mounting after that PR is merged
	s.rootfsOnce.Do(func() {
		defer logPanicAndDie(logger)

		err := s.machine.UpdateGuestDrive(requestCtx, "containerRootfs", request.Rootfs[0].Source)
		if err != nil {
			panic(err)
		}
	})

	logger.Info("creating task")

	extraData, err := s.generateExtraData(bundleDir, runcOpts)
	if err != nil {
		err = errors.Wrap(err, "failed to generate extra data")
		logger.WithError(err).Error()
		return nil, err
	}

	taskCtx, taskCancel := context.WithCancel(s.shimCtx)
	task, err := s.taskManager.AddTask(request.ID, s.agentClient, bundleDir, extraData, cio.NewFIFOSet(cio.Config{
		Stdin:    request.Stdin,
		Stdout:   request.Stdout,
		Stderr:   request.Stderr,
		Terminal: request.Terminal,
	}, nil), taskCtx.Done(), taskCancel)
	if err != nil {
		err = errors.Wrap(err, "failed to add task")
		logger.WithError(err).Error()
		return nil, err
	}

	defer func() {
		if err != nil {
			s.taskManager.Remove(request.ID)
		}
	}()

	// Begin initializing stdio, but don't block on the initialization so we can send the Create
	// call (which will allow the stdio initialization to complete).
	// Use requestCtx as the ctx here is only used for initializing stdio, not the actual copying of io
	stdioReadyCh := task.StartStdioProxy(requestCtx, vm.FIFOtoVSock, cidDialer(s.machineCID))

	// Override the original request options with ExtraData needed by the VM Agent before sending it off
	request.Options, err = task.MarshalExtraData()
	if err != nil {
		err = errors.Wrap(err, "failed to marshal extra data")
		logger.WithError(err).Error()
		return nil, err
	}

	resp, err := task.Create(requestCtx, request)
	if err != nil {
		err = errors.Wrap(err, "failed to create task")
		logger.WithError(err).Error()
		return nil, err
	}

	// make sure stdio was initialized successfully
	err = <-stdioReadyCh
	if err != nil {
		return nil, err
	}

	logger.WithField("pid_in_vm", resp.Pid).Info("successfully created task")
	return resp, nil
}

func (s *service) Start(requestCtx context.Context, req *taskAPI.StartRequest) (*taskAPI.StartResponse, error) {
	defer logPanicAndDie(log.G(requestCtx))

	log.G(requestCtx).WithFields(logrus.Fields{"id": req.ID, "exec_id": req.ExecID}).Debug("start")
	task, err := s.taskManager.Task(req.ID)
	if err != nil {
		return nil, err
	}

	resp, err := task.Start(requestCtx, req)
	if err != nil {
		return nil, err
	}

	return resp, nil
}

func (s *service) Delete(requestCtx context.Context, req *taskAPI.DeleteRequest) (*taskAPI.DeleteResponse, error) {
	defer logPanicAndDie(log.G(requestCtx))

	log.G(requestCtx).WithFields(logrus.Fields{"id": req.ID, "exec_id": req.ExecID}).Debug("delete")

	task, err := s.taskManager.Task(req.ID)
	if err != nil {
		return nil, err
	}

	resp, err := task.Delete(requestCtx, req)
	if err != nil {
		return nil, err
	}

	s.taskManager.Remove(req.ID)

	return resp, nil
}

// Exec an additional process inside the container
func (s *service) Exec(requestCtx context.Context, req *taskAPI.ExecProcessRequest) (*ptypes.Empty, error) {
	defer logPanicAndDie(log.G(requestCtx))

	log.G(requestCtx).WithFields(logrus.Fields{"id": req.ID, "exec_id": req.ExecID}).Debug("exec")
	task, err := s.taskManager.Task(req.ID)
	if err != nil {
		return nil, err
	}

	resp, err := task.Exec(requestCtx, req)
	if err != nil {
		return nil, err
	}

	return resp, nil
}

// ResizePty of a process
func (s *service) ResizePty(requestCtx context.Context, req *taskAPI.ResizePtyRequest) (*ptypes.Empty, error) {
	defer logPanicAndDie(log.G(requestCtx))

	log.G(requestCtx).WithFields(logrus.Fields{"id": req.ID, "exec_id": req.ExecID}).Debug("resize_pty")
	task, err := s.taskManager.Task(req.ID)
	if err != nil {
		return nil, err
	}

	resp, err := task.ResizePty(requestCtx, req)
	if err != nil {
		return nil, err
	}

	return resp, nil
}

// State returns runtime state information for a process
func (s *service) State(requestCtx context.Context, req *taskAPI.StateRequest) (*taskAPI.StateResponse, error) {
	defer logPanicAndDie(log.G(requestCtx))

	log.G(requestCtx).WithFields(logrus.Fields{"id": req.ID, "exec_id": req.ExecID}).Debug("state")
	task, err := s.taskManager.Task(req.ID)
	if err != nil {
		return nil, err
	}

	resp, err := task.State(requestCtx, req)
	if err != nil {
		return nil, err
	}

	return resp, nil
}

// Pause the container
func (s *service) Pause(requestCtx context.Context, req *taskAPI.PauseRequest) (*ptypes.Empty, error) {
	defer logPanicAndDie(log.G(requestCtx))

	log.G(requestCtx).WithField("id", req.ID).Debug("pause")
	task, err := s.taskManager.Task(req.ID)
	if err != nil {
		return nil, err
	}

	resp, err := task.Pause(requestCtx, req)
	if err != nil {
		return nil, err
	}

	return resp, nil
}

// Resume the container
func (s *service) Resume(requestCtx context.Context, req *taskAPI.ResumeRequest) (*ptypes.Empty, error) {
	defer logPanicAndDie(log.G(requestCtx))

	log.G(requestCtx).WithField("id", req.ID).Debug("resume")
	task, err := s.taskManager.Task(req.ID)
	if err != nil {
		return nil, err
	}

	resp, err := task.Resume(requestCtx, req)
	if err != nil {
		return nil, err
	}

	return resp, nil
}

// Kill a process with the provided signal
func (s *service) Kill(requestCtx context.Context, req *taskAPI.KillRequest) (*ptypes.Empty, error) {
	defer logPanicAndDie(log.G(requestCtx))

	log.G(requestCtx).WithFields(logrus.Fields{"id": req.ID, "exec_id": req.ExecID}).Debug("kill")
	task, err := s.taskManager.Task(req.ID)
	if err != nil {
		return nil, err
	}

	resp, err := task.Kill(requestCtx, req)
	if err != nil {
		return nil, err
	}
	return resp, nil
}

// Pids returns all pids inside the container
func (s *service) Pids(requestCtx context.Context, req *taskAPI.PidsRequest) (*taskAPI.PidsResponse, error) {
	defer logPanicAndDie(log.G(requestCtx))

	log.G(requestCtx).WithField("id", req.ID).Debug("pids")
	task, err := s.taskManager.Task(req.ID)
	if err != nil {
		return nil, err
	}

	resp, err := task.Pids(requestCtx, req)
	if err != nil {
		return nil, err
	}

	return resp, nil
}

// CloseIO of a process
func (s *service) CloseIO(requestCtx context.Context, req *taskAPI.CloseIORequest) (*ptypes.Empty, error) {
	defer logPanicAndDie(log.G(requestCtx))

	log.G(requestCtx).WithFields(logrus.Fields{"id": req.ID, "exec_id": req.ExecID}).Debug("close_io")
	task, err := s.taskManager.Task(req.ID)
	if err != nil {
		return nil, err
	}

	resp, err := task.CloseIO(requestCtx, req)
	if err != nil {
		return nil, err
	}

	return resp, nil
}

// Checkpoint the container
func (s *service) Checkpoint(requestCtx context.Context, req *taskAPI.CheckpointTaskRequest) (*ptypes.Empty, error) {
	defer logPanicAndDie(log.G(requestCtx))

	log.G(requestCtx).WithFields(logrus.Fields{"id": req.ID, "path": req.Path}).Info("checkpoint")
	task, err := s.taskManager.Task(req.ID)
	if err != nil {
		return nil, err
	}

	resp, err := task.Checkpoint(requestCtx, req)
	if err != nil {
		return nil, err
	}

	return resp, nil
}

// Connect returns shim information such as the shim's pid
func (s *service) Connect(requestCtx context.Context, req *taskAPI.ConnectRequest) (*taskAPI.ConnectResponse, error) {
	defer logPanicAndDie(log.G(requestCtx))

	log.G(requestCtx).WithField("id", req.ID).Debug("connect")
	task, err := s.taskManager.Task(req.ID)
	if err != nil {
		return nil, err
	}

	resp, err := task.Connect(requestCtx, req)
	if err != nil {
		return nil, err
	}

	return resp, nil
}

func (s *service) Shutdown(requestCtx context.Context, req *taskAPI.ShutdownRequest) (*ptypes.Empty, error) {
	defer logPanicAndDie(log.G(requestCtx))

	s.logger.WithFields(logrus.Fields{"id": req.ID, "now": req.Now}).Debug("shutdown")

	// If we are still managing containers, don't shutdown
	if s.taskManager.TaskCount() > 0 {
		return &ptypes.Empty{}, nil
	}

	defer s.shimCancel()

	err := s.waitVMReady()
	if err != nil {
		s.logger.WithError(err).Error()
		return nil, err
	}

	_, err = s.agentClient.Shutdown(requestCtx, req)
	if err != nil {
		return nil, err
	}

	if _, err := s.StopVM(requestCtx, &proto.StopVMRequest{VMID: s.vmID}); err != nil {
		s.logger.WithError(err).Error("failed to stop VM")
		return nil, err
	}

	return &ptypes.Empty{}, nil
}

func (s *service) Stats(requestCtx context.Context, req *taskAPI.StatsRequest) (*taskAPI.StatsResponse, error) {
	defer logPanicAndDie(log.G(requestCtx))

	log.G(requestCtx).WithField("id", req.ID).Debug("stats")
	task, err := s.taskManager.Task(req.ID)
	if err != nil {
		return nil, err
	}

	resp, err := task.Stats(requestCtx, req)
	if err != nil {
		return nil, err
	}

	return resp, nil
}

// Update a running container
func (s *service) Update(requestCtx context.Context, req *taskAPI.UpdateTaskRequest) (*ptypes.Empty, error) {
	defer logPanicAndDie(log.G(requestCtx))

	log.G(requestCtx).WithField("id", req.ID).Debug("update")
	task, err := s.taskManager.Task(req.ID)
	if err != nil {
		return nil, err
	}

	resp, err := task.Update(requestCtx, req)
	if err != nil {
		return nil, err
	}

	return resp, nil
}

// Wait for a process to exit
func (s *service) Wait(requestCtx context.Context, req *taskAPI.WaitRequest) (*taskAPI.WaitResponse, error) {
	defer logPanicAndDie(log.G(requestCtx))

	log.G(requestCtx).WithFields(logrus.Fields{"id": req.ID, "exec_id": req.ExecID}).Debug("wait")
	task, err := s.taskManager.Task(req.ID)
	if err != nil {
		return nil, err
	}

	resp, err := task.Wait(requestCtx, req)
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

func dialVsock(requestCtx context.Context, contextID, port uint32) (net.Conn, error) {
	// VM should start within 200ms, vsock dial will make retries at 100ms, 200ms, 400ms, 800ms, 1.6s, 3.2s, 6.4s
	const (
		retryCount      = 7
		initialDelay    = 100 * time.Millisecond
		delayMultiplier = 2
	)

	var lastErr error
	var currentDelay = initialDelay

	for i := 1; i <= retryCount; i++ {
		select {
		case <-requestCtx.Done():
			return nil, requestCtx.Err()
		default:
			conn, err := vsock.Dial(contextID, port)
			if err == nil {
				log.G(requestCtx).WithField("connection", conn).Debug("Dial succeeded")
				return conn, nil
			}

			log.G(requestCtx).WithError(err).Warnf("vsock dial failed (attempt %d of %d), will retry in %s", i, retryCount, currentDelay)
			time.Sleep(currentDelay)

			lastErr = err
			currentDelay *= delayMultiplier
		}
	}

	log.G(requestCtx).WithError(lastErr).WithFields(logrus.Fields{"context_id": contextID, "port": port}).Error("vsock dial failed")
	return nil, lastErr
}

func cidDialer(cid uint32) vm.VSockConnector {
	return func(requestCtx context.Context, port uint32) (net.Conn, error) {
		return dialVsock(requestCtx, cid, port)
	}
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

func (s *service) monitorTaskExit(exitAfterAllTasksGone bool) {
	exitEvents, exitEventErrs := s.eventExchange.Subscribe(s.shimCtx, fmt.Sprintf(`topic=="%s"`, runtime.TaskExitEventTopic))

	var err error
	defer func() {
		if err != nil && err != context.Canceled {
			s.logger.WithError(err).Error("error while waiting for task exit events")
		}
	}()

	for {
		select {
		case envelope := <-exitEvents:
			unmarshaledEvent, err := typeurl.UnmarshalAny(envelope.Event)
			if err != nil {
				s.logger.WithError(err).Error("error unmarshaling event")
				continue
			}

			switch event := unmarshaledEvent.(type) {
			case *eventsAPI.TaskExit:
				logger := s.logger.WithField("id", event.ContainerID)
				logger.Debug("received container exit event")

				s.taskManager.Remove(event.ContainerID)

				if exitAfterAllTasksGone {
					// If we have no more containers, shutdown. If we still have containers left,
					// this will be a no-op
					_, err = s.Shutdown(s.shimCtx, &taskAPI.ShutdownRequest{})
					if err != nil {
						logger.WithError(err).Fatal("failed to shutdown after container exit")
					}
				}

			default:
				s.logger.Error("unexpected non-exit event type published on exit event channel")
			}

		case err = <-exitEventErrs:
			if err != nil {
				s.logger.WithError(err).Error("event error channel published to")
			}

		case <-s.shimCtx.Done():
			err = s.shimCtx.Err()
			return

		}
	}
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
