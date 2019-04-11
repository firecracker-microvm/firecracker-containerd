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
	"encoding/json"
	"fmt"
	"io"
	"io/ioutil"
	"math"
	"strings"

	// disable gosec check for math/rand. We just need a random starting
	// place to start looking for CIDs; no need for cryptographically
	// secure randomness
	"math/rand" // #nosec

	"net"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"syscall"
	"time"
	"unsafe"

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
	"github.com/mdlayher/vsock"
	"github.com/pkg/errors"
	"github.com/sirupsen/logrus"
	"golang.org/x/sys/unix"

	"github.com/firecracker-microvm/firecracker-containerd/eventbridge"
	"github.com/firecracker-microvm/firecracker-containerd/internal"
	"github.com/firecracker-microvm/firecracker-containerd/proto"
	"github.com/firecracker-microvm/firecracker-containerd/runtime/firecrackeroci"
)

func init() {
	rand.Seed(time.Now().UnixNano())
}

const (
	defaultVsockPort     = 10789
	supportedMountFSType = "ext4"

	vmIDEnvVarKey = "FIRECRACKER_VM_ID"

	varRunDir                  = "/var/run/firecracker-containerd"
	firecrackerSockName        = "firecracker.sock"
	firecrackerLogFifoName     = "fc-logs.fifo"
	firecrackerMetricsFifoName = "fc-metrics.fifo"

	// TODO these strings are hardcoded throughout the containerd codebase, it may
	// be worth sending them a PR to define them as constants accessible to shim
	// implementations like our own
	shimAddrFileName = "address"
	shimLogFifoName  = "log"
	ociConfigName    = "config.json"
)

// implements shimapi
type service struct {
	server        *ttrpc.Server
	id            string
	vmID          string
	publish       events.Publisher
	eventExchange *exchange.Exchange

	startVMMutex sync.Mutex
	agentStarted bool
	agentClient  taskAPI.TaskService
	config       *Config
	machine      *firecracker.Machine
	machineCID   uint32
	ctx          context.Context
	cancel       context.CancelFunc
}

var (
	_       = (taskAPI.TaskService)(&service{})
	sysCall = syscall.Syscall
)

func shimOpts(ctx context.Context) (*shim.Opts, error) {
	opts, ok := ctx.Value(shim.OptsKey{}).(shim.Opts)
	if !ok {
		return nil, errors.New("failed to parse containerd shim opts from context")
	}

	return &opts, nil
}

// The bundle dir for the container can be provided by a containerd-defined flag, or, if that's
// not set, it's assumed to be the current working directory of this process (as set by the
// containerd parent process).
func resolveBundleDir(ctx context.Context) (string, error) {
	// Check to see if it was set by command line flag
	opts, err := shimOpts(ctx)
	if err != nil {
		return "", err
	}

	if opts.BundlePath != "" {
		return opts.BundlePath, nil
	}

	// If no command line flag, this must be a shim start routine, so we can assume it's the
	// current working directory
	cwd, err := os.Getwd()
	if err != nil {
		return "", errors.Wrap(err, "failed to read bundle dir path")
	}

	return cwd, nil
}

// If the VMID was provided by the client, we'll find it in the OCI config Annotations section.
// If, however, no VMID was provided in the OCI config, we it may have been set by a "shim start"
// parent process as an env var. resolveVMID checks those two locations and returns the VMID
// if found in either.
func resolveVMID(ctx context.Context) (string, error) {
	// If the vmID is provided via env var, use that
	if vmID := os.Getenv(vmIDEnvVarKey); vmID != "" {
		return vmID, nil
	}

	// if no env var, check the OCI config annotation section
	bundleDir, err := resolveBundleDir(ctx)
	if err != nil {
		return "", err
	}

	specPath := filepath.Join(bundleDir, ociConfigName)
	ociConfigFile, err := os.Open(specPath)
	if err != nil {
		return "", errors.Wrapf(err, "failed to open OCI config file %s", specPath)
	}

	defer ociConfigFile.Close()
	var ociConfig struct {
		Annotations map[string]string `json:"annotations,omitempty"`
	}

	if err := json.NewDecoder(ociConfigFile).Decode(&ociConfig); err != nil {
		return "", errors.Wrapf(err, "failed to parse Annotations section of OCI config file %s", specPath)
	}

	// This will return empty string if the key is not present in the OCI config, which the caller can decided
	// how to deal with
	return ociConfig.Annotations[firecrackeroci.VMIDAnnotationKey], nil
}

