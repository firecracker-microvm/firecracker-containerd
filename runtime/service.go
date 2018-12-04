// Copyright 2018 Amazon.com, Inc. or its affiliates. All Rights Reserved.
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
	"io/ioutil"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	eventstypes "github.com/containerd/containerd/api/events"
	"github.com/containerd/containerd/api/types"
	"github.com/containerd/containerd/api/types/task"
	"github.com/containerd/containerd/events"
	"github.com/containerd/containerd/log"
	"github.com/containerd/containerd/mount"
	"github.com/containerd/containerd/namespaces"
	"github.com/containerd/containerd/runtime"
	"github.com/containerd/containerd/runtime/v2/shim"
	taskAPI "github.com/containerd/containerd/runtime/v2/task"
	"github.com/containerd/ttrpc"
	"github.com/firecracker-microvm/firecracker-containerd/internal"
	"github.com/firecracker-microvm/firecracker-containerd/proto"
	firecracker "github.com/firecracker-microvm/firecracker-go-sdk"
	ptypes "github.com/gogo/protobuf/types"
	"github.com/mdlayher/vsock"
	"github.com/pkg/errors"
	"github.com/sirupsen/logrus"
	"golang.org/x/sys/unix"
)

const (
	defaultVsockPort = 10789
	// TODO: This will need to be changed once we are managing CID's
	defaultCID = uint32(3)
)

// implements shimapi
type service struct {
	server  *ttrpc.Server
	id      string
	publish events.Publisher

	agentStarted bool
	agentClient  taskAPI.TaskService
	config       *Config
	machine      *firecracker.Machine
}

var _ = (taskAPI.TaskService)(&service{})

// Matches type Init func(..).. defined https://github.com/containerd/containerd/blob/master/runtime/v2/shim/shim.go#L47
func NewService(ctx context.Context, id string, publisher events.Publisher) (shim.Shim, error) {
	server, err := newServer()
	if err != nil {
		return nil, err
	}

	config, err := LoadConfig("")
	if err != nil {
		return nil, err
	}

	if !config.Debug {
		// Check if containerd is running in debug mode
		opts := ctx.Value(shim.OptsKey{}).(shim.Opts)
		config.Debug = opts.Debug
	}

	s := &service{
		server:  server,
		id:      id,
		publish: publisher,
		config:  config,
	}

	return s, nil
}

func (s *service) StartShim(ctx context.Context, id, containerdBinary, containerdAddress string) (string, error) {
	cmd, err := s.newCommand(ctx, containerdBinary, containerdAddress)
	if err != nil {
		return "", err
	}

	address, err := shim.SocketAddress(ctx, id)
	if err != nil {
		return "", err
	}

	socket, err := shim.NewSocket(address)
	if err != nil {
		return "", err
	}

	defer socket.Close()

	f, err := socket.File()
	if err != nil {
		return "", err
	}

	defer f.Close()

	cmd.ExtraFiles = append(cmd.ExtraFiles, f)

	if err := cmd.Start(); err != nil {
		return "", err
	}

	defer func() {
		if err != nil {
			cmd.Process.Kill()
		}
	}()

	// make sure to wait after start
	go cmd.Wait()
	if err := shim.WritePidFile("shim.pid", cmd.Process.Pid); err != nil {
		return "", err
	}

	if err := shim.WriteAddress("address", address); err != nil {
		return "", err
	}

	if err := shim.SetScore(cmd.Process.Pid); err != nil {
		return "", errors.Wrap(err, "failed to set OOM Score on shim")
	}

	return address, nil
}

