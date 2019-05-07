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
	"os/exec"
	"path/filepath"
	"runtime/debug"
	"strings"
	"sync"
	"syscall"
	"time"
	"unsafe"

	// disable gosec check for math/rand. We just need a random starting
	// place to start looking for CIDs; no need for cryptographically
	// secure randomness
	"math/rand" // #nosec

	eventsAPI "github.com/containerd/containerd/api/events"
	"github.com/containerd/containerd/cio"
	"github.com/containerd/containerd/events"
	"github.com/containerd/containerd/events/exchange"
	"github.com/containerd/containerd/log"
	"github.com/containerd/containerd/namespaces"
	"github.com/containerd/containerd/runtime"
	"github.com/containerd/containerd/runtime/v2/shim"
	taskAPI "github.com/containerd/containerd/runtime/v2/task"
	"github.com/containerd/ttrpc"
	"github.com/containerd/typeurl"
	"github.com/firecracker-microvm/firecracker-go-sdk"
	models "github.com/firecracker-microvm/firecracker-go-sdk/client/models"
	"github.com/gofrs/uuid"
	ptypes "github.com/gogo/protobuf/types"
	"github.com/mdlayher/vsock"
	"github.com/pkg/errors"
	"github.com/sirupsen/logrus"
	"golang.org/x/sys/unix"

	"github.com/firecracker-microvm/firecracker-containerd/eventbridge"
	"github.com/firecracker-microvm/firecracker-containerd/internal/bundle"
	"github.com/firecracker-microvm/firecracker-containerd/internal/vm"
	"github.com/firecracker-microvm/firecracker-containerd/proto"
)

func init() {
	rand.Seed(time.Now().UnixNano())
}

const (
	defaultVsockPort     = 10789
	minVsockIOPort       = uint32(11000)
	supportedMountFSType = "ext4"

	vmIDEnvVarKey = "FIRECRACKER_VM_ID"

	varRunDir = "/var/run/firecracker-containerd"
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

	server        *ttrpc.Server
	publish       events.Publisher
	eventExchange *exchange.Exchange
	namespace     string

	vmID   string
	config *Config

	startVMMutex sync.Mutex
	agentStarted bool
	agentClient  taskAPI.TaskService

	machine          *firecracker.Machine
	machineCID       uint32
	vsockIOPortCount uint32
	vsockPortMu      sync.Mutex
}

func shimOpts(ctx context.Context) (*shim.Opts, error) {
	opts, ok := ctx.Value(shim.OptsKey{}).(shim.Opts)
	if !ok {
		return nil, errors.New("failed to parse containerd shim opts from context")
	}

	return &opts, nil
}

// NewService creates new runtime shim.
func NewService(ctx context.Context, id string, remotePublisher events.Publisher) (shim.Shim, error) {
	server, err := newServer()
	if err != nil {
		return nil, err
	}

	config, err := LoadConfig("")
	if err != nil {
		return nil, err
	}

	if !config.Debug {
		opts, err := shimOpts(ctx)
		if err != nil {
			return nil, err
		}

		config.Debug = opts.Debug
	}

	namespace, ok := namespaces.Namespace(ctx)
	if !ok {
		namespace = namespaces.Default
	}

	eventExchange := exchange.NewExchange()

	// Republish each event received on our exchange to the provided remote publisher.
	// TODO ideally we would be forwarding events instead of re-publishing them, which would
	// preserve the events' original timestamps and namespaces. However, as of this writing,
	// the containerd v2 runtime model only provides a shim with a publisher, not a forwarder,
	// so we have to republish for now.
	go func() {
		if err := <-eventbridge.Republish(ctx, eventExchange, remotePublisher); err != nil && err != context.Canceled {
			log.G(ctx).WithError(err).Error("error while republishing events")
		}
	}()

	s := &service{
		taskManager: vm.NewTaskManager(),

		server:        server,
		publish:       remotePublisher,
		eventExchange: eventExchange,
		namespace:     namespace,

		vmID:   os.Getenv(vmIDEnvVarKey),
		config: config,
	}

	return s, nil
}