var _ shim.Init = NewService

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

	eventExchange := exchange.NewExchange()

	// Republish each event received on our exchange to the provided remote publisher.
	// TODO ideally we would be forwarding events instead of re-publishing them, which would preserve the events'
	// original timestamps and namespaces. However, as of this writing, the containerd v2 runtime model only provides a
	// shim with a publisher, not a forwarder, so we have to republish for now.
	go func() {
		if err := <-eventbridge.Republish(ctx, eventExchange, remotePublisher); err != nil && err != context.Canceled {
			log.G(ctx).WithError(err).Error("error while republishing events")
		}
	}()

	vmID, err := resolveVMID(ctx)
	if err != nil {
		return nil, err
	}

	// If no VMID was provided by the client or already auto-generated by a parent "shim start", generate a new random one.
	// This results in a default behavior of running each container in its own VM.
	if vmID == "" {
		uuid, err := uuid.NewV4()
		if err != nil {
			return nil, errors.Wrap(err, "failed to generate UUID for VMID")
		}
		vmID = uuid.String()
	}

	s := &service{
		server:        server,
		id:            id,
		vmID:          vmID,
		publish:       remotePublisher,
		eventExchange: eventExchange,

		config: config,
	}

	return s, nil
}

// vmDir holds files, sockets and FIFOs scoped to a single given VM
func (s *service) vmDir() string {
	return filepath.Join(varRunDir, s.vmID)
}

// shimAddrFilePath is the path to the shim address file as found in the vmDir. The
// address file holds the abstract unix socket path at which the shim serves its API.
// It's read by containerd to find the shim.
func (s *service) shimAddrFilePath() string {
	return filepath.Join(s.vmDir(), shimAddrFileName)
}

// bundleAddrFilePath is the path to the address file as found in the bundleDir.
// Even though the shim address is set per-VM, not per-container, containerd expects
// to find the shim addr file in the bundle dir, so we still have to create it or
// symlink it to the shimAddrFilePath
func (s *service) bundleAddrFilePath(ctx context.Context) (string, error) {
	bundleDir, err := resolveBundleDir(ctx)
	if err != nil {
		return "", err
	}

	return filepath.Join(bundleDir, shimAddrFileName), nil
}

// shimLogFifoPath is a path to a FIFO for writing shim logs as found in the vmDir.
func (s *service) shimLogFifoPath() string {
	return filepath.Join(s.vmDir(), shimLogFifoName)
}

// bundleLogFifoPath is a path to a FIFO for writing shim logs as found in the bundleDir.
// It is the path created by containerd for us, the shimLogFifoPath is just a symlink to one.
func (s *service) bundleLogFifoPath(ctx context.Context) (string, error) {
	bundleDir, err := resolveBundleDir(ctx)
	if err != nil {
		return "", err
	}

	return filepath.Join(bundleDir, shimLogFifoName), nil
}

// shimSocketAddress is the abstract unix socket path at which our shim serves its API
// It is unique per-VM.
func (s *service) shimSocketAddress(ctx context.Context) (string, error) {
	return shim.SocketAddress(ctx, s.vmID)
}

// firecrackerSockPath is the path to the firecracker VMM's API socket
func (s *service) firecrackerSockPath() string {
	return filepath.Join(s.vmDir(), firecrackerSockName)
}