func (s *service) Create(ctx context.Context, request *taskAPI.CreateTaskRequest) (*taskAPI.CreateTaskResponse, error) {
	log.G(ctx).WithFields(logrus.Fields{
		"id":         request.ID,
		"bundle":     request.Bundle,
		"terminal":   request.Terminal,
		"stdin":      request.Stdin,
		"stdout":     request.Stdout,
		"stderr":     request.Stderr,
		"checkpoint": request.Checkpoint,
	}).Debug("creating task")

	// TODO: should there be a lock here
	if !s.agentStarted {
		client, err := s.startVM(ctx, request)
		if err != nil {
			log.G(ctx).WithError(err).Error("failed to start VM")
			return nil, err
		}

		s.agentClient = client
		s.agentStarted = true
	}

	log.G(ctx).Infof("creating task '%s'", request.ID)

	// Generate new anyData with bundle/config.json packed inside
	anyData, err := packBundle(filepath.Join(request.Bundle, "config.json"), request.Options)
	if err != nil {
		return nil, err
	}

	request.Options = anyData

	resp, err := s.agentClient.Create(ctx, request)
	if err != nil {
		log.G(ctx).WithError(err).Error("create failed")
		return nil, err
	}
	log.G(ctx).Infof("successfully created task with pid %d", resp.Pid)
	return resp, nil
}

func (s *service) Start(ctx context.Context, req *taskAPI.StartRequest) (*taskAPI.StartResponse, error) {
	log.G(ctx).WithFields(logrus.Fields{"id": req.ID, "exec_id": req.ExecID}).Debug("start")
	resp, err := s.agentClient.Start(ctx, req)
	if err != nil {
		return nil, err
	}
	// TODO: Do we need to cancel this at some point?
	go s.monitorState(ctx, req.ID, req.ExecID, resp.Pid)

	return resp, nil
}

func (s *service) monitorState(ctx context.Context, id, execID string, pid uint32) {
	ticker := time.NewTicker(time.Second)
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			//make a state request
			req := &taskAPI.StateRequest{
				ID:     id,
				ExecID: execID,
			}
			resp, err := s.agentClient.State(ctx, req)
			if err != nil {
				log.G(ctx).WithError(err).Error("error monitoring state")
				continue
			}
			// if ending state, stop vm and break
			if resp.Status == task.StatusStopped {
				s.publish.Publish(ctx, runtime.TaskExitEventTopic, &eventstypes.TaskExit{
					ContainerID: s.id,
					ID:          s.id,
					Pid:         pid,
					ExitStatus:  resp.ExitStatus,
					ExitedAt:    time.Now(),
				})
				s.server.Close()
				s.Shutdown(ctx, &taskAPI.ShutdownRequest{ID: id})
				return
			}
		}

	}
}

// Delete the initial process and container
func (s *service) Delete(ctx context.Context, req *taskAPI.DeleteRequest) (*taskAPI.DeleteResponse, error) {
	log.G(ctx).WithFields(logrus.Fields{"id": req.ID, "exec_id": req.ExecID}).Debug("delete")
	resp, err := s.agentClient.Delete(ctx, req)
	if err != nil {
		return nil, err
	}

	return resp, nil
}

// Exec an additional process inside the container
func (s *service) Exec(ctx context.Context, req *taskAPI.ExecProcessRequest) (*ptypes.Empty, error) {
	log.G(ctx).WithFields(logrus.Fields{"id": req.ID, "exec_id": req.ExecID}).Debug("exec")
	resp, err := s.agentClient.Exec(ctx, req)
	if err != nil {
		return nil, err
	}

	return resp, nil
}

// ResizePty of a process
func (s *service) ResizePty(ctx context.Context, req *taskAPI.ResizePtyRequest) (*ptypes.Empty, error) {
	log.G(ctx).WithFields(logrus.Fields{"id": req.ID, "exec_id": req.ExecID}).Debug("resize_pty")
	resp, err := s.agentClient.ResizePty(ctx, req)
	if err != nil {
		return nil, err
	}

	return resp, nil
}

// State returns runtime state information for a process
func (s *service) State(ctx context.Context, req *taskAPI.StateRequest) (*taskAPI.StateResponse, error) {
	log.G(ctx).WithFields(logrus.Fields{"id": req.ID, "exec_id": req.ExecID}).Debug("state")
	resp, err := s.agentClient.State(ctx, req)
	if err != nil {
		return nil, err
	}

	return resp, nil
}

