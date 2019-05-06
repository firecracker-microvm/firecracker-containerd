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
	"io"
	"net"
	"sync"
	"syscall"

	"github.com/containerd/containerd/cio"
	taskAPI "github.com/containerd/containerd/runtime/v2/task"
	"github.com/containerd/fifo"
	ptypes "github.com/gogo/protobuf/types"
	"github.com/pkg/errors"
	"github.com/sirupsen/logrus"

	"github.com/firecracker-microvm/firecracker-containerd/internal"
	"github.com/firecracker-microvm/firecracker-containerd/internal/bundle"
	"github.com/firecracker-microvm/firecracker-containerd/proto"
)

// IODirection represents the direction in which container related I/O should
// flow between a FIFO and a vsock connection (the possible directions being
// from FIFO to vsock or from vsock to FIFO). This is needed to parameterize
// functions that operate on either Host or Guest (in which case I/O directions
// for the same stream are inverses) and functions that operate on stdin vs.
// stdout/stderr (which again have inverse directions).
type IODirection bool

const (
	// FIFOtoVSock represents I/O that should be copied from a FIFO to a VSock.
	FIFOtoVSock IODirection = true

	// VSockToFIFO represents I/O that should be copied from a VSock to a FIFO.
	VSockToFIFO IODirection = false
)

func (d IODirection) opposite() IODirection {
	return IODirection(!bool(d))
}

// VSockConnector is a function used as a callback for obtaining a vsock connection.
// Implementations may include one that dials a remote vsock listener and one that
// listens and accepts incoming connections.
type VSockConnector func(ctx context.Context, port uint32) (net.Conn, error)

// TaskManager manages a mapping of containerIDs to metadata for containerd tasks
// being executed via a firecracker-containerd runtime. It's intended to be
// abstracted over whether it's being executed on the Host or inside a VM Guest.
type TaskManager interface {
	AddTask(string, taskAPI.TaskService, bundle.Dir, *proto.ExtraData, *cio.FIFOSet, <-chan struct{}, context.CancelFunc) (*Task, error)
	Task(string) (*Task, error)
	TaskCount() uint
	Remove(string)
	RemoveAll()
}

// NewTaskManager initializes a new TaskManager
func NewTaskManager(logger *logrus.Entry) TaskManager {
	return &taskManager{
		tasks:  make(map[string]*Task),
		logger: logger,
	}
}

type taskManager struct {
	mu     sync.RWMutex
	tasks  map[string]*Task
	logger *logrus.Entry
}

