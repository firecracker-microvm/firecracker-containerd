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
	"net"
	"path/filepath"
	"runtime/debug"
	"time"

	"github.com/containerd/containerd/cio"
	"github.com/containerd/containerd/events/exchange"
	"github.com/containerd/containerd/log"
	"github.com/containerd/containerd/namespaces"
	"github.com/containerd/containerd/runtime/v2/runc"
	taskAPI "github.com/containerd/containerd/runtime/v2/task"
	"github.com/gogo/protobuf/types"
	"github.com/mdlayher/vsock"
	"github.com/pkg/errors"
	"github.com/sirupsen/logrus"

	"github.com/firecracker-microvm/firecracker-containerd/internal/bundle"
	"github.com/firecracker-microvm/firecracker-containerd/internal/vm"
	proto "github.com/firecracker-microvm/firecracker-containerd/proto/grpc"
)

const (
	containerRootDir = "/container"
)

// TaskService represents inner shim wrapper over runc in order to:
// - Add default namespace to ctx as it's not passed by ttrpc over vsock
// - Add debug logging to simplify debugging
// - Make place for future extensions as needed
type TaskService struct {
	taskManager vm.TaskManager

	eventExchange *exchange.Exchange
	cancel        func()
}

// NewTaskService creates new runc shim wrapper
func NewTaskService(eventExchange *exchange.Exchange, cancel context.CancelFunc) taskAPI.TaskService {
	return &TaskService{
		taskManager: vm.NewTaskManager(),

		eventExchange: eventExchange,
		cancel:        cancel,
	}
}

func logPanicAndDie(logger *logrus.Entry) {
	if err := recover(); err != nil {
		logger.WithError(err.(error)).Fatalf("panic: %s", string(debug.Stack()))
	}
}

func unmarshalExtraData(marshalled *types.Any) (*proto.ExtraData, error) {
	// get json bytes from task request
	extraData := &proto.ExtraData{}
	err := types.UnmarshalAny(marshalled, extraData)
	if err != nil {
		return nil, err
	}

	return extraData, nil
}

// Create creates a new initial process and container using runc
func (ts *TaskService) Create(ctx context.Context, req *taskAPI.CreateTaskRequest) (*taskAPI.CreateTaskResponse, error) {
	defer logPanicAndDie(log.G(ctx))
	logger := log.G(ctx).WithFields(logrus.Fields{"id": req.ID, "bundle": req.Bundle})
	logger.Info("create")

	extraData, err := unmarshalExtraData(req.Options)
	if err != nil {
		return nil, err
	}

	bundleDir := bundle.Dir(filepath.Join(containerRootDir, req.ID))
	err = bundleDir.Create()
	if err != nil {
		return nil, err
	}

	err = bundleDir.OCIConfig().Write(extraData.JsonSpec)
	if err != nil {
		return nil, err
	}

	// TODO replace with proper drive mounting once that PR is merged. Right now, all containers in
	// this VM start up with the same rootfs image no matter their configuration
	err = bundleDir.MountRootfs("/dev/vdb", "ext4", nil)
	if err != nil {
		return nil, err
	}

	// Create a runc shim to manage this task
	// TODO if we update to the v2 runc implementation in containerd, we can use a single
	// runc service instance to manage all tasks instead of creating a new one for each
	runcService, err := runc.New(ctx, req.ID, ts.eventExchange)
	if err != nil {
		return nil, err
	}

	// Override the incoming stdio FIFOs, which have paths from the host that we can't use
	fifoSet, err := cio.NewFIFOSetInDir(bundleDir.RootPath(), req.ID, req.Terminal)
	if err != nil {
		log.G(ctx).WithError(err).Error("failed opening stdio FIFOs")
		return nil, err
	}

	// Don't try to connect any io streams that weren't requested by the client
	if req.Stdin == "" {
		fifoSet.Stdin = ""
	}

	if req.Stdout == "" {
		fifoSet.Stdout = ""
	}

	if req.Stderr == "" {
		fifoSet.Stderr = ""
	}

	task, err := ts.taskManager.AddTask(ctx, req.ID, runcService, bundleDir, extraData, fifoSet)
	if err != nil {
		return nil, err
	}

	logger.Debug("calling runc create")

	// Override some of the incoming paths, which were set on the Host and thus not valid here in the Guest
	req.Bundle = bundleDir.RootPath()
	req.Rootfs = nil
	req.Stdin = fifoSet.Stdin
	req.Stdout = fifoSet.Stdout
	req.Stderr = fifoSet.Stderr

	// Just provide runc the options it knows about, not our wrapper
	req.Options = task.ExtraData().GetRuncOptions()

	// Start the io proxy and wait for initialization to complete before starting
	// the task to ensure we capture all task output
	err = <-task.StartStdioProxy(ctx, vm.VSockToFIFO, acceptVSock)
	if err != nil {
		return nil, err
	}

	resp, err := task.Create(ctx, req)
	if err != nil {
		logger.WithError(err).Error("error creating container")
		return nil, err
	}

	logger.WithField("pid", resp.Pid).Debugf("create succeeded")
	return resp, nil
}