// firecrackerLogFifoPath is the path to the firecracker VMM's log fifo
func (s *service) firecrackerLogFifoPath() string {
	return filepath.Join(s.vmDir(), firecrackerLogFifoName)
}

// firecrackerMetricsFifoPath is the path to the firecracker VMM's metrics fifo
func (s *service) firecrackerMetricsFifoPath() string {
	return filepath.Join(s.vmDir(), firecrackerMetricsFifoName)
}

// containerd expects there to be a file named "address" in the container bundle directory (it contains the
// unix abstract address at which the shim managing that container can be found). We create that file in the
// shim's VM directory and then symlink to it from each container's bundle dir.
func (s *service) createAddressSymlink(ctx context.Context) error {
	bundleAddrFilePath, err := s.bundleAddrFilePath(ctx)
	if err != nil {
		return err
	}

	err = os.Symlink(s.shimAddrFilePath(), bundleAddrFilePath)
	if err != nil {
		return errors.Wrapf(err, `failed to create shim address file symlink from "%s"->"%s"`, bundleAddrFilePath, s.shimAddrFilePath())
	}

	return nil
}

// containerd creates a fifo for writing shim logs every time it spins up a container in the container's bundle dir.
// Code defined as part containerd packages also assume there to be a fifo name "log" in the current working directory
// of the shim. Our shim's working directory after initialization is the vmID dir, so we create a symlink from
// <vmdir>/log to the log fifo of the first container spun up for the shim.
func (s *service) createShimLogFifoSymlink(ctx context.Context) error {
	bundleLogFifoPath, err := s.bundleLogFifoPath(ctx)
	if err != nil {
		return err
	}

	err = os.Symlink(bundleLogFifoPath, s.shimLogFifoPath())
	if err != nil {
		return errors.Wrapf(err, `failed to create shim log fifo symlink from "%s"->"%s"`, s.shimLogFifoPath(), bundleLogFifoPath)
	}

	return nil
}

func isEADDRINUSE(err error) bool {
	return err != nil && strings.Contains(err.Error(), "address already in use")
}

