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
	"fmt"
	"log"
	"os"
	"os/exec"
	"syscall"
	"time"

	"github.com/containerd/containerd/events"
	"github.com/containerd/containerd/namespaces"
	"github.com/containerd/containerd/runtime/v2/shim"
	taskAPI "github.com/containerd/containerd/runtime/v2/task"
	"github.com/containerd/ttrpc"
	firecracker "github.com/firecracker-microvm/firecracker-go-sdk"
	ptypes "github.com/gogo/protobuf/types"
	"github.com/mdlayher/vsock"
	"github.com/pkg/errors"
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
	machine      *firecracker.Machine
}

var _ = (taskAPI.TaskService)(&service{})

// Matches type Init func(..).. defined https://github.com/containerd/containerd/blob/master/runtime/v2/shim/shim.go#L47
func NewService(ctx context.Context, id string, publisher events.Publisher) (shim.Shim, error) {
	server, err := newServer()
	if err != nil {
		return nil, err
	}
	s := &service{
		server:  server,
		id:      id,
		publish: publisher,
	}
	return s, nil
}

func (s *service) StartShim(ctx context.Context, id, containerdBinary, containerdAddress string) (string, error) {
	log.Println("StartShim Called with", id, containerdBinary, containerdAddress)
	cmd, err := newCommand(ctx, containerdBinary, containerdAddress)
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
	log.Println("starting shim at :", address, socket)
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

func (s *service) Create(ctx context.Context, r *taskAPI.CreateTaskRequest) (*taskAPI.CreateTaskResponse, error) {
	log.Println("CreateCalled")
	// TODO: should there be a lock here
	if !s.agentStarted {
		client, err := s.startVM(ctx)
		if err != nil {
			return nil, err
		}

		s.agentClient = client
		s.agentStarted = true
	}
	// Proxy Request
	log.Println("Calling agentCreate")
	resp, err := s.agentClient.Create(ctx, r)
	log.Println("Received ", resp, err, " from agent")
	log.Println("bundle:", r.Bundle)
	for i := range r.Rootfs {
		log.Println("Mount ", i, " ", r.Rootfs[i])
	}
	if err != nil {
		return nil, err
	}
	return resp, nil
}

func (s *service) startVM(ctx context.Context) (taskAPI.TaskService, error) {
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

	log.Println("starting VM")

	// TODO: find next available CID
	cid := defaultCID

	cfg := firecracker.Config{
		BinPath:         "./firecracker",
		SocketPath:      fmt.Sprintf("./firecracker_%d.sock", cid),
		VsockDevices:    []firecracker.VsockDevice{{Path: "root", CID: cid}},
		KernelImagePath: "./vmlinux",
		KernelArgs:      "console=ttyS0 noapic reboot=k panic=1 pci=off nomodules rw",
		RootDrive:       firecracker.BlockDevice{HostPath: "vsock.img", Mode: "rw"},
		CPUCount:        1,
		CPUTemplate:     firecracker.CPUTemplate(firecracker.CPUTemplateT2),
	}

	s.machine = firecracker.NewMachine(cfg)

	log.Println("initializing FC")
	if _, err := s.machine.Init(ctx); err != nil {
		return nil, err
	}

	log.Println("starting instance")
	if err := s.machine.StartInstance(ctx); err != nil {
		s.stopVM()
		return nil, err
	}

	// TODO: wait for agent to be started / Dial retries?

	log.Println("calling agent")
	conn, err := vsock.Dial(cid, defaultVsockPort)
	if err != nil {
		s.stopVM()
		return nil, err
	}

	// Create ttrpc client
	log.Println("Creating client")
	rpcClient := ttrpc.NewClient(conn)
	// Create taskClient
	svc := taskAPI.NewTaskClient(rpcClient)
	// return client
	return svc, nil
}

func (s *service) stopVM() error {
	return s.machine.StopVMM()
}

func (s *service) Start(ctx context.Context, r *taskAPI.StartRequest) (*taskAPI.StartResponse, error) {
	log.Println("StartCalled")
	resp, err := s.agentClient.Start(ctx, r)
	if err != nil {
		return nil, err
	}
	return resp, nil
}

// Delete the initial process and container
func (s *service) Delete(ctx context.Context, r *taskAPI.DeleteRequest) (*taskAPI.DeleteResponse, error) {
	log.Println("DeleteCalled")
	resp, err := s.agentClient.Delete(ctx, r)
	if err != nil {
		return nil, err
	}
	return resp, nil
}

// Exec an additional process inside the container
func (s *service) Exec(ctx context.Context, r *taskAPI.ExecProcessRequest) (*ptypes.Empty, error) {
	log.Println("ExecCalled")
	resp, err := s.agentClient.Exec(ctx, r)
	if err != nil {
		return nil, err
	}
	return resp, nil
}

// ResizePty of a process
func (s *service) ResizePty(ctx context.Context, r *taskAPI.ResizePtyRequest) (*ptypes.Empty, error) {
	log.Println("ResizePTYCalled")
	resp, err := s.agentClient.ResizePty(ctx, r)
	if err != nil {
		return nil, err
	}
	return resp, nil
}

// State returns runtime state information for a process
func (s *service) State(ctx context.Context, r *taskAPI.StateRequest) (*taskAPI.StateResponse, error) {
	log.Println("StateCalled")
	resp, err := s.agentClient.State(ctx, r)
	if err != nil {
		return nil, err
	}
	return resp, nil
}

// Pause the container
func (s *service) Pause(ctx context.Context, r *taskAPI.PauseRequest) (*ptypes.Empty, error) {
	log.Println("PauseCalled")
	resp, err := s.agentClient.Pause(ctx, r)
	if err != nil {
		return nil, err
	}
	return resp, nil
}

// Resume the container
func (s *service) Resume(ctx context.Context, r *taskAPI.ResumeRequest) (*ptypes.Empty, error) {
	log.Println("ResumeCalled")
	resp, err := s.agentClient.Resume(ctx, r)
	if err != nil {
		return nil, err
	}
	return resp, nil
}

// Kill a process with the provided signal
func (s *service) Kill(ctx context.Context, r *taskAPI.KillRequest) (*ptypes.Empty, error) {
	log.Println("KillCalled")
	resp, err := s.agentClient.Kill(ctx, r)
	if err != nil {
		return nil, err
	}
	return resp, nil
}

// Pids returns all pids inside the container
func (s *service) Pids(ctx context.Context, r *taskAPI.PidsRequest) (*taskAPI.PidsResponse, error) {
	log.Println("PidsCalled")
	resp, err := s.agentClient.Pids(ctx, r)
	if err != nil {
		return nil, err
	}
	return resp, nil
}

// CloseIO of a process
func (s *service) CloseIO(ctx context.Context, r *taskAPI.CloseIORequest) (*ptypes.Empty, error) {
	log.Println("CloseIOCalled")
	resp, err := s.agentClient.CloseIO(ctx, r)
	if err != nil {
		return nil, err
	}
	return resp, nil
}

// Checkpoint the container
func (s *service) Checkpoint(ctx context.Context, r *taskAPI.CheckpointTaskRequest) (*ptypes.Empty, error) {
	log.Println("CheckpointCalled")
	resp, err := s.agentClient.Checkpoint(ctx, r)
	if err != nil {
		return nil, err
	}
	return resp, nil
}

// Connect returns shim information such as the shim's pid
func (s *service) Connect(ctx context.Context, r *taskAPI.ConnectRequest) (*taskAPI.ConnectResponse, error) {
	log.Println("ConnectCalled")
	resp, err := s.agentClient.Connect(ctx, r)
	if err != nil {
		return nil, err
	}
	return resp, nil
}

func (s *service) Shutdown(ctx context.Context, r *taskAPI.ShutdownRequest) (*ptypes.Empty, error) {
	log.Println("ShutdownCalled")

	if _, err := s.agentClient.Shutdown(ctx, r); err != nil {
		log.Printf("failed to shutdown agent: %v", err)
	}

	if err := s.stopVM(); err != nil {
		log.Printf("failed to stop VM: %v", err)
		return nil, err
	}

	return &ptypes.Empty{}, nil
}

func (s *service) Stats(ctx context.Context, r *taskAPI.StatsRequest) (*taskAPI.StatsResponse, error) {
	log.Println("StatsCalled")
	resp, err := s.agentClient.Stats(ctx, r)
	if err != nil {
		return nil, err
	}
	return resp, nil
}

// Update a running container
func (s *service) Update(ctx context.Context, r *taskAPI.UpdateTaskRequest) (*ptypes.Empty, error) {
	log.Println("UpdateCalled")
	resp, err := s.agentClient.Update(ctx, r)
	if err != nil {
		return nil, err
	}
	return resp, nil
}

// Wait for a process to exit
func (s *service) Wait(ctx context.Context, r *taskAPI.WaitRequest) (*taskAPI.WaitResponse, error) {
	log.Println("WaitCalled")
	resp, err := s.agentClient.Wait(ctx, r)
	if err != nil {
		return nil, err
	}
	return resp, nil
}

func (s *service) Cleanup(ctx context.Context) (*taskAPI.DeleteResponse, error) {
	log.Println("CleanupCalled")
	// Destroy VM/etc here?
	// copied from runcs impl, nothing to cleanup atm
	return &taskAPI.DeleteResponse{
		ExitedAt:   time.Now(),
		ExitStatus: 128 + uint32(unix.SIGKILL),
	}, nil
}

func newCommand(ctx context.Context, containerdBinary, containerdAddress string) (*exec.Cmd, error) {
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