var _ vm.VSockConnector = acceptVSock

func acceptVSock(ctx context.Context, port uint32) (net.Conn, error) {
	listener, err := vsock.Listen(port)
	if err != nil {
		return nil, errors.Wrap(err, "unable to listen on vsock")
	}

	var conn net.Conn
	for range time.Tick(10 * time.Millisecond) {
		// accept is non-blocking so try to accept until we get a connection
		// TODO: investigate if there is a way to distinguish
		// transient errors from permanent ones.
		conn, err = listener.Accept()
		if err == nil {
			break
		}
		log.G(ctx).WithError(err).Debug("stdio vsock accept failure")
	}

	return conn, nil
}

// State returns process state information
func (ts *TaskService) State(ctx context.Context, req *taskAPI.StateRequest) (*taskAPI.StateResponse, error) {
	defer logPanicAndDie(log.G(ctx))

	log.G(ctx).WithFields(logrus.Fields{"id": req.ID, "exec_id": req.ExecID}).Debug("state")
	task, err := ts.taskManager.Task(req.ID)
	if err != nil {
		return nil, err
	}

	ctx = namespaces.WithNamespace(ctx, defaultNamespace)
	resp, err := task.State(ctx, req)
	if err != nil {
		log.G(ctx).WithError(err).Error("state failed")
		return nil, err
	}

	log.G(ctx).WithFields(logrus.Fields{
		"id":     resp.ID,
		"bundle": resp.Bundle,
		"pid":    resp.Pid,
		"status": resp.Status,
	}).Debug("state succeeded")
	return resp, nil
}

// Start starts a process
func (ts *TaskService) Start(ctx context.Context, req *taskAPI.StartRequest) (*taskAPI.StartResponse, error) {
	defer logPanicAndDie(log.G(ctx))

	log.G(ctx).WithFields(logrus.Fields{"id": req.ID, "exec_id": req.ExecID}).Debug("start")
	task, err := ts.taskManager.Task(req.ID)
	if err != nil {
		return nil, err
	}

	ctx = namespaces.WithNamespace(ctx, defaultNamespace)
	resp, err := task.Start(ctx, req)
	if err != nil {
		log.G(ctx).WithError(err).Error("start failed")
		return nil, err
	}

	log.G(ctx).WithField("pid", resp.Pid).Debug("start succeeded")
	return resp, nil
}

// Delete deletes the initial process and container
func (ts *TaskService) Delete(ctx context.Context, req *taskAPI.DeleteRequest) (*taskAPI.DeleteResponse, error) {
	defer logPanicAndDie(log.G(ctx))

	log.G(ctx).WithFields(logrus.Fields{"id": req.ID, "exec_id": req.ExecID}).Debug("delete")
	task, err := ts.taskManager.Task(req.ID)
	if err != nil {
		return nil, err
	}

	ctx = namespaces.WithNamespace(ctx, defaultNamespace)
	resp, err := task.Delete(ctx, req)
	if err != nil {
		log.G(ctx).WithError(err).Error("delete failed")
		return nil, err
	}

	log.G(ctx).WithFields(logrus.Fields{
		"pid":         resp.Pid,
		"exit_status": resp.ExitStatus,
	}).Debug("delete succeeded")
	return resp, nil
}

// Pids returns all pids inside the container
func (ts *TaskService) Pids(ctx context.Context, req *taskAPI.PidsRequest) (*taskAPI.PidsResponse, error) {
	defer logPanicAndDie(log.G(ctx))

	log.G(ctx).WithField("id", req.ID).Debug("pids")
	task, err := ts.taskManager.Task(req.ID)
	if err != nil {
		return nil, err
	}

	ctx = namespaces.WithNamespace(ctx, defaultNamespace)
	resp, err := task.Pids(ctx, req)
	if err != nil {
		log.G(ctx).WithError(err).Error("pids failed")
		return nil, err
	}

	log.G(ctx).Debug("pids succeeded")
	return resp, nil
}

