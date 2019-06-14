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

package vm

import (
	"context"
	"sync"
	"time"

	taskAPI "github.com/containerd/containerd/runtime/v2/task"
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

	// DeleteTask removes a task being managed from the TaskManager. It deletes
	// the task from the provided TaskService using the provided request. It
	// also blocks until all IO for the task has been flushed or a timeout is hit.
	DeleteTask(context.Context, *taskAPI.DeleteRequest, taskAPI.TaskService) (*taskAPI.DeleteResponse, error)

	// ShutdownIfEmpty will atomically check if there are no tasks left being managed and, if so,
	// prevent any new tasks from being added going forward (future attempts will return an error).
	// If any tasks are still in the TaskManager's map, no change will be made.
	// It returns a bool indicating whether TaskManager shut down as a result of the call.
	ShutdownIfEmpty() bool
}

// NewTaskManager initializes a new TaskManager
func NewTaskManager(shimCtx context.Context, logger *logrus.Entry) TaskManager {
	return &taskManager{
		tasks:   make(map[string]*vmTask),
		shimCtx: shimCtx,
		logger:  logger,
	}
}

type taskManager struct {
	mu         sync.Mutex
	tasks      map[string]*vmTask
	isShutdown bool

	shimCtx context.Context
	logger  *logrus.Entry
}

type vmTask struct {
	ID     string
	logger *logrus.Entry

	ctx        context.Context
	cancel     context.CancelFunc
	ioInitDone <-chan error
	ioCopyDone <-chan error
}

func (m *taskManager) newTask(taskID string) (*vmTask, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.isShutdown {
		return nil, errors.New("cannot create new task after shutdown")
	}

	_, taskExists := m.tasks[taskID]
	if taskExists {
		return nil, errors.Errorf("cannot create duplicate task %q", taskID)
	}

	taskCtx, taskCancel := context.WithCancel(m.shimCtx)
	task := &vmTask{
		ID:     taskID,
		logger: m.logger.WithField("TaskID", taskID),
		ctx:    taskCtx,
		cancel: taskCancel,
	}
	m.tasks[taskID] = task
	return task, nil
}

func (m *taskManager) deleteTask(taskID string) (*vmTask, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	task, ok := m.tasks[taskID]
	if !ok {
		return nil, errors.Errorf("cannot delete non-existent task %q", taskID)
	}

	delete(m.tasks, taskID)
	return task, nil
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
	task, err := m.newTask(req.ID)
	if err != nil {
		return nil, err
	}

	defer func() {
		if err != nil {
			task.cancel()
			m.deleteTask(req.ID)
		}
	}()

	// Begin initializing stdio, but don't block on the initialization so we can send the Create
	// call (which will allow the stdio initialization to complete).
	task.ioInitDone, task.ioCopyDone = ioProxy.start(task)

	createResp, err := taskService.Create(reqCtx, req)
	if err != nil {
		return nil, err
	}

	// make sure stdio was initialized successfully
	err = <-proc.ioInitDone
	if err != nil {
		return nil, err
	}

	task.logger.WithField("pid_in_vm", createResp.Pid).Info("successfully created task")

	go m.monitorExit(task, taskService)
	return createResp, nil
}

func (m *taskManager) DeleteTask(
	reqCtx context.Context,
	req *taskAPI.DeleteRequest,
	taskService taskAPI.TaskService,
) (*taskAPI.DeleteResponse, error) {
	resp, err := taskService.Delete(reqCtx, req)
	if err != nil {
		return nil, err
	}

	task, err := m.deleteTask(req.ID)
	if err != nil {
		return nil, err
	}

	// Wait for the io streams to be flushed+closed before returning. Error is ignored
	// here as any io error is already logged in the IOProxy implementation directly.
	// Timeouts on waiting to close this channel after process exit are handled by the
	// IOProxy implementation being used for this proc. There's no extra timeout here
	// in order to keep things simpler.
	<-task.ioCopyDone

	return resp, nil
}

func (m *taskManager) monitorExit(task *vmTask, taskService taskAPI.TaskService) {
	waitResp, waitErr := taskService.Wait(task.ctx, &taskAPI.WaitRequest{
		ID: task.ID,
	})
	task.cancel()

	if waitErr == context.Canceled {
		return
	}

	if waitErr != nil {
		task.logger.WithError(waitErr).Error("error waiting on task")
	} else {
		task.logger.
			WithField("exit_status", waitResp.ExitStatus).
			WithField("exited_at", waitResp.ExitedAt).
			Info("task exited")
	}
}