// AddTask registers a task with the provided metadata with the taskManager.
// taskService should implement the TaskService API for the task (i.e. Create, Kill, Exec, etc.).
func (m *taskManager) AddTask(
	containerID string,
	taskService taskAPI.TaskService,
	bundleDir bundle.Dir,
	extraData *proto.ExtraData,
	fifoSet *cio.FIFOSet,
	doneCh <-chan struct{},
	cancel context.CancelFunc,
) (*Task, error) {
	err := bundleDir.Create() // no-op if it already exists
	if err != nil {
		return nil, err
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	if _, ok := m.tasks[containerID]; ok {
		return nil, errors.Errorf("cannot add duplicate task with container ID %q", containerID)
	}

	task := &Task{
		TaskService: taskService,
		ID:          containerID,
		logger:      m.logger.WithField("id", containerID),

		doneCh: doneCh,
		cancel: cancel,

		extraData: extraData,
		bundleDir: bundleDir,
		fifoSet:   fifoSet,
	}

	m.tasks[task.ID] = task
	return task, nil
}

func (m *taskManager) get(containerID string) (*Task, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	task, ok := m.tasks[containerID]
	if !ok {
		return nil, errors.Errorf("task with containerID %q not found ", containerID)
	}

	return task, nil
}

// assumes caller is holding m.mu lock
func (m *taskManager) remove(containerID string, task *Task) {
	delete(m.tasks, containerID)
	task.cancel()
}

// Task returns the TaskService API for the task with the provided containerID, allowing
// the caller to perform APIs like Create, Kill, Exec, etc. on the task.
func (m *taskManager) Task(containerID string) (*Task, error) {
	return m.get(containerID)
}

// TaskCount returns the number of tasks registered with the taskManager.
func (m *taskManager) TaskCount() uint {
	m.mu.RLock()
	defer m.mu.RUnlock()

	return uint(len(m.tasks))
}

// Remove unregisters the task with the provided containerID with the taskManager and cancels
// any ongoing I/O proxying for the task.
func (m *taskManager) Remove(containerID string) {
	m.mu.Lock()
	defer m.mu.Unlock()

	task, ok := m.tasks[containerID]
	if !ok {
		// the task was already removed or just never existed, no-op
		return
	}

	m.remove(containerID, task)
}

// RemoveAll calls Remove() on all tasks registered with the taskManager.
func (m *taskManager) RemoveAll() {
	m.mu.Lock()
	defer m.mu.Unlock()

	for containerID, task := range m.tasks {
		m.remove(containerID, task)
	}
}

// Task represents a containerd Task under the control of a taskManager
type Task struct {
	taskAPI.TaskService
	ID     string
	logger *logrus.Entry

	doneCh <-chan struct{}
	cancel context.CancelFunc

	extraData *proto.ExtraData
	bundleDir bundle.Dir
	fifoSet   *cio.FIFOSet
}

// ExtraData returns the extra data the VM Agent needs to execute this task
func (task *Task) ExtraData() *proto.ExtraData {
	return task.extraData
}

// MarshalExtraData serializes the task in preparation for transport over vsock
func (task *Task) MarshalExtraData() (*ptypes.Any, error) {
	return ptypes.MarshalAny(task.extraData)
}

// BundleDir returns the bundle dir for the task
func (task *Task) BundleDir() bundle.Dir {
	return task.bundleDir
}

// StartStdioProxy sets up stdin, stdout and stderr streams for the task. It returns a channel that will be closed once
// *initialization* of the streams has completed (or, if there was an error, the error will be published and then the chan
// will be closed). Initialization consists of opening the FIFOs and establishing the vsock connections. After initialization,
// data will be copied asynchronously until an error is encountered or task.Cancel() is called.
//
// Doing the setup asynchronously allows the caller to perform other actions that may be necessary for the initialization to
// complete (such as sending a request over vsock to the other side of the Host/Guest barrier).
//
// inputDirection is the IODirection that should be used for *stdin*. Stdout/stderr will, by definition, go in the opposite direction.
// vsockConnector is a callback for establishing a vsock connection.
//
// initializeCtx is used exclusively during initialization, not during the actual IO copying. To stop IO copying, the task should be
// removed from the TaskManager it was added to.
func (task *Task) StartStdioProxy(initializeCtx context.Context, inputDirection IODirection, vsockConnector VSockConnector) <-chan error {
	initializeCtx, initializeCancel := context.WithCancel(initializeCtx)
	returnCh := make(chan error)

	var wg sync.WaitGroup
	errCh := make(chan error, 3)

	// For each of stdin, stdout and stderr, if a FIFO has been set (i.e. the client requested that stream to be setup), go
	// open the FIFO and connect the vsock.
	if task.fifoSet.Stdin != "" {
		wg.Add(1)
		go func() {
			errCh <- task.proxyIO(initializeCtx, task.fifoSet.Stdin, task.extraData.StdinPort, inputDirection, vsockConnector)
			wg.Done()
		}()
	} else {
		task.logger.Info("skipping proxy io for unset stdin")
	}

	if task.fifoSet.Stdout != "" {
		wg.Add(1)
		go func() {
			errCh <- task.proxyIO(initializeCtx, task.fifoSet.Stdout, task.extraData.StdoutPort, inputDirection.opposite(), vsockConnector)
			wg.Done()
		}()
	} else {
		task.logger.Info("skipping proxy io for unset stdout")
	}

	if task.fifoSet.Stderr != "" {
		wg.Add(1)
		go func() {
			errCh <- task.proxyIO(initializeCtx, task.fifoSet.Stderr, task.extraData.StderrPort, inputDirection.opposite(), vsockConnector)
			wg.Done()
		}()
	} else {
		task.logger.Info("skipping proxy io for unset stderr")
	}

	// once each stream is initialized, close the error chan
	go func() {
		defer close(errCh)
		wg.Wait()
	}()

	// Once each stream has been initialized, get any error it returned and let the caller know by publishing
	// on the return chan
	go func() {
		defer close(returnCh)
		for err := range errCh {
			if err != nil {
				initializeCancel()
				returnCh <- err
				return
			}
		}
	}()

	return returnCh
}

func (task *Task) proxyIO(initializeCtx context.Context, fifoPath string, port uint32, ioDirection IODirection, vsockConnector VSockConnector) error {
	logger := task.logger.WithField("path", fifoPath).WithField("port", port)

	fifoFile, err := fifo.OpenFifo(initializeCtx, fifoPath, syscall.O_RDWR|syscall.O_CREAT|syscall.O_NONBLOCK, 0700)
	if err != nil {
		logger.WithError(err).Error("failed to open fifo")
		return err
	}

	sock, err := vsockConnector(initializeCtx, port)
	if err != nil {
		fifoFile.Close()
		logger.WithError(err).Error("failed to establish vsock connection")
		return err
	}

	// the FIFO and vsock have been opened, so go off proxying between them asynchronously
	// until Cancel is called.
	go func() {
		buf := make([]byte, internal.DefaultBufferSize)

		logger.Debug("begin copying io")

		switch ioDirection {
		case FIFOtoVSock:
			go func() {
				<-task.doneCh
				// If we are copying from FIFO to vsock, be sure to first close just the FIFO,
				// allowing any buffered data to be flushed into the vsock first.
				fifoFile.Close()
			}()

			_, err = io.CopyBuffer(sock, fifoFile, buf)
			sock.Close()

		case VSockToFIFO:
			go func() {
				<-task.doneCh
				// If we are copying from vsock to FIFO, be sure to first close just the vsock,
				// allowing any buffered data to be flushed into the FIFO first.
				sock.Close()
			}()

			_, err = io.CopyBuffer(fifoFile, sock, buf)
			fifoFile.Close()

		default:
			logger.Fatalf("undefined io direction") // should not be possible, direction is a bool
		}

		// All FIFO+vsocks should be closed by this point in the previous switch cases.
		if err != nil {
			logger.WithError(err).Error("error with stdio")
		}
	}()

	return nil
}
