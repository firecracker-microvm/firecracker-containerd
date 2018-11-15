package main

import (
	"context"
	"log"
	"os"
	"os/exec"
	"strings"
	"syscall"
	"time"

	firecracker "github.com/awslabs/go-firecracker"
	"github.com/containerd/containerd/events"
	"github.com/containerd/containerd/namespaces"
	"github.com/containerd/containerd/runtime/v2/shim"
	taskAPI "github.com/containerd/containerd/runtime/v2/task"
	"github.com/containerd/ttrpc"
	ptypes "github.com/gogo/protobuf/types"
	"github.com/pkg/errors"
	"golang.org/x/sys/unix"
)

const runcV1Path = "containerd-shim-runc-v1"

// implements shimapi
type service struct {
	server  *ttrpc.Server
	id      string
	ctx     context.Context
	publish events.Publisher

	//TODO: REALNAME
	shimShimStarted bool
	shimShimClient  taskAPI.TaskService
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
		ctx:     ctx,
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
	if !s.shimShimStarted {
		var err error
		log.Println("Starting shimshim")
		// Args[4] = containerd socket address
		// Args[6] = containerd binary publish path
		// TODO: refactor so we don't read directly from os.Args
		s.shimShimClient, err = startShimShim(s.ctx, s.id, os.Args[4], os.Args[6])
		if err != nil {
			return nil, err
		}
		s.shimShimStarted = true
	}
	// Proxy Request
	log.Println("Calling shimshimClient")
	resp, err := s.shimShimClient.Create(ctx, r)
	log.Println("Received ", resp, err, " from shimshim")
	if err != nil {
		return nil, err
	}
	return resp, nil
}

func startShimShim(ctx context.Context, id, containerdAddress, containerdBinary string) (taskAPI.TaskService, error) {
	// Start binary -- get address
	ns, err := namespaces.NamespaceRequired(ctx)
	if err != nil {
		return nil, err
	}
	log.Println(ns)
	cwd, err := os.Getwd()
	if err != nil {
		return nil, err
	}
	args := []string{
		"-namespace", "inside-shim",
		"-address", containerdAddress,
		"-publish-binary", containerdBinary,
		"start",
	}
	cmd := exec.Command(runcV1Path, args...)
	cmd.Dir = cwd
	cmd.Env = append(os.Environ(), "GOMAXPROCS=2")
	cmd.SysProcAttr = &syscall.SysProcAttr{
		Setpgid: true,
	}
	log.Println("Starting other shim")
	out, err := cmd.CombinedOutput()
	log.Println("inside shim exited, err:", err)
	if err != nil {
		return nil, errors.Wrapf(err, "%s", out)
	}
	address := strings.TrimSpace(string(out))
	//conn, err := net.Dial("unix", address)
	conn, err := shim.Connect(address, shim.AnonDialer)
	if err != nil {
		return nil, err
	}
	// THIS IS WHERE WE WOULD START A VM -- passing whatever args needed to
	// TODO: will need to be vsock
	_ = firecracker.Config{}
	// Create ttrpc client
	rpcClient := ttrpc.NewClient(conn)
	// Create taskClient
	svc := taskAPI.NewTaskClient(rpcClient)
	// return client
	return svc, nil
}

func (s *service) Start(ctx context.Context, r *taskAPI.StartRequest) (*taskAPI.StartResponse, error) {
	log.Println("StartCalled")
	resp, err := s.shimShimClient.Start(ctx, r)
	if err != nil {
		return nil, err
	}
	return resp, nil
}

// Delete the initial process and container
func (s *service) Delete(ctx context.Context, r *taskAPI.DeleteRequest) (*taskAPI.DeleteResponse, error) {
	log.Println("DeleteCalled")
	resp, err := s.shimShimClient.Delete(ctx, r)
	if err != nil {
		return nil, err
	}
	return resp, nil
}

// Exec an additional process inside the container
func (s *service) Exec(ctx context.Context, r *taskAPI.ExecProcessRequest) (*ptypes.Empty, error) {
	log.Println("ExecCalled")
	resp, err := s.shimShimClient.Exec(ctx, r)
	if err != nil {
		return nil, err
	}
	return resp, nil
}