// Pause pauses the container
func (ts *TaskService) Pause(ctx context.Context, req *taskAPI.PauseRequest) (*types.Empty, error) {
	defer logPanicAndDie(log.G(ctx))

	log.G(ctx).WithField("id", req.ID).Debug("pause")
	task, err := ts.taskManager.Task(req.ID)
	if err != nil {
		return nil, err
	}

	ctx = namespaces.WithNamespace(ctx, defaultNamespace)
	resp, err := task.Pause(ctx, req)
	if err != nil {
		log.G(ctx).WithError(err).Error("pause failed")
		return nil, err
	}

	log.G(ctx).Debug("pause succeeded")
	return resp, nil
}

// Resume resumes the container
func (ts *TaskService) Resume(ctx context.Context, req *taskAPI.ResumeRequest) (*types.Empty, error) {
	defer logPanicAndDie(log.G(ctx))

	log.G(ctx).WithField("id", req.ID).Debug("resume")
	task, err := ts.taskManager.Task(req.ID)
	if err != nil {
		return nil, err
	}

	ctx = namespaces.WithNamespace(ctx, defaultNamespace)
	resp, err := task.Resume(ctx, req)
	if err != nil {
		log.G(ctx).WithError(err).Debug("resume failed")
		return nil, err
	}

	log.G(ctx).Debug("resume succeeded")
	return resp, nil
}

// Checkpoint saves the state of the container instance
func (ts *TaskService) Checkpoint(ctx context.Context, req *taskAPI.CheckpointTaskRequest) (*types.Empty, error) {
	defer logPanicAndDie(log.G(ctx))

	log.G(ctx).WithFields(logrus.Fields{"id": req.ID, "path": req.Path}).Info("checkpoint")
	task, err := ts.taskManager.Task(req.ID)
	if err != nil {
		return nil, err
	}

	ctx = namespaces.WithNamespace(ctx, defaultNamespace)
	resp, err := task.Checkpoint(ctx, req)
	if err != nil {
		log.G(ctx).WithError(err).Error("checkout failed")
		return nil, err
	}

	log.G(ctx).Debug("checkpoint sutaskAPI")
	return resp, nil
}

// Kill kills a process with the provided signal
func (ts *TaskService) Kill(ctx context.Context, req *taskAPI.KillRequest) (*types.Empty, error) {
	defer logPanicAndDie(log.G(ctx))

	log.G(ctx).WithFields(logrus.Fields{"id": req.ID, "exec_id": req.ExecID}).Debug("kill")
	task, err := ts.taskManager.Task(req.ID)
	if err != nil {
		return nil, err
	}

	ctx = namespaces.WithNamespace(ctx, defaultNamespace)
	resp, err := task.Kill(ctx, req)
	if err != nil {
		log.G(ctx).WithError(err).Error("kill failed")
		return nil, err
	}

	log.G(ctx).Debug("kill succeeded")
	return resp, nil
}

// Exec runs an additional process inside the container
func (ts *TaskService) Exec(ctx context.Context, req *taskAPI.ExecProcessRequest) (*types.Empty, error) {
	defer logPanicAndDie(log.G(ctx))

	log.G(ctx).WithFields(logrus.Fields{"id": req.ID, "exec_id": req.ExecID}).Debug("exec")
	task, err := ts.taskManager.Task(req.ID)
	if err != nil {
		return nil, err
	}

	ctx = namespaces.WithNamespace(ctx, defaultNamespace)
	resp, err := task.Exec(ctx, req)
	if err != nil {
		log.G(ctx).WithError(err).Error("exec failed")
		return nil, err
	}

	log.G(ctx).Debug("exec succeeded")
	return resp, nil
}

// ResizePty resizes pty
func (ts *TaskService) ResizePty(ctx context.Context, req *taskAPI.ResizePtyRequest) (*types.Empty, error) {
	defer logPanicAndDie(log.G(ctx))

	log.G(ctx).WithFields(logrus.Fields{"id": req.ID, "exec_id": req.ExecID}).Debug("resize_pty")
	task, err := ts.taskManager.Task(req.ID)
	if err != nil {
		return nil, err
	}

	ctx = namespaces.WithNamespace(ctx, defaultNamespace)
	resp, err := task.ResizePty(ctx, req)
	if err != nil {
		log.G(ctx).WithError(err).Error("resize_pty failed")
		return nil, err
	}

	log.G(ctx).Debug("resize_pty succeeded")
	return resp, nil
}