// Pause the container
func (s *service) Pause(ctx context.Context, req *taskAPI.PauseRequest) (*ptypes.Empty, error) {
	log.G(ctx).WithField("id", req.ID).Debug("pause")
	resp, err := s.agentClient.Pause(ctx, req)
	if err != nil {
		return nil, err
	}

	return resp, nil
}

// Resume the container
func (s *service) Resume(ctx context.Context, req *taskAPI.ResumeRequest) (*ptypes.Empty, error) {
	log.G(ctx).WithField("id", req.ID).Debug("resume")
	resp, err := s.agentClient.Resume(ctx, req)
	if err != nil {
		return nil, err
	}

	return resp, nil
}

// Kill a process with the provided signal
func (s *service) Kill(ctx context.Context, req *taskAPI.KillRequest) (*ptypes.Empty, error) {
	log.G(ctx).WithFields(logrus.Fields{"id": req.ID, "exec_id": req.ExecID}).Debug("kill")
	// right now we want to kill vm always when kill is called
	// may not be true in multi-container vm
	defer func() {
		log.G(ctx).Debug("Stopping VM during kill")
		if err := s.stopVM(); err != nil {
			log.G(ctx).WithError(err).Error("failed to stop VM")
		}
	}()
	resp, err := s.agentClient.Kill(ctx, req)
	if err != nil {
		return nil, err
	}
	return resp, nil
}

// Pids returns all pids inside the container
func (s *service) Pids(ctx context.Context, req *taskAPI.PidsRequest) (*taskAPI.PidsResponse, error) {
	log.G(ctx).WithField("id", req.ID).Debug("pids")
	resp, err := s.agentClient.Pids(ctx, req)
	if err != nil {
		return nil, err
	}

	return resp, nil
}

// CloseIO of a process
func (s *service) CloseIO(ctx context.Context, req *taskAPI.CloseIORequest) (*ptypes.Empty, error) {
	log.G(ctx).WithFields(logrus.Fields{"id": req.ID, "exec_id": req.ExecID}).Debug("close_io")
	resp, err := s.agentClient.CloseIO(ctx, req)
	if err != nil {
		return nil, err
	}

	return resp, nil
}

// Checkpoint the container
func (s *service) Checkpoint(ctx context.Context, req *taskAPI.CheckpointTaskRequest) (*ptypes.Empty, error) {
	log.G(ctx).WithFields(logrus.Fields{"id": req.ID, "path": req.Path}).Info("checkpoint")
	resp, err := s.agentClient.Checkpoint(ctx, req)
	if err != nil {
		return nil, err
	}

	return resp, nil
}

// Connect returns shim information such as the shim's pid
func (s *service) Connect(ctx context.Context, req *taskAPI.ConnectRequest) (*taskAPI.ConnectResponse, error) {
	log.G(ctx).WithField("id", req.ID).Debug("connect")
	resp, err := s.agentClient.Connect(ctx, req)
	if err != nil {
		return nil, err
	}

	return resp, nil
}

func (s *service) Shutdown(ctx context.Context, req *taskAPI.ShutdownRequest) (*ptypes.Empty, error) {
	log.G(ctx).WithFields(logrus.Fields{"id": req.ID, "now": req.Now}).Debug("shutdown")

	if _, err := s.agentClient.Shutdown(ctx, req); err != nil {
		log.G(ctx).WithError(err).Error("failed to shutdown agent")
	}

	if err := s.stopVM(); err != nil {
		log.G(ctx).WithError(err).Error("failed to stop VM")
		return nil, err
	}
	// Exit to avoid 'zombie' shim processes
	defer os.Exit(0)
	return &ptypes.Empty{}, nil
}

