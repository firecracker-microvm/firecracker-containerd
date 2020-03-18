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

package vm

import (
	"context"
	"sync"
	"time"

	taskAPI "github.com/containerd/containerd/runtime/v2/task"
	"github.com/gogo/protobuf/types"
	"github.com/pkg/errors"
	"github.com/sirupsen/logrus"
)

const (
	// By default, once the task exits, wait defaultIOFlushTimeout for
	// the IO streams to close on their own before forcibly closing them.
	defaultIOFlushTimeout = 5 * time.Second
)

// TaskManager manages a set of tasks, ensuring that creation, deletion and
// io stream handling of the task happen in a synchronized manner. It's intended
// to contain logic common to both the host runtime shim and VM agent (which
// would otherwise be duped between the two).
type TaskManager interface {
	// CreateTask adds a task to be managed. It creates the task using the
	// provided request/TaskService and sets up IO streams using the provided
	// IOConnectorSet.
	// If the TaskManager was shutdown by a previous call to ShutdownIfEmpty, an
	// error will be returned.
	CreateTask(context.Context, *taskAPI.CreateTaskRequest, taskAPI.TaskService, IOProxy) (*taskAPI.CreateTaskResponse, error)

	// DeleteProcess removes a task or exec being managed from the TaskManager.
	// It deletes the process from the provided TaskService using the provided
	// request. It also blocks until all IO for the task has been flushed or a
	// hardcoded timeout is hit.
	DeleteProcess(context.Context, *taskAPI.DeleteRequest, taskAPI.TaskService) (*taskAPI.DeleteResponse, error)

	// ExecProcess adds an exec process to be managed. It must be an exec for a
	// Task already managed by the TaskManager.
	// If the TaskManager was shutdown by a previous call to ShutdownIfEmpty, an
	// error will be returned.
	ExecProcess(context.Context, *taskAPI.ExecProcessRequest, taskAPI.TaskService, IOProxy) (*types.Empty, error)

	// ShutdownIfEmpty will atomically check if there are no tasks left being managed and, if so,
	// prevent any new tasks from being added going forward (future attempts will return an error).
	// If any tasks are still in the TaskManager's map, no change will be made.
	// It returns a bool indicating whether TaskManager shut down as a result of the call.
	ShutdownIfEmpty() bool
}

// NewTaskManager initializes a new TaskManager
func NewTaskManager(shimCtx context.Context, logger *logrus.Entry) TaskManager {
	return &taskManager{
		tasks:   make(map[string]map[string]*vmProc),
		shimCtx: shimCtx,
		logger:  logger,
	}
}

type taskManager struct {
	mu sync.Mutex
	// tasks is a map of taskID to a map of execID->vmProc for that task
	tasks      map[string]map[string]*vmProc
	isShutdown bool

	shimCtx context.Context
	logger  *logrus.Entry
}

type vmProc struct {
	taskID string
	execID string
	logger *logrus.Entry

	ctx        context.Context
	cancel     context.CancelFunc
	ioInitDone <-chan error
	ioCopyDone <-chan error
}

func (m *taskManager) newProc(taskID, execID string) (*vmProc, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.isShutdown {
		return nil, errors.Errorf("cannot create new exec %q in task %q after shutdown", execID, taskID)
	}

	_, taskExists := m.tasks[taskID]
	if !taskExists {
		if execID != "" {
			return nil, errors.Errorf("cannot add exec %q to non-existent task %q", execID, taskID)
		}

		m.tasks[taskID] = make(map[string]*vmProc)
	}

	_, procExists := m.tasks[taskID][execID]
	if procExists {
		return nil, errors.Errorf("cannot add duplicate exec %q to task %q", execID, taskID)
	}

	proc := &vmProc{}
	proc.taskID = taskID
	proc.execID = execID
	proc.logger = m.logger.WithField("TaskID", taskID).WithField("ExecID", execID)
	proc.ctx, proc.cancel = context.WithCancel(m.shimCtx)

	m.tasks[taskID][execID] = proc
	return proc, nil
}