// vmDir holds files, sockets and FIFOs scoped to a single given VM.
// It is unique per-VM and containerd namespace.
func (s *service) vmDir() vm.Dir {
	return vm.Dir(filepath.Join(varRunDir, s.namespace, s.vmID))
}

// shimSocketAddress is the abstract unix socket path at which our shim serves its API
// It is unique per-VM and containerd namespace.
func (s *service) shimSocketAddress(ctx context.Context) (string, error) {
	return shim.SocketAddress(ctx, s.vmID)
}

func (s *service) newShim(ctx context.Context, containerdBinary, containerdAddress string, shimSocket *net.UnixListener) (*exec.Cmd, error) {
	ns, err := namespaces.NamespaceRequired(ctx)
	if err != nil {
		return nil, err
	}

	self, err := os.Executable()
	if err != nil {
		return nil, err
	}

	args := []string{
		"-namespace", ns,
		"-address", containerdAddress,
		"-publish-binary", containerdBinary,
	}

	if s.config.Debug {
		args = append(args, "-debug")
	}

	cmd := exec.Command(self, args...)

	cmd.Dir = s.vmDir().RootPath()

	cmd.Env = append(os.Environ(),
		fmt.Sprintf("%s=%s", vmIDEnvVarKey, s.vmID))

	shimSocketFile, err := shimSocket.File()
	if err != nil {
		return nil, err
	}
	cmd.ExtraFiles = append(cmd.ExtraFiles, shimSocketFile)

	cmd.SysProcAttr = &syscall.SysProcAttr{
		Setpgid: true,
	}

	err = cmd.Start()
	if err != nil {
		return nil, err
	}

	// make sure to wait after start
	go func() {
		log.G(ctx).WithField("vmID", s.vmID).Debug("waiting on shim process")
		waitErr := cmd.Wait()
		log.G(ctx).WithError(waitErr).WithField("vmID", s.vmID).Debug("completed waiting on shim process")
	}()

	err = shim.SetScore(cmd.Process.Pid)
	if err != nil {
		log.G(ctx).WithError(err).WithField("vmID", s.vmID).Error("failed to set OOM score on shim")
		return nil, errors.Wrap(err, "failed to set OOM Score on shim")
	}

	return cmd, nil
}

func isEADDRINUSE(err error) bool {
	return err != nil && strings.Contains(err.Error(), "address already in use")
}