func (s *service) StartShim(ctx context.Context, id, containerdBinary, containerdAddress string) (string, error) {
	log.G(ctx).WithField("id", id).Debug("StartShim")

	shimSocketAddress, err := s.shimSocketAddress(ctx)
	if err != nil {
		return "", err
	}

	// We determine if there is already a shim managing a VM with the current VMID by attempting
	// to listen on the abstract socket address (which is parameterized by VMID). If we get
	// EADDRINUSE, then we assume there is already a shim for the VM and just hand off to that
	// one. This is in line with the approach used by containerd's reference runC shim v2
	// implementation (which is also designed to manage multiple containers from a single shim
	// process)
	shimSocket, err := shim.NewSocket(shimSocketAddress)
	if isEADDRINUSE(err) {
		// There's already a shim for this VMID, so just hand off to it
		err = s.createAddressSymlink(ctx)
		if err != nil {
			return "", err
		}

		return shimSocketAddress, nil
	} else if err != nil {
		return "", errors.Wrapf(err, "failed to create new shim socket at address \"%s\"", shimSocketAddress)
	}

	// If we're here, there is no pre-existing shim for this VMID, so we spawn a new one
	defer func() {
		log.G(ctx).WithField("id", id).Debug("closing shim socket")
		shimSocket.Close()
	}()

	err = os.MkdirAll(s.vmDir(), 0700)
	if err != nil {
		return "", err
	}

	shimSocketFile, err := shimSocket.File()
	if err != nil {
		return "", err
	}
	defer func() {
		log.G(ctx).WithField("id", id).Debug("closing shim socket file")
		shimSocketFile.Close()
	}()

	cmd, err := s.newCommand(ctx, id, containerdBinary, containerdAddress, shimSocketFile)
	if err != nil {
		return "", err
	}

	err = cmd.Start()
	if err != nil {
		log.G(ctx).WithError(err).WithField("id", id).Error("Failed to Start()")
		return "", err
	}

	defer func() {
		if err != nil {
			log.G(ctx).WithError(err).WithField("id", id).Error("killing shim process")
			cmd.Process.Kill()
		}
	}()

	// make sure to wait after start
	go func() {
		log.G(ctx).WithField("id", id).Debug("waiting on shim process")
		waitErr := cmd.Wait()
		log.G(ctx).WithError(waitErr).WithField("id", id).Debug("completed waiting on shim process")
	}()

	err = shim.WriteAddress(s.shimAddrFilePath(), shimSocketAddress)
	if err != nil {
		log.G(ctx).WithError(err).WithField("id", id).Error("failed to write address")
		return "", err
	}

	err = s.createAddressSymlink(ctx)
	if err != nil {
		return "", err
	}

	err = s.createShimLogFifoSymlink(ctx)
	if err != nil {
		return "", err
	}

	err = shim.SetScore(cmd.Process.Pid)
	if err != nil {
		log.G(ctx).WithError(err).WithField("id", id).Error("failed to set OOM score on shim")
		return "", errors.Wrap(err, "failed to set OOM Score on shim")
	}

	return shimSocketAddress, nil
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

	var firecrackerConfig *proto.FirecrackerConfig
	var err error
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

	// TODO: handle case where VM is pre-created
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

	// Generate new anyData with bundle/config.json packed inside
	anyData, err := packBundle(filepath.Join(request.Bundle, "config.json"), runcOpts)
	if err != nil {
		return nil, err
	}

	request.Options = anyData

	resp, err := s.agentClient.Create(ctx, request)
	if err != nil {
		log.G(ctx).WithError(err).Error("create failed")
		return nil, err
	}
	s.ctx, s.cancel = context.WithCancel(ctx)
	s.proxyStdio(s.ctx, request.Stdin, request.Stdout, request.Stderr, s.machineCID)
	log.G(ctx).WithField("id", request.ID).WithField("pid", resp.Pid).Info("successfully created task")

	return resp, nil
}

func (s *service) Start(ctx context.Context, req *taskAPI.StartRequest) (*taskAPI.StartResponse, error) {
	log.G(ctx).WithFields(logrus.Fields{"id": req.ID, "exec_id": req.ExecID}).Debug("start")
	resp, err := s.agentClient.Start(ctx, req)
	if err != nil {
		return nil, err
	}

	return resp, nil
}

func (s *service) proxyStdio(ctx context.Context, stdin, stdout, stderr string, cid uint32) {
	go proxyIO(ctx, stdin, cid, internal.StdinPort, true)
	go proxyIO(ctx, stdout, cid, internal.StdoutPort, false)
	go proxyIO(ctx, stderr, cid, internal.StderrPort, false)
}