func (m *taskManager) deleteProc(taskID, execID string) (*vmProc, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	_, ok := m.tasks[taskID]
	if !ok {
		return nil, errors.Errorf("cannot delete exec %q from non-existent task %q", execID, taskID)
	}

	proc, ok := m.tasks[taskID][execID]
	if !ok {
		return nil, errors.Errorf("cannot delete non-existent exec %q from task %q", execID, taskID)
	}

	delete(m.tasks[taskID], execID)
	if len(m.tasks[taskID]) == 0 {
		delete(m.tasks, taskID)
	}

	return proc, nil
}

func (m *taskManager) ShutdownIfEmpty() bool {
	m.mu.Lock()
	defer m.mu.Unlock()

	if len(m.tasks) == 0 {
		m.isShutdown = true
		return true
	}

	return false
}

func (m *taskManager) CreateTask(
	reqCtx context.Context,
	req *taskAPI.CreateTaskRequest,
	taskService taskAPI.TaskService,
	ioProxy IOProxy,
) (_ *taskAPI.CreateTaskResponse, err error) {
	taskID := req.ID
	execID := "" // ExecID of initial process in task is empty string by containerd convention

	proc, err := m.newProc(taskID, execID)
	if err != nil {
		return nil, err
	}

	defer func() {
		if err != nil {
			proc.cancel()
			m.deleteProc(taskID, execID)
		}
	}()

	// Begin initializing stdio, but don't block on the initialization so we can send the Create
	// call (which will allow the stdio initialization to complete).
	proc.ioInitDone, proc.ioCopyDone = ioProxy.start(proc)

	createResp, err := taskService.Create(reqCtx, req)
	if err != nil {
		return nil, err
	}

	// make sure stdio was initialized successfully
	err = <-proc.ioInitDone
	if err != nil {
		return nil, err
	}

	proc.logger.WithField("pid_in_vm", createResp.Pid).Info("successfully created task")

	go m.monitorExit(proc, taskService)
	return createResp, nil
}

func (m *taskManager) ExecProcess(
	reqCtx context.Context,
	req *taskAPI.ExecProcessRequest,
	taskService taskAPI.TaskService,
	ioProxy IOProxy,
) (_ *types.Empty, err error) {
	taskID := req.ID
	execID := req.ExecID

	proc, err := m.newProc(taskID, execID)
	if err != nil {
		return nil, err
	}

	defer func() {
		if err != nil {
			proc.cancel()
			m.deleteProc(taskID, execID)
		}
	}()

	// Begin initializing stdio, but don't block on the initialization so we can send the Exec
	// call (which will allow the stdio initialization to complete).
	proc.ioInitDone, proc.ioCopyDone = ioProxy.start(proc)

	execResp, err := taskService.Exec(reqCtx, req)
	if err != nil {
		return nil, err
	}

	// make sure stdio was initialized successfully
	err = <-proc.ioInitDone
	if err != nil {
		return nil, err
	}

	go m.monitorExit(proc, taskService)
	return execResp, nil
}

func (m *taskManager) DeleteProcess(
	reqCtx context.Context,
	req *taskAPI.DeleteRequest,
	taskService taskAPI.TaskService,
) (*taskAPI.DeleteResponse, error) {
	resp, err := taskService.Delete(reqCtx, req)
	if err != nil {
		return nil, err
	}

	proc, err := m.deleteProc(req.ID, req.ExecID)
	if err != nil {
		return nil, err
	}

	// Wait for the io streams to be flushed+closed before returning. Error is ignored
	// here as any io error is already logged in the IOProxy implementation directly.
	// Timeouts on waiting to close this channel after process exit are handled by the
	// IOProxy implementation being used for this proc. There's no extra timeout here
	// in order to keep things simpler.
	<-proc.ioCopyDone

	return resp, nil
}

func (m *taskManager) monitorExit(proc *vmProc, taskService taskAPI.TaskService) {
	waitResp, waitErr := taskService.Wait(proc.ctx, &taskAPI.WaitRequest{
		ID:     proc.taskID,
		ExecID: proc.execID,
	})
	proc.cancel()

	if waitErr == context.Canceled {
		return
	}

	if waitErr != nil {
		proc.logger.WithError(waitErr).Error("error waiting for exit")
	} else {
		proc.logger.
			WithField("exit_status", waitResp.ExitStatus).
			WithField("exited_at", waitResp.ExitedAt).
			Info("exited")
	}
}