func (s *service) StartShim(ctx context.Context, containerID, containerdBinary, containerdAddress string) (string, error) {
	log.G(ctx).WithField("id", containerID).Debug("StartShim")

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

	if s.vmID == "" {
		// If here, no VMID has been provided by the client for this container, so auto-generate a new one.
		// This results in a default behavior of running each container in its own VM if not otherwise
		// specified by the client.
		uuid, err := uuid.NewV4()
		if err != nil {
			return "", errors.Wrap(err, "failed to generate UUID for VMID")
		}

		s.vmID = uuid.String()
	}

	// We determine if there is already a shim managing a VM with the current VMID by attempting
	// to listen on the abstract socket address (which is parameterized by VMID). If we get
	// EADDRINUSE, then we assume there is already a shim for the VM and just hand off to that
	// one. This is in line with the approach used by containerd's reference runC shim v2
	// implementation (which is also designed to manage multiple containers from a single shim
	// process)
	shimSocketAddress, err := s.shimSocketAddress(ctx)
	if err != nil {
		return "", err
	}

	shimSocket, err := shim.NewSocket(shimSocketAddress)
	if isEADDRINUSE(err) {
		// There's already a shim for this VMID, so just hand off to it
		err = s.vmDir().CreateBundleLink(containerID, bundleDir)
		if err != nil {
			return "", err
		}

		err = s.vmDir().CreateAddressLink(containerID)
		if err != nil {
			return "", err
		}

		return shimSocketAddress, nil
	} else if err != nil {
		return "", errors.Wrapf(err, "failed to create new shim socket at address \"%s\"", shimSocketAddress)
	}

	// If we're here, there is no pre-existing shim for this VMID, so we spawn a new one
	defer func() {
		log.G(ctx).WithField("id", containerID).Debug("closing shim socket")
		shimSocket.Close()
	}()

	err = s.vmDir().Create()
	if err != nil {
		return "", err
	}

	err = s.vmDir().CreateBundleLink(containerID, bundleDir)
	if err != nil {
		return "", err
	}

	cmd, err := s.newShim(ctx, containerdBinary, containerdAddress, shimSocket)
	if err != nil {
		log.G(ctx).WithError(err).WithField("id", containerID).Error("Failed to start new shim")
		return "", err
	}

	defer func() {
		if err != nil {
			log.G(ctx).WithError(err).WithField("id", containerID).Error("killing shim process after error")
			cmd.Process.Kill()
		}
	}()

	err = s.vmDir().WriteAddress(shimSocketAddress)
	if err != nil {
		log.G(ctx).WithError(err).WithField("id", containerID).Error("failed to write address")
		return "", err
	}

	err = s.vmDir().CreateAddressLink(containerID)
	if err != nil {
		return "", err
	}

	err = s.vmDir().CreateShimLogFifoLink(containerID)
	if err != nil {
		return "", err
	}

	return shimSocketAddress, nil
}

func logPanicAndDie(logger *logrus.Entry) {
	if err := recover(); err != nil {
		logger.WithError(err.(error)).Fatalf("panic: %s", string(debug.Stack()))
	}
}