func proxyIO(ctx context.Context, path string, cid, port uint32, in bool) {
	if path == "" {
		log.G(ctx).WithField("cid", cid).WithField("port", port).Warn("skipping IO, path is empty")
		return
	}
	log.G(ctx).WithField("path", path).WithField("cid", cid).WithField("port", port).Debug("setting up IO")
	f, err := fifo.OpenFifo(ctx, path, syscall.O_RDWR|syscall.O_NONBLOCK, 0700)
	if err != nil {
		log.G(ctx).WithError(err).WithField("path", path).Error("error opening fifo")
		return
	}
	conn, err := vsock.Dial(cid, port)
	if err != nil {
		log.G(ctx).WithError(err).WithField("path", path).Error("unable to dial agent vsock IO port")
		f.Close()
		return
	}
	go func() {
		<-ctx.Done()
		conn.Close()
		f.Close()
	}()
	log.G(ctx).WithField("path", path).Debug("begin copying io")
	buf := make([]byte, internal.DefaultBufferSize)
	if in {
		_, err = io.CopyBuffer(conn, f, buf)
	} else {
		_, err = io.CopyBuffer(f, conn, buf)
	}
	if err != nil {
		log.G(ctx).WithError(err).WithField("path", path).Error("error with stdio")
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

	log.G(ctx).Debug("stopping VM")
	if err := s.stopVM(); err != nil {
		log.G(ctx).WithError(err).Error("failed to stop VM")
		return nil, err
	}

	if err := os.RemoveAll(s.vmDir()); err != nil {
		log.G(ctx).WithError(err).Errorf("failed to remove VM dir %s", s.vmDir())
		return nil, err
	}

	s.cancel()
	// Exit to avoid 'zombie' shim processes
	defer os.Exit(0)
	log.G(ctx).Debug("stopping runtime")
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

func (s *service) newCommand(ctx context.Context, id, containerdBinary, containerdAddress string, shimSocketFile *os.File) (*exec.Cmd, error) {
	ns, err := namespaces.NamespaceRequired(ctx)
	if err != nil {
		return nil, err
	}

	self, err := os.Executable()
	if err != nil {
		return nil, err
	}

	bundleDir, err := resolveBundleDir(ctx)
	if err != nil {
		return nil, err
	}

	args := []string{
		"-namespace", ns,
		"-bundle", bundleDir,
		"-id", id,
		"-address", containerdAddress,
		"-publish-binary", containerdBinary,
	}

	if s.config.Debug {
		args = append(args, "-debug")
	}

	cmd := exec.Command(self, args...)
	cmd.Dir = s.vmDir()
	cmd.Env = append(os.Environ(),
		"GOMAXPROCS=2",
		fmt.Sprintf("%s=%s", vmIDEnvVarKey, s.vmID))
	cmd.ExtraFiles = append(cmd.ExtraFiles, shimSocketFile)
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
		SocketPath:      s.firecrackerSockPath(),
		VsockDevices:    []firecracker.VsockDevice{{Path: "root", CID: cid}},
		KernelImagePath: s.config.KernelImagePath,
		KernelArgs:      s.config.KernelArgs,
		MachineCfg: models.MachineConfiguration{
			VcpuCount:   int64(s.config.CPUCount),
			CPUTemplate: models.CPUTemplate(s.config.CPUTemplate),
			MemSizeMib:  256,
		},
		LogFifo:     s.firecrackerLogFifoPath(),
		LogLevel:    s.config.LogLevel,
		MetricsFifo: s.firecrackerMetricsFifoPath(),
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
		WithSocketPath(s.firecrackerSockPath()).
		Build(ctx)
	machineOpts := []firecracker.Opt{
		firecracker.WithProcessRunner(cmd),
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
		// Connect the agent's event exchange to our own own event exchange using the eventbridge client. All events
		// that are published on the agent's exchange will also be published on our own
		if err := <-eventbridge.Attach(ctx, eventBridgeClient, s.eventExchange); err != nil && err != context.Canceled {
			log.G(ctx).WithError(err).Error("error while forwarding events from VM agent")
		}
	}()

	go s.monitorTaskExit(ctx)

	return apiClient, nil
}

func (s *service) monitorTaskExit(ctx context.Context) {
	exitEvents, exitEventErrs := s.eventExchange.Subscribe(ctx, fmt.Sprintf(`topic=="%s"`, runtime.TaskExitEventTopic))

	var err error
	defer func() {
		if err != nil && err != context.Canceled {
			log.G(ctx).WithError(err).Error("error while waiting for task exit events")
		}
	}()

	select {
	case <-exitEvents:
		// If the task exits, we shut down the VM and exit this shim. This behavior may change in the future when
		// we support multiple containers per VM.
		s.Shutdown(ctx, &taskAPI.ShutdownRequest{ID: s.id})
	case err = <-exitEventErrs:
	case <-ctx.Done():
		err = ctx.Err()
	}
}

func (s *service) stopVM() error {
	return s.machine.StopVMM()
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