// CloseIO closes all IO inside container
func (ts *TaskService) CloseIO(ctx context.Context, req *taskAPI.CloseIORequest) (*types.Empty, error) {
	defer logPanicAndDie(log.G(ctx))

	log.G(ctx).WithFields(logrus.Fields{"id": req.ID, "exec_id": req.ExecID}).Debug("close_io")
	task, err := ts.taskManager.Task(req.ID)
	if err != nil {
		return nil, err
	}

	ctx = namespaces.WithNamespace(ctx, defaultNamespace)
	resp, err := task.CloseIO(ctx, req)
	if err != nil {
		log.G(ctx).WithError(err).Error("close io failed")
		return nil, err
	}

	log.G(ctx).Debug("close io succeeded")
	return resp, nil
}

// Update updates running container
func (ts *TaskService) Update(ctx context.Context, req *taskAPI.UpdateTaskRequest) (*types.Empty, error) {
	defer logPanicAndDie(log.G(ctx))

	log.G(ctx).WithField("id", req.ID).Debug("update")
	task, err := ts.taskManager.Task(req.ID)
	if err != nil {
		return nil, err
	}

	ctx = namespaces.WithNamespace(ctx, defaultNamespace)
	resp, err := task.Update(ctx, req)
	if err != nil {
		log.G(ctx).WithError(err).Error("update failed")
		return nil, err
	}

	log.G(ctx).Debug("update succeeded")
	return resp, nil
}

// Wait waits for a process to exit
func (ts *TaskService) Wait(ctx context.Context, req *taskAPI.WaitRequest) (*taskAPI.WaitResponse, error) {
	defer logPanicAndDie(log.G(ctx))

	log.G(ctx).WithFields(logrus.Fields{"id": req.ID, "exec_id": req.ExecID}).Debug("wait")
	task, err := ts.taskManager.Task(req.ID)
	if err != nil {
		return nil, err
	}

	ctx = namespaces.WithNamespace(ctx, defaultNamespace)
	resp, err := task.Wait(ctx, req)
	if err != nil {
		log.G(ctx).WithError(err).Error("wait failed")
		return nil, err
	}

	log.G(ctx).WithField("exit_status", resp.ExitStatus).Debug("wait succeeded")
	return resp, nil
}

// Stats returns a process stats
func (ts *TaskService) Stats(ctx context.Context, req *taskAPI.StatsRequest) (*taskAPI.StatsResponse, error) {
	defer logPanicAndDie(log.G(ctx))

	log.G(ctx).WithField("id", req.ID).Debug("stats")
	task, err := ts.taskManager.Task(req.ID)
	if err != nil {
		return nil, err
	}

	ctx = namespaces.WithNamespace(ctx, defaultNamespace)
	resp, err := task.Stats(ctx, req)
	if err != nil {
		log.G(ctx).WithError(err).Error("stats failed")
		return nil, err
	}

	log.G(ctx).Debug("stats succeeded")
	return resp, nil
}

// taskAPI returns shim information such as the shim's pid
func (ts *TaskService) Connect(ctx context.Context, req *taskAPI.ConnectRequest) (*taskAPI.ConnectResponse, error) {
	defer logPanicAndDie(log.G(ctx))

	log.G(ctx).WithField("id", req.ID).Debug("connect")
	task, err := ts.taskManager.Task(req.ID)
	if err != nil {
		return nil, err
	}

	ctx = namespaces.WithNamespace(ctx, defaultNamespace)
	resp, err := task.Connect(ctx, req)
	if err != nil {
		log.G(ctx).WithError(err).Error("connect failed")
		return nil, err
	}

	log.G(ctx).WithFields(logrus.Fields{
		"shim_pid": resp.ShimPid,
		"task_pid": resp.TaskPid,
		"version":  resp.Version,
	}).Error("connect succeeded")
	return resp, nil
}

// Shutdown cleanups runc resources ans gracefully shutdowns ttrpc server
func (ts *TaskService) Shutdown(ctx context.Context, req *taskAPI.ShutdownRequest) (*types.Empty, error) {
	defer logPanicAndDie(log.G(ctx))

	log.G(ctx).WithFields(logrus.Fields{"id": req.ID, "now": req.Now}).Debug("shutdown")
	ctx = namespaces.WithNamespace(ctx, defaultNamespace)

	// We don't want to call runc.Shutdown here as it just os.Exits behind.
	// calling all cancels here for graceful shutdown instead and call runc Shutdown at end.
	ts.taskManager.RemoveAll(ctx)
	ts.cancel()

	log.G(ctx).Debug("going to gracefully shutdown agent")
	return &types.Empty{}, nil
}