func parseCreateTaskOpts(ctx context.Context, opts *ptypes.Any) (*proto.FirecrackerConfig, *ptypes.Any, error) {
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

func (s *service) Create(ctx context.Context, request *taskAPI.CreateTaskRequest) (*taskAPI.CreateTaskResponse, error) {
	defer logPanicAndDie(log.G(ctx))

	log.G(ctx).WithFields(logrus.Fields{
		"id":         request.ID,
		"bundle":     request.Bundle,
		"terminal":   request.Terminal,
		"stdin":      request.Stdin,
		"stdout":     request.Stdout,
		"stderr":     request.Stderr,
		"checkpoint": request.Checkpoint,
	}).Debug("creating task")

	var err error
	var firecrackerConfig *proto.FirecrackerConfig
	var runcOpts *ptypes.Any

	if request.Options != nil {
		firecrackerConfig, runcOpts, err = parseCreateTaskOpts(ctx, request.Options)
		if err != nil {
			log.G(ctx).WithFields(logrus.Fields{
				"id":      request.ID,
				"options": request.Options,
				"error":   err,
			}).Error("failed to unmarshal task create request options")
			return nil, errors.Wrapf(err, "unmarshaling task create request options")
		}
	}

	s.startVMMutex.Lock()
	if !s.agentStarted {
		log.G(ctx).WithField("id", request.ID).Debug("calling startVM")
		client, err := s.startVM(ctx, request, firecrackerConfig)
		if err != nil {
			s.startVMMutex.Unlock()
			log.G(ctx).WithError(err).WithField("id", request.ID).Error("failed to start VM")
			return nil, err
		}

		s.agentClient = client
		s.agentStarted = true
	}
	s.startVMMutex.Unlock()

	log.G(ctx).WithField("id", request.ID).Info("creating task")

	bundleDir := bundle.Dir(request.Bundle)
	extraData, err := s.generateExtraData(bundleDir, runcOpts)
	if err != nil {
		return nil, err
	}

	task, err := s.taskManager.AddTask(ctx, request.ID, s.agentClient, bundleDir, extraData, cio.NewFIFOSet(cio.Config{
		Stdin:    request.Stdin,
		Stdout:   request.Stdout,
		Stderr:   request.Stderr,
		Terminal: request.Terminal,
	}, nil))
	if err != nil {
		return nil, err
	}

	defer func() {
		if err != nil {
			s.taskManager.Remove(ctx, request.ID)
		}
	}()

	// Begin initializing stdio, but don't block on the initialization so we can send the Create
	// call (which will allow the stdio initialization to complete)
	stdioReadyCh := task.StartStdioProxy(ctx, vm.FIFOtoVSock, cidDialer(s.machineCID))

	// Override the original request options with ExtraData needed by the VM Agent before sending it off
	request.Options, err = task.MarshalExtraData()
	if err != nil {
		return nil, err
	}

	resp, err := task.Create(ctx, request)
	if err != nil {
		log.G(ctx).WithError(err).Error("create failed")
		return nil, err
	}

	// make sure stdio was initialized successfully
	err = <-stdioReadyCh
	if err != nil {
		return nil, err
	}

	log.G(ctx).WithField("id", request.ID).WithField("pid", resp.Pid).Info("successfully created task")
	return resp, nil
}

func (s *service) Start(ctx context.Context, req *taskAPI.StartRequest) (*taskAPI.StartResponse, error) {
	defer logPanicAndDie(log.G(ctx))

	log.G(ctx).WithFields(logrus.Fields{"id": req.ID, "exec_id": req.ExecID}).Debug("start")
	task, err := s.taskManager.Task(req.ID)
	if err != nil {
		return nil, err
	}

	resp, err := task.Start(ctx, req)
	if err != nil {
		return nil, err
	}

	return resp, nil
}

func (s *service) Delete(ctx context.Context, req *taskAPI.DeleteRequest) (*taskAPI.DeleteResponse, error) {
	defer logPanicAndDie(log.G(ctx))

	log.G(ctx).WithFields(logrus.Fields{"id": req.ID, "exec_id": req.ExecID}).Debug("delete")

	task, err := s.taskManager.Task(req.ID)
	if err != nil {
		return nil, err
	}

	resp, err := task.Delete(ctx, req)
	if err != nil {
		return nil, err
	}

	s.taskManager.Remove(ctx, req.ID)

	return resp, nil
}

// Exec an additional process inside the container
func (s *service) Exec(ctx context.Context, req *taskAPI.ExecProcessRequest) (*ptypes.Empty, error) {
	defer logPanicAndDie(log.G(ctx))

	log.G(ctx).WithFields(logrus.Fields{"id": req.ID, "exec_id": req.ExecID}).Debug("exec")
	task, err := s.taskManager.Task(req.ID)
	if err != nil {
		return nil, err
	}

	resp, err := task.Exec(ctx, req)
	if err != nil {
		return nil, err
	}

	return resp, nil
}

// ResizePty of a process
func (s *service) ResizePty(ctx context.Context, req *taskAPI.ResizePtyRequest) (*ptypes.Empty, error) {
	defer logPanicAndDie(log.G(ctx))

	log.G(ctx).WithFields(logrus.Fields{"id": req.ID, "exec_id": req.ExecID}).Debug("resize_pty")
	task, err := s.taskManager.Task(req.ID)
	if err != nil {
		return nil, err
	}

	resp, err := task.ResizePty(ctx, req)
	if err != nil {
		return nil, err
	}

	return resp, nil
}

// State returns runtime state information for a process
func (s *service) State(ctx context.Context, req *taskAPI.StateRequest) (*taskAPI.StateResponse, error) {
	defer logPanicAndDie(log.G(ctx))

	log.G(ctx).WithFields(logrus.Fields{"id": req.ID, "exec_id": req.ExecID}).Debug("state")
	task, err := s.taskManager.Task(req.ID)
	if err != nil {
		return nil, err
	}

	resp, err := task.State(ctx, req)
	if err != nil {
		return nil, err
	}

	return resp, nil
}

// Pause the container
func (s *service) Pause(ctx context.Context, req *taskAPI.PauseRequest) (*ptypes.Empty, error) {
	defer logPanicAndDie(log.G(ctx))

	log.G(ctx).WithField("id", req.ID).Debug("pause")
	task, err := s.taskManager.Task(req.ID)
	if err != nil {
		return nil, err
	}

	resp, err := task.Pause(ctx, req)
	if err != nil {
		return nil, err
	}

	return resp, nil
}

// Resume the container
func (s *service) Resume(ctx context.Context, req *taskAPI.ResumeRequest) (*ptypes.Empty, error) {
	defer logPanicAndDie(log.G(ctx))

	log.G(ctx).WithField("id", req.ID).Debug("resume")
	task, err := s.taskManager.Task(req.ID)
	if err != nil {
		return nil, err
	}

	resp, err := task.Resume(ctx, req)
	if err != nil {
		return nil, err
	}

	return resp, nil
}

// Kill a process with the provided signal
func (s *service) Kill(ctx context.Context, req *taskAPI.KillRequest) (*ptypes.Empty, error) {
	defer logPanicAndDie(log.G(ctx))

	log.G(ctx).WithFields(logrus.Fields{"id": req.ID, "exec_id": req.ExecID}).Debug("kill")
	task, err := s.taskManager.Task(req.ID)
	if err != nil {
		return nil, err
	}

	resp, err := task.Kill(ctx, req)
	if err != nil {
		return nil, err
	}
	return resp, nil
}

// Pids returns all pids inside the container
func (s *service) Pids(ctx context.Context, req *taskAPI.PidsRequest) (*taskAPI.PidsResponse, error) {
	defer logPanicAndDie(log.G(ctx))

	log.G(ctx).WithField("id", req.ID).Debug("pids")
	task, err := s.taskManager.Task(req.ID)
	if err != nil {
		return nil, err
	}

	resp, err := task.Pids(ctx, req)
	if err != nil {
		return nil, err
	}

	return resp, nil
}

// CloseIO of a process
func (s *service) CloseIO(ctx context.Context, req *taskAPI.CloseIORequest) (*ptypes.Empty, error) {
	defer logPanicAndDie(log.G(ctx))

	log.G(ctx).WithFields(logrus.Fields{"id": req.ID, "exec_id": req.ExecID}).Debug("close_io")
	task, err := s.taskManager.Task(req.ID)
	if err != nil {
		return nil, err
	}

	resp, err := task.CloseIO(ctx, req)
	if err != nil {
		return nil, err
	}

	return resp, nil
}

// Checkpoint the container
func (s *service) Checkpoint(ctx context.Context, req *taskAPI.CheckpointTaskRequest) (*ptypes.Empty, error) {
	defer logPanicAndDie(log.G(ctx))

	log.G(ctx).WithFields(logrus.Fields{"id": req.ID, "path": req.Path}).Info("checkpoint")
	task, err := s.taskManager.Task(req.ID)
	if err != nil {
		return nil, err
	}

	resp, err := task.Checkpoint(ctx, req)
	if err != nil {
		return nil, err
	}

	return resp, nil
}

// Connect returns shim information such as the shim's pid
func (s *service) Connect(ctx context.Context, req *taskAPI.ConnectRequest) (*taskAPI.ConnectResponse, error) {
	defer logPanicAndDie(log.G(ctx))

	log.G(ctx).WithField("id", req.ID).Debug("connect")
	task, err := s.taskManager.Task(req.ID)
	if err != nil {
		return nil, err
	}

	resp, err := task.Connect(ctx, req)
	if err != nil {
		return nil, err
	}

	return resp, nil
}

func (s *service) Shutdown(ctx context.Context, req *taskAPI.ShutdownRequest) (*ptypes.Empty, error) {
	defer logPanicAndDie(log.G(ctx))

	log.G(ctx).WithFields(logrus.Fields{"id": req.ID, "now": req.Now}).Debug("shutdown")

	// If we are still managing containers, don't shutdown
	if s.taskManager.TaskCount() > 0 {
		return &ptypes.Empty{}, nil
	}

	_, err := s.agentClient.Shutdown(ctx, req)
	if err != nil {
		return nil, err
	}

	log.G(ctx).Debug("stopping VM")
	if err := s.stopVM(); err != nil {
		log.G(ctx).WithError(err).Error("failed to stop VM")
		return nil, err
	}

	err = os.RemoveAll(s.vmDir().RootPath())
	if err != nil {
		log.G(ctx).WithField("path", s.vmDir().RootPath()).WithError(err).Error("failed to remove VM dir during shutdown")
	}

	// Exit to avoid 'zombie' shim processes
	defer os.Exit(0)
	log.G(ctx).Debug("stopping runtime")
	return &ptypes.Empty{}, nil
}

func (s *service) Stats(ctx context.Context, req *taskAPI.StatsRequest) (*taskAPI.StatsResponse, error) {
	defer logPanicAndDie(log.G(ctx))

	log.G(ctx).WithField("id", req.ID).Debug("stats")
	task, err := s.taskManager.Task(req.ID)
	if err != nil {
		return nil, err
	}

	resp, err := task.Stats(ctx, req)
	if err != nil {
		return nil, err
	}

	return resp, nil
}

// Update a running container
func (s *service) Update(ctx context.Context, req *taskAPI.UpdateTaskRequest) (*ptypes.Empty, error) {
	defer logPanicAndDie(log.G(ctx))

	log.G(ctx).WithField("id", req.ID).Debug("update")
	task, err := s.taskManager.Task(req.ID)
	if err != nil {
		return nil, err
	}

	resp, err := task.Update(ctx, req)
	if err != nil {
		return nil, err
	}

	return resp, nil
}

// Wait for a process to exit
func (s *service) Wait(ctx context.Context, req *taskAPI.WaitRequest) (*taskAPI.WaitResponse, error) {
	defer logPanicAndDie(log.G(ctx))

	log.G(ctx).WithFields(logrus.Fields{"id": req.ID, "exec_id": req.ExecID}).Debug("wait")
	task, err := s.taskManager.Task(req.ID)
	if err != nil {
		return nil, err
	}

	resp, err := task.Wait(ctx, req)
	if err != nil {
		return nil, err
	}

	return resp, nil
}

func (s *service) Cleanup(ctx context.Context) (*taskAPI.DeleteResponse, error) {
	defer logPanicAndDie(log.G(ctx))

	log.G(ctx).Debug("cleanup")
	// Destroy VM/etc here?
	// copied from runcs impl, nothing to cleanup atm
	return &taskAPI.DeleteResponse{
		ExitedAt:   time.Now(),
		ExitStatus: 128 + uint32(unix.SIGKILL),
	}, nil
}

func newServer() (*ttrpc.Server, error) {
	return ttrpc.NewServer(ttrpc.WithServerHandshaker(ttrpc.UnixSocketRequireSameUser()))
}

func dialVsock(ctx context.Context, contextID, port uint32) (net.Conn, error) {
	// VM should start within 200ms, vsock dial will make retries at 100ms, 200ms, 400ms, 800ms, 1.6s, 3.2s, 6.4s
	const (
		retryCount      = 7
		initialDelay    = 100 * time.Millisecond
		delayMultiplier = 2
	)

	var lastErr error
	var currentDelay = initialDelay
	for i := 1; i <= retryCount; i++ {
		conn, err := vsock.Dial(contextID, port)
		if err == nil {
			log.G(ctx).WithField("connection", conn).Debug("Dial succeeded")
			return conn, nil
		}

		log.G(ctx).WithError(err).Warnf("vsock dial failed (attempt %d of %d), will retry in %s", i, retryCount, currentDelay)
		time.Sleep(currentDelay)

		lastErr = err
		currentDelay *= delayMultiplier
	}

	log.G(ctx).WithError(lastErr).WithFields(logrus.Fields{"context_id": contextID, "port": port}).Error("vsock dial failed")
	return nil, lastErr
}

func cidDialer(cid uint32) vm.VSockConnector {
	return func(ctx context.Context, port uint32) (net.Conn, error) {
		return dialVsock(ctx, cid, port)
	}
}

// findNextAvailableVsockCID finds first available vsock context ID.
// It uses VHOST_VSOCK_SET_GUEST_CID ioctl which allows some CID ranges to be statically reserved in advance.
// The ioctl fails with EADDRINUSE if cid is already taken and with EINVAL if the CID is invalid.
// Taken from https://bugzilla.redhat.com/show_bug.cgi?id=1291851
func findNextAvailableVsockCID(ctx context.Context) (uint32, error) {
	const (
		// Corresponds to VHOST_VSOCK_SET_GUEST_CID in vhost.h
		ioctlVsockSetGuestCID = uintptr(0x4008AF60)
		// 0, 1 and 2 are reserved CIDs, see http://man7.org/linux/man-pages/man7/vsock.7.html
		minCID          = 3
		maxCID          = math.MaxUint32
		vsockDevicePath = "/dev/vhost-vsock"
	)

	file, err := os.OpenFile(vsockDevicePath, syscall.O_RDWR, 0600)
	if err != nil {
		return 0, errors.Wrap(err, "failed to open vsock device")
	}
	defer file.Close()

	// Start at a random ID to minimize chances of conflicts when shims are racing each other here
	// TODO the actual fix to this problem will come when the shim is updated to just use the FC-control plugin,
	// this current implementation is a very short-term bandaid.
	start := rand.Intn(maxCID - minCID)
	for n := 0; n < maxCID-minCID; n++ {
		cid := minCID + ((start + n) % (maxCID - minCID))
		select {
		case <-ctx.Done():
			return 0, ctx.Err()
		default:
			_, _, err = sysCall(
				unix.SYS_IOCTL,
				file.Fd(),
				ioctlVsockSetGuestCID,
				uintptr(unsafe.Pointer(&cid)))

			switch err {
			case unix.Errno(0):
				return uint32(cid), nil
			case unix.EADDRINUSE:
				// ID is already taken, try next one
				continue
			default:
				// Fail if we get an error we don't expect
				return 0, err
			}
		}
	}

	return 0, errors.New("couldn't find any available vsock context id")
}

// TODO: replace startVM with calls to the FC-control plugin
func (s *service) startVM(ctx context.Context,
	request *taskAPI.CreateTaskRequest,
	vmConfig *proto.FirecrackerConfig,
) (taskAPI.TaskService, error) {
	log.G(ctx).Info("starting VM")

	cid, err := findNextAvailableVsockCID(ctx)
	if err != nil {
		return nil, err
	}

	cfg := firecracker.Config{
		SocketPath:      s.vmDir().FirecrackerSockPath(),
		VsockDevices:    []firecracker.VsockDevice{{Path: "root", CID: cid}},
		KernelImagePath: s.config.KernelImagePath,
		KernelArgs:      s.config.KernelArgs,
		MachineCfg: models.MachineConfiguration{
			VcpuCount:   int64(s.config.CPUCount),
			CPUTemplate: models.CPUTemplate(s.config.CPUTemplate),
			MemSizeMib:  256,
		},
		LogFifo:     s.vmDir().FirecrackerLogFifoPath(),
		LogLevel:    s.config.LogLevel,
		MetricsFifo: s.vmDir().FirecrackerMetricsFifoPath(),
		Debug:       s.config.Debug,
	}

	driveBuilder := firecracker.NewDrivesBuilder(s.config.RootDrive)
	// Attach block devices passed from snapshotter
	for _, mnt := range request.Rootfs {
		if mnt.Type != supportedMountFSType {
			return nil, errors.Errorf("unsupported mount type '%s', expected '%s'", mnt.Type, supportedMountFSType)
		}

		driveBuilder = driveBuilder.AddDrive(mnt.Source, false)
	}
	// Override config provided in task's create opts if any.
	// Note: We've chosen to override here instead of merging in order to
	// provide a cleaner, simpler interface to reason about for clients.
	// Any config provided by clients for create task opts will always override
	// the default config generated by the runtime.
	cfg, driveBuilder, err = overrideVMConfigFromTaskOpts(cfg, vmConfig, driveBuilder)
	if err != nil {
		return nil, errors.Wrap(err, "failed to build VM options")
	}

	cfg.Drives = driveBuilder.Build()
	cmd := firecracker.VMCommandBuilder{}.
		WithBin(s.config.FirecrackerBinaryPath).
		WithSocketPath(s.vmDir().FirecrackerSockPath()).
		Build(ctx)
	machineOpts := []firecracker.Opt{
		firecracker.WithProcessRunner(cmd),
		firecracker.WithLogger(log.G(ctx)),
	}

	vmmCtx, vmmCancel := context.WithCancel(context.Background())
	defer vmmCancel()
	s.machine, err = firecracker.NewMachine(vmmCtx, cfg, machineOpts...)
	if err != nil {
		return nil, errors.Wrap(err, "failed to create firecracker config")
	}
	s.machineCID = cid

	log.G(ctx).Info("starting instance")
	if err := s.machine.Start(vmmCtx); err != nil {
		return nil, errors.Wrap(err, "failed to start firecracker VM")
	}

	log.G(ctx).Info("calling agent")
	conn, err := dialVsock(ctx, cid, defaultVsockPort)
	if err != nil {
		s.stopVM()
		return nil, errors.Wrap(err, "failed to dial vsock")
	}

	log.G(ctx).Info("creating clients")
	rpcClient := ttrpc.NewClient(conn)
	rpcClient.OnClose(func() { conn.Close() })
	apiClient := taskAPI.NewTaskClient(rpcClient)
	eventBridgeClient := eventbridge.NewGetterClient(rpcClient)

	go func() {
		// Connect the agent's event exchange to our own own event exchange
		// using the eventbridge client. All events that are published on the
		// agent's exchange will also be published on our own
		if err := <-eventbridge.Attach(ctx, eventBridgeClient, s.eventExchange); err != nil && err != context.Canceled {
			log.G(ctx).WithError(err).Error("error while forwarding events from VM agent")
		}
	}()

	go s.monitorTaskExit(ctx)

	return apiClient, nil
}

func (s *service) stopVM() error {
	return s.machine.StopVMM()
}

func (s *service) monitorTaskExit(ctx context.Context) {
	logger := log.G(ctx)
	exitEvents, exitEventErrs := s.eventExchange.Subscribe(ctx, fmt.Sprintf(`topic=="%s"`, runtime.TaskExitEventTopic))

	var err error
	defer func() {
		if err != nil && err != context.Canceled {
			logger.WithError(err).Error("error while waiting for task exit events")
		}
	}()

	for {
		select {
		case envelope := <-exitEvents:
			unmarshaledEvent, err := typeurl.UnmarshalAny(envelope.Event)
			if err != nil {
				logger.WithError(err).Error("error unmarshaling event")
				continue
			}

			switch event := unmarshaledEvent.(type) {
			case *eventsAPI.TaskExit:
				s.taskManager.Remove(ctx, event.ContainerID)

				// If we have no more containers, shutdown. If we still have containers left,
				// this will be a no-op
				_, err = s.Shutdown(ctx, &taskAPI.ShutdownRequest{})
				if err != nil {
					logger.WithError(err).WithField("id", event.ContainerID).Fatal("failed to shutdown after container exit")
				}

			default:
				logger.Error("unexpected non-exit event type published on exit event channel")
			}

		case err = <-exitEventErrs:
			logger.WithError(err).Error("event error channel published to")

		case <-ctx.Done():
			err = ctx.Err()
			return

		}
	}
}