// ResizePty of a process
func (s *service) ResizePty(ctx context.Context, r *taskAPI.ResizePtyRequest) (*ptypes.Empty, error) {
	log.Println("ResizePTYCalled")
	resp, err := s.shimShimClient.ResizePty(ctx, r)
	if err != nil {
		return nil, err
	}
	return resp, nil
}

// State returns runtime state information for a process
func (s *service) State(ctx context.Context, r *taskAPI.StateRequest) (*taskAPI.StateResponse, error) {
	log.Println("StateCalled")
	resp, err := s.shimShimClient.State(ctx, r)
	if err != nil {
		return nil, err
	}
	return resp, nil
}

// Pause the container
func (s *service) Pause(ctx context.Context, r *taskAPI.PauseRequest) (*ptypes.Empty, error) {
	log.Println("PauseCalled")
	resp, err := s.shimShimClient.Pause(ctx, r)
	if err != nil {
		return nil, err
	}
	return resp, nil
}

// Resume the container
func (s *service) Resume(ctx context.Context, r *taskAPI.ResumeRequest) (*ptypes.Empty, error) {
	log.Println("ResumeCalled")
	resp, err := s.shimShimClient.Resume(ctx, r)
	if err != nil {
		return nil, err
	}
	return resp, nil
}

// Kill a process with the provided signal
func (s *service) Kill(ctx context.Context, r *taskAPI.KillRequest) (*ptypes.Empty, error) {
	log.Println("KillCalled")
	resp, err := s.shimShimClient.Kill(ctx, r)
	if err != nil {
		return nil, err
	}
	return resp, nil
}

// Pids returns all pids inside the container
func (s *service) Pids(ctx context.Context, r *taskAPI.PidsRequest) (*taskAPI.PidsResponse, error) {
	log.Println("PidsCalled")
	resp, err := s.shimShimClient.Pids(ctx, r)
	if err != nil {
		return nil, err
	}
	return resp, nil
}

// CloseIO of a process
func (s *service) CloseIO(ctx context.Context, r *taskAPI.CloseIORequest) (*ptypes.Empty, error) {
	log.Println("CloseIOCalled")
	resp, err := s.shimShimClient.CloseIO(ctx, r)
	if err != nil {
		return nil, err
	}
	return resp, nil
}

// Checkpoint the container
func (s *service) Checkpoint(ctx context.Context, r *taskAPI.CheckpointTaskRequest) (*ptypes.Empty, error) {
	log.Println("CheckpointCalled")
	resp, err := s.shimShimClient.Checkpoint(ctx, r)
	if err != nil {
		return nil, err
	}
	return resp, nil
}

// Connect returns shim information such as the shim's pid
func (s *service) Connect(ctx context.Context, r *taskAPI.ConnectRequest) (*taskAPI.ConnectResponse, error) {
	log.Println("ConnectCalled")
	resp, err := s.shimShimClient.Connect(ctx, r)
	if err != nil {
		return nil, err
	}
	return resp, nil
}

func (s *service) Shutdown(ctx context.Context, r *taskAPI.ShutdownRequest) (*ptypes.Empty, error) {
	log.Println("ShutdownCalled")
	s.shimShimClient.Shutdown(ctx, r)
	os.Exit(0)
	return &ptypes.Empty{}, nil
}

func (s *service) Stats(ctx context.Context, r *taskAPI.StatsRequest) (*taskAPI.StatsResponse, error) {
	log.Println("StatsCalled")
	resp, err := s.shimShimClient.Stats(ctx, r)
	if err != nil {
		return nil, err
	}
	return resp, nil
}

// Update a running container
func (s *service) Update(ctx context.Context, r *taskAPI.UpdateTaskRequest) (*ptypes.Empty, error) {
	log.Println("UpdateCalled")
	resp, err := s.shimShimClient.Update(ctx, r)
	if err != nil {
		return nil, err
	}
	return resp, nil
}

// Wait for a process to exit
func (s *service) Wait(ctx context.Context, r *taskAPI.WaitRequest) (*taskAPI.WaitResponse, error) {
	log.Println("WaitCalled")
	resp, err := s.shimShimClient.Wait(ctx, r)
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