func (s *service) Stats(ctx context.Context, req *taskAPI.StatsRequest) (*taskAPI.StatsResponse, error) {
	log.G(ctx).WithField("id", req.ID).Debug("stats")
	resp, err := s.agentClient.Stats(ctx, req)
	if err != nil {
		return nil, err
	}

	return resp, nil
}

// Update a running container
func (s *service) Update(ctx context.Context, req *taskAPI.UpdateTaskRequest) (*ptypes.Empty, error) {
	log.G(ctx).WithField("id", req.ID).Debug("update")
	resp, err := s.agentClient.Update(ctx, req)
	if err != nil {
		return nil, err
	}

	return resp, nil
}

// Wait for a process to exit
func (s *service) Wait(ctx context.Context, req *taskAPI.WaitRequest) (*taskAPI.WaitResponse, error) {
	log.G(ctx).WithFields(logrus.Fields{"id": req.ID, "exec_id": req.ExecID}).Debug("wait")
	resp, err := s.agentClient.Wait(ctx, req)
	if err != nil {
		return nil, err
	}

	return resp, nil
}

func (s *service) Cleanup(ctx context.Context) (*taskAPI.DeleteResponse, error) {
	log.G(ctx).Debug("cleanup")
	// Destroy VM/etc here?
	// copied from runcs impl, nothing to cleanup atm
	return &taskAPI.DeleteResponse{
		ExitedAt:   time.Now(),
		ExitStatus: 128 + uint32(unix.SIGKILL),
	}, nil
}

func (s *service) newCommand(ctx context.Context, containerdBinary, containerdAddress string) (*exec.Cmd, error) {
	ns, err := namespaces.NamespaceRequired(ctx)
	if err != nil {
		return nil, err
	}

	self, err := os.Executable()
	if err != nil {
		return nil, err
	}

	cwd, err := os.Getwd()
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
	cmd.Dir = cwd
	cmd.Env = append(os.Environ(), "GOMAXPROCS=2")
	cmd.SysProcAttr = &syscall.SysProcAttr{
		Setpgid: true,
	}

	return cmd, nil
}

func newServer() (*ttrpc.Server, error) {
	return ttrpc.NewServer(ttrpc.WithServerHandshaker(ttrpc.UnixSocketRequireSameUser()))
}

func dialVsock(ctx context.Context, contextID uint32, port uint32) (net.Conn, error) {
	// VM should start within 200ms, vsock dial will make retries at 100ms, 200ms, 400ms, 800ms and 1.6s
	const (
		retryCount      = 5
		initialDelay    = 100 * time.Millisecond
		delayMultiplier = 2
	)

	var lastErr error
	var currentDelay = initialDelay
	for i := 1; i <= retryCount; i++ {
		conn, err := vsock.Dial(contextID, port)
		if err == nil {
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

func (s *service) startVM(ctx context.Context, request *taskAPI.CreateTaskRequest) (taskAPI.TaskService, error) {
	/*
		What needs to be done here:
			- Start a firecracker agent with:
				- The container rootfs as a block device
					- rootfs will be the cwd
				- Mount this device inside agent at a well known location
				- With the agent running inside on a well-known port
			- After agent startup, create a vsock client dialing to the
				specified vsock port.
				TODO: We will need some sort of vsock CID accounting mechanism
				to know what CID to use, defaults to 3 for now
			- Return this client or error
	*/

	log.G(ctx).Info("starting VM")

	// TODO: find next available CID
	cid := defaultCID

	cfg := firecracker.Config{
		BinPath:         s.config.FirecrackerBinaryPath,
		SocketPath:      s.config.SocketPath,
		VsockDevices:    []firecracker.VsockDevice{{Path: "root", CID: cid}},
		KernelImagePath: s.config.KernelImagePath,
		KernelArgs:      s.config.KernelArgs,
		RootDrive:       firecracker.BlockDevice{HostPath: s.config.RootDrive, Mode: "rw"},
		CPUCount:        int64(s.config.CPUCount),
		CPUTemplate:     firecracker.CPUTemplate(s.config.CPUTemplate),
		MemInMiB:        256,
		Console:         s.config.Console,
		LogFifo:         s.config.LogFifo,
		LogLevel:        s.config.LogLevel,
		MetricsFifo:     s.config.MetricsFifo,
		Debug:           s.config.Debug,
	}

	// Verify rootfs mount and get file system image path

	if len(request.Rootfs) != 1 {
		return nil, errors.Errorf("unexpected number of mounts: %d", len(request.Rootfs))
	}

	mnt := request.Rootfs[0]
	if mnt.Type != internal.SnapshotterMountType {
		// Mount came not from Firecracker snapshotter, which is not supported by Firecracker.
		log.G(ctx).Errorf("invalid mount (type: '%s', source: '%s')", mnt.Type, mnt.Source)
		return nil, errors.New("unsupported mount, use Firecracker snapshotter")
	}

	imagePath, err := getOptionByKey(mnt, internal.SnapshotterImageKey)
	if err != nil {
		log.G(ctx).WithError(err).Error("can't find rootfs image path option")
		return nil, err
	}

	cfg.AdditionalDrives = append(cfg.AdditionalDrives, firecracker.BlockDevice{HostPath: imagePath, Mode: "rw"})

	// Snapshotter.Prepare creates an active snapshot, returned mounts are used to mount the snapshot to capture changes.
	// In the case of Firecracker, Prepare makes a file system image and mounts it for writing (hence Commit captures changes
	// by unmounting this image).
	// There are 2 cases when containerd calls Prepare:
	// - When pulling an image, containerd calls Prepare and unpacks layer data to it. This works as expected similar to
	// other snapshotter implementations.
	// - When running a task, an image is mounted for writing as well. In this case we don't need writable mount on
	// host machine as it must be attached to Firecracker instead and mounted inside a VM. So in order to prevent 2
	// writable mounts in 2 different places, unmount image on host machine.
	log.G(ctx).Debugf("umounting fs: %s", mnt.Source)
	if err := mount.Unmount(mnt.Source, 0); err != nil {
		log.G(ctx).WithError(err).Error("failed to unmount fs")
		return nil, err
	}

	s.machine = firecracker.NewMachine(cfg, firecracker.WithLogger(log.G(ctx)))

	log.G(ctx).Println("initializing machine")
	if _, err := s.machine.Init(ctx); err != nil {
		log.G(ctx).WithError(err).Error("machine init failed")
		return nil, err
	}

	log.G(ctx).Info("starting instance")
	if err := s.machine.StartInstance(ctx); err != nil {
		log.G(ctx).WithError(err).Error("failed to start instance")
		s.stopVM()
		return nil, err
	}

	log.G(ctx).Info("calling agent")
	conn, err := dialVsock(ctx, cid, defaultVsockPort)
	if err != nil {
		s.stopVM()
		return nil, err
	}

	log.G(ctx).Info("creating clients")
	rpcClient := ttrpc.NewClient(conn)
	rpcClient.OnClose(func() { conn.Close() })
	apiClient := taskAPI.NewTaskClient(rpcClient)

	return apiClient, nil
}

func (s *service) stopVM() error {
	return s.machine.StopVMM()
}

func getOptionByKey(mount *types.Mount, key string) (string, error) {
	prefix := key + "="
	for _, opt := range mount.Options {
		if strings.HasPrefix(opt, prefix) {
			return strings.TrimPrefix(opt, prefix), nil
		}
	}

	return "", errors.Errorf("couldn't find option with key '%s'", key)
}

func packBundle(path string, options *ptypes.Any) (*ptypes.Any, error) {
	// Add the bundle/config.json to the request so it can be recreated
	// inside the vm:
	// Read bundle json
	jsonBytes, err := ioutil.ReadFile(path)
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
	// add it to a type
	// Convert to any
	extraData := &proto.ExtraData{
		JsonSpec:    jsonBytes,
		RuncOptions: opts,
	}
	return ptypes.MarshalAny(extraData)
}
