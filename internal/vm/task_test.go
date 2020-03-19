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
	"bytes"
	"context"
	"io"
	"net"
	"sync"
	"testing"
	"time"

	"github.com/containerd/containerd/runtime/v2/task"
	taskAPI "github.com/containerd/containerd/runtime/v2/task"
	"github.com/gogo/protobuf/types"
	"github.com/pkg/errors"
	"github.com/sirupsen/logrus"
	"github.com/sirupsen/logrus/hooks/test"
	"github.com/stretchr/testify/require"
)

type mockTaskService struct {
	task.TaskService

	// map of taskID to chan *taskAPI.CreateTaskRequest holding each Create request for that taskID
	createRequests sync.Map

	// map of taskID to chan *taskAPI.ExecProcessRequest holding each Exec request for that taskID
	execRequests sync.Map

	// map of taskID to chan *taskAPI.DeleteRequest holding each Delete request for that taskID
	deleteRequests sync.Map

	// map of taskID to chan *taskAPI.WaitRequest holding each Wait request for that taskID
	waitRequests sync.Map

	// map of "taskID/execID" to chan struct{} that will be used to block Wait requests
	waitCh sync.Map
}

func (s *mockTaskService) Create(reqCtx context.Context, req *taskAPI.CreateTaskRequest) (*taskAPI.CreateTaskResponse, error) {
	reqCh, _ := s.createRequests.LoadOrStore(req.ID, make(chan *taskAPI.CreateTaskRequest, 128))
	reqCh.(chan *taskAPI.CreateTaskRequest) <- req
	return &taskAPI.CreateTaskResponse{
		Pid: 123,
	}, nil
}

func (s *mockTaskService) PopCreateRequests(id string) (reqs []*taskAPI.CreateTaskRequest) {
	reqCh, _ := s.createRequests.Load(id)
	if reqCh == nil {
		return reqs
	}

	for {
		select {
		case req := <-(reqCh.(chan *taskAPI.CreateTaskRequest)):
			reqs = append(reqs, req)
		default:
			return reqs
		}
	}
}

func (s *mockTaskService) Exec(reqCtx context.Context, req *taskAPI.ExecProcessRequest) (*types.Empty, error) {
	reqCh, _ := s.execRequests.LoadOrStore(req.ID, make(chan *taskAPI.ExecProcessRequest, 128))
	reqCh.(chan *taskAPI.ExecProcessRequest) <- req
	return nil, nil
}

func (s *mockTaskService) PopExecRequests(id string) (reqs []*taskAPI.ExecProcessRequest) {
	reqCh, _ := s.execRequests.Load(id)
	if reqCh == nil {
		return reqs
	}

	for {
		select {
		case req := <-(reqCh.(chan *taskAPI.ExecProcessRequest)):
			reqs = append(reqs, req)
		default:
			return reqs
		}
	}
}

func (s *mockTaskService) Delete(reqCtx context.Context, req *taskAPI.DeleteRequest) (*taskAPI.DeleteResponse, error) {
	reqCh, _ := s.deleteRequests.LoadOrStore(req.ID, make(chan *taskAPI.DeleteRequest, 128))
	reqCh.(chan *taskAPI.DeleteRequest) <- req
	return nil, nil
}

func (s *mockTaskService) PopDeleteRequests(id string) (reqs []*taskAPI.DeleteRequest) {
	reqCh, _ := s.deleteRequests.Load(id)
	if reqCh == nil {
		return reqs
	}

	for {
		select {
		case req := <-(reqCh.(chan *taskAPI.DeleteRequest)):
			reqs = append(reqs, req)
		default:
			return reqs
		}
	}
}

func (s *mockTaskService) processID(taskID, execID string) string {
	return taskID + "/" + execID
}

func (s *mockTaskService) Wait(reqCtx context.Context, req *taskAPI.WaitRequest) (*taskAPI.WaitResponse, error) {
	reqCh, _ := s.waitRequests.LoadOrStore(req.ID, make(chan *taskAPI.WaitRequest, 128))
	reqCh.(chan *taskAPI.WaitRequest) <- req

	waitCh, _ := s.waitCh.Load(s.processID(req.ID, req.ExecID))
	if waitCh != nil {
		<-(waitCh.(chan struct{}))
	}

	return &taskAPI.WaitResponse{
		ExitStatus: 0,
		ExitedAt:   time.Now(),
	}, nil
}

func (s *mockTaskService) PopWaitRequests(id string) (reqs []*taskAPI.WaitRequest) {
	reqCh, _ := s.waitRequests.Load(id)
	if reqCh == nil {
		return reqs
	}

	for {
		select {
		case req := <-(reqCh.(chan *taskAPI.WaitRequest)):
			reqs = append(reqs, req)
		default:
			return reqs
		}
	}
}

func (s *mockTaskService) SetWaitCh(taskID, execID string, waitCh chan struct{}) {
	s.waitCh.Store(s.processID(taskID, execID), waitCh)
}

func mockIOConnector(conn io.ReadWriteCloser) IOConnector {
	return func(_ context.Context, _ *logrus.Entry) <-chan IOConnectorResult {
		returnCh := make(chan IOConnectorResult)
		go func() {
			defer close(returnCh)
			time.Sleep(100 * time.Millisecond) // simulate async stuff taking a little bit

			returnCh <- IOConnectorResult{ReadWriteCloser: conn}
		}()

		return returnCh
	}
}

func mockIOFailConnector(conn io.ReadWriteCloser) IOConnector {
	return func(_ context.Context, _ *logrus.Entry) <-chan IOConnectorResult {
		returnCh := make(chan IOConnectorResult)
		go func() {
			defer close(returnCh)
			time.Sleep(100 * time.Millisecond) // simulate async stuff taking a little bit
			conn.Close()
			returnCh <- IOConnectorResult{Err: errors.New("nothing works")}
		}()

		return returnCh
	}
}

type mockProc struct {
	TaskID string
	ExecID string

	WaitCh         chan struct{}
	IOConnectorSet *ioConnectorSet

	stdinInput  io.ReadWriteCloser
	stdinOutput *bytes.Buffer

	stdoutInput  io.ReadWriteCloser
	stdoutOutput *bytes.Buffer

	stderrInput  io.ReadWriteCloser
	stderrOutput *bytes.Buffer

	ioDone *sync.WaitGroup
}

func (t *mockProc) WriteStdin(bytes []byte) error {
	_, err := t.stdinInput.Write(bytes)
	return err
}

func (t *mockProc) WriteStdout(bytes []byte) error {
	_, err := t.stdoutInput.Write(bytes)
	return err
}

func (t *mockProc) WriteStderr(bytes []byte) error {
	_, err := t.stderrInput.Write(bytes)
	return err
}

func (t *mockProc) CloseStdin() {
	t.stdinInput.Close()
}

func (t *mockProc) CloseStdout() {
	t.stdoutInput.Close()
}

func (t *mockProc) CloseStderr() {
	t.stderrInput.Close()
}

func (t *mockProc) StdinOutput() []byte {
	t.ioDone.Wait()
	return t.stdinOutput.Bytes()
}

func (t *mockProc) StdoutOutput() []byte {
	t.ioDone.Wait()
	return t.stdoutOutput.Bytes()
}

func (t *mockProc) StderrOutput() []byte {
	t.ioDone.Wait()
	return t.stderrOutput.Bytes()
}

func newMockProc(
	taskID, execID string,
	getStdinConnector func(io.ReadWriteCloser) IOConnector,
	getStdoutConnector func(io.ReadWriteCloser) IOConnector,
	getStderrConnector func(io.ReadWriteCloser) IOConnector,
) *mockProc {
	var ioDone sync.WaitGroup

	taskStdinR, taskStdinW := net.Pipe()
	clientStdinR, clientStdinW := net.Pipe()
	var stdinBuf bytes.Buffer
	ioDone.Add(1)
	go func() {
		defer ioDone.Done()
		io.Copy(&stdinBuf, taskStdinR)
	}()

	taskStdoutR, taskStdoutW := net.Pipe()
	clientStdoutR, clientStdoutW := net.Pipe()
	var stdoutBuf bytes.Buffer
	ioDone.Add(1)
	go func() {
		defer ioDone.Done()
		io.Copy(&stdoutBuf, clientStdoutR)
	}()

	taskStderrR, taskStderrW := net.Pipe()
	clientStderrR, clientStderrW := net.Pipe()
	var stderrBuf bytes.Buffer
	ioDone.Add(1)
	go func() {
		defer ioDone.Done()
		io.Copy(&stderrBuf, clientStderrR)
	}()

	return &mockProc{
		TaskID: taskID,
		ExecID: execID,
		WaitCh: make(chan struct{}),

		IOConnectorSet: &ioConnectorSet{
			stdin: &IOConnectorPair{
				ReadConnector:  getStdinConnector(clientStdinR),
				WriteConnector: getStdinConnector(taskStdinW),
			},
			stdout: &IOConnectorPair{
				ReadConnector:  getStdoutConnector(taskStdoutR),
				WriteConnector: getStdoutConnector(clientStdoutW),
			},
			stderr: &IOConnectorPair{
				ReadConnector:  getStderrConnector(taskStderrR),
				WriteConnector: getStderrConnector(clientStderrW),
			},
		},

		stdinInput:  clientStdinW,
		stdinOutput: &stdinBuf,

		stdoutInput:  taskStdoutW,
		stdoutOutput: &stdoutBuf,

		stderrInput:  taskStderrW,
		stderrOutput: &stderrBuf,

		ioDone: &ioDone,
	}
}

func TestTaskManager_CreateExecDeleteTask(t *testing.T) {
	shimCtx, shimCancel := context.WithCancel(context.Background())
	defer shimCancel()

	logger, logHook := test.NewNullLogger()
	logger.SetLevel(logrus.DebugLevel)
	defer func() {
		for _, entry := range logHook.AllEntries() {
			logLine, _ := entry.String()
			t.Log(logLine)
		}
	}()

	tm := NewTaskManager(shimCtx, logger.WithField("test", t.Name()))
	ts := &mockTaskService{}

	mockTask := newMockProc("fakeTask", "", mockIOConnector, mockIOConnector, mockIOConnector)
	ts.SetWaitCh(mockTask.TaskID, mockTask.ExecID, mockTask.WaitCh)

	// Create a task, simulate data on stdin, stdout, stderr
	createReqCtx, createReqCancel := context.WithCancel(shimCtx)
	_, err := tm.CreateTask(createReqCtx, &taskAPI.CreateTaskRequest{ID: mockTask.TaskID}, ts, mockTask.IOConnectorSet)
	require.NoError(t, err, "create task failed")
	createReqCancel()

	taskStdinData := []byte("stdin")
	err = mockTask.WriteStdin(taskStdinData)
	require.NoError(t, err, "write stdin failed")

	taskFirstStdoutData := []byte("stdout1")
	err = mockTask.WriteStdout(taskFirstStdoutData)
	require.NoError(t, err, "first write stdout to failed")

	taskFirstStderrData := []byte("stderr1")
	err = mockTask.WriteStderr(taskFirstStderrData)
	require.NoError(t, err, "first write stderr to failed")

	// Exec a process, simulate stdio
	mockExec := newMockProc(mockTask.TaskID, "fakeExec", mockIOConnector, mockIOConnector, mockIOConnector)
	ts.SetWaitCh(mockExec.TaskID, mockExec.ExecID, mockExec.WaitCh)

	execReqCtx, execReqCancel := context.WithCancel(shimCtx)
	_, err = tm.ExecProcess(execReqCtx, &taskAPI.ExecProcessRequest{
		ID:     mockExec.TaskID,
		ExecID: mockExec.ExecID,
	}, ts, mockExec.IOConnectorSet)
	require.NoError(t, err, "exec failed")
	execReqCancel()

	execStdinData := []byte("execstdin")
	err = mockExec.WriteStdin(execStdinData)
	require.NoError(t, err, "write exec stdin failed")

	execStdoutData := []byte("execstdout")
	err = mockExec.WriteStdout(execStdoutData)
	require.NoError(t, err, "write exec stdout failed")

	execStderrData := []byte("execstderr")
	err = mockExec.WriteStderr(execStderrData)
	require.NoError(t, err, "write exec stderr failed")

	// simulate the exec exiting
	close(mockExec.WaitCh)
	mockExec.CloseStdin()
	mockExec.CloseStdout()
	mockExec.CloseStderr()

	deleteReqCtx, deleteReqCancel := context.WithCancel(shimCtx)
	_, err = tm.DeleteProcess(deleteReqCtx, &taskAPI.DeleteRequest{
		ID:     mockExec.TaskID,
		ExecID: mockExec.ExecID,
	}, ts)
	require.NoError(t, err, "delete exec failed")
	deleteReqCancel()

	// Verify exec had io proxied transparently
	require.Equalf(t, execStdinData, mockExec.StdinOutput(), "unexpected stdin data proxied for exec %q", mockExec.ExecID)
	require.Equalf(t, execStdoutData, mockExec.StdoutOutput(), "unexpected stdout data proxied for exec %q", mockTask.ExecID)
	require.Equalf(t, execStderrData, mockExec.StderrOutput(), "unexpected stderr data proxied for exec %q", mockTask.ExecID)

	// simulate the task exiting
	close(mockTask.WaitCh)
	mockTask.CloseStdin()

	// test that lingering stdout/stderr can still be flushed after the task exits (actual assertions happen at end of test)
	taskSecondStdoutData := []byte("stdout2")
	err = mockTask.WriteStdout(taskSecondStdoutData)
	require.NoError(t, err, "second write to stdout failed")
	mockTask.CloseStdout()

	taskSecondStderrData := []byte("stderr2")
	err = mockTask.WriteStderr(taskSecondStderrData)
	require.NoError(t, err, "second write stderr to failed")
	mockTask.CloseStderr()

	// delete the task, verifying that the shutdown fails before and succeeds after
	didShutdown := tm.ShutdownIfEmpty()
	require.False(t, didShutdown, "task manager shutdown while tasks not deleted")

	deleteReqCtx, deleteReqCancel = context.WithCancel(shimCtx)
	_, err = tm.DeleteProcess(deleteReqCtx, &taskAPI.DeleteRequest{ID: mockTask.TaskID}, ts)
	require.NoError(t, err, "delete task failed")
	deleteReqCancel()

	didShutdown = tm.ShutdownIfEmpty()
	require.True(t, didShutdown, "task manager didn't shutdown when all tasks deleted")

	tooLateTask := newMockProc("too.late", "", mockIOConnector, mockIOConnector, mockIOConnector)
	createReqCtx, createReqCancel = context.WithCancel(shimCtx)
	_, err = tm.CreateTask(createReqCtx, &taskAPI.CreateTaskRequest{ID: tooLateTask.TaskID}, ts, tooLateTask.IOConnectorSet)
	require.Error(t, err, "create unexpectedly succeeded after shutdown")
	createReqCancel()

	// Verify task had io proxied transparently
	require.Equalf(t, taskStdinData, mockTask.StdinOutput(), "unexpected stdin data proxied for task %q", mockTask.TaskID)
	require.Equalf(t, append(taskFirstStdoutData, taskSecondStdoutData...), mockTask.StdoutOutput(), "unexpected stdout data proxied for task %q", mockTask.TaskID)
	require.Equalf(t, append(taskFirstStderrData, taskSecondStderrData...), mockTask.StderrOutput(), "unexpected stderr data proxied for task %q", mockTask.TaskID)

	// Verify all tasks had all APIs called as expected
	mockTaskCreateReqs := ts.PopCreateRequests(mockTask.TaskID)
	require.Lenf(t, mockTaskCreateReqs, 1, "Create called unexpected number of times for %q", mockTask.TaskID)
	for _, req := range mockTaskCreateReqs {
		require.Equalf(t, mockTask.TaskID, req.ID, "unexpected ID in Create request for %q", mockTask.TaskID)
	}

	mockTaskExecReqs := ts.PopExecRequests(mockTask.TaskID)
	require.Lenf(t, mockTaskExecReqs, 1, "Exec called unexpected number of times for %q", mockTask.TaskID)
	for _, req := range mockTaskExecReqs {
		require.Equalf(t, mockExec.TaskID, req.ID, "unexpected ID in Exec request for task %q exec %q", mockExec.TaskID, mockExec.ExecID)
		require.Equalf(t, mockExec.ExecID, req.ExecID, "unexpected ExecID in Exec request for task %q exec %q", mockExec.TaskID, mockExec.ExecID)
	}

	mockTaskDeleteReqs := ts.PopDeleteRequests(mockTask.TaskID)
	require.Lenf(t, mockTaskDeleteReqs, 2, "Delete called unexpected number of times for %q", mockTask.TaskID)
	for _, req := range mockTaskDeleteReqs {
		require.Equalf(t, mockTask.TaskID, req.ID, "unexpected ID in Delete request for %q", mockTask.TaskID)
		if req.ExecID != "" {
			require.Equalf(t, mockExec.ExecID, req.ExecID, "unexpected ExecID in Delete request for %q", mockExec.ExecID)
		}
	}

	mockTaskWaitReqs := ts.PopWaitRequests(mockTask.TaskID)
	require.Lenf(t, mockTaskWaitReqs, 2, "Wait called unexpected number of times for %q", mockTask.TaskID)
	for _, req := range mockTaskWaitReqs {
		require.Equalf(t, mockTask.TaskID, req.ID, "unexpected ID in Wait request for %q", mockTask.TaskID)
		if req.ExecID != "" {
			require.Equalf(t, mockExec.ExecID, req.ExecID, "unexpected ExecID in Wait request for %q", mockExec.ExecID)
		}
	}

	tooLateTaskCreateReqs := ts.PopCreateRequests(tooLateTask.TaskID)
	require.Lenf(t, tooLateTaskCreateReqs, 0, "Create called unexpected number of times for %q", tooLateTask.TaskID)
	tooLateTaskExecReqs := ts.PopExecRequests(tooLateTask.TaskID)
	require.Lenf(t, tooLateTaskExecReqs, 0, "Exec called unexpected number of times for %q", tooLateTask.TaskID)
	tooLateTaskDeleteReqs := ts.PopDeleteRequests(tooLateTask.TaskID)
	require.Lenf(t, tooLateTaskDeleteReqs, 0, "Delete called unexpected number of times for %q", tooLateTask.TaskID)
	tooLateTaskWaitReqs := ts.PopWaitRequests(tooLateTask.TaskID)
	require.Lenf(t, tooLateTaskWaitReqs, 0, "Wait called unexpected number of times for %q", tooLateTask.TaskID)
}

// verifies that if io connection initialization fails, then CreateTask fails too
func TestTaskManager_IOCreateFails(t *testing.T) {
	shimCtx, shimCancel := context.WithCancel(context.Background())
	defer shimCancel()

	logger, logHook := test.NewNullLogger()
	logger.SetLevel(logrus.DebugLevel)
	defer func() {
		for _, entry := range logHook.AllEntries() {
			logLine, _ := entry.String()
			t.Log(logLine)
		}
	}()

	tm := NewTaskManager(shimCtx, logger.WithField("test", t.Name()))
	ts := &mockTaskService{}

	// Try to create a task where the io initialization fails, make sure CreateTask also fails
	mockTask := newMockProc("fakeTask", "", mockIOFailConnector, mockIOConnector, mockIOConnector)
	ts.SetWaitCh(mockTask.TaskID, mockTask.ExecID, mockTask.WaitCh)

	createReqCtx, createReqCancel := context.WithCancel(shimCtx)
	_, err := tm.CreateTask(createReqCtx, &taskAPI.CreateTaskRequest{ID: mockTask.TaskID}, ts, mockTask.IOConnectorSet)
	require.Error(t, err, "create task unexpectedly succeeded")
	createReqCancel()

	// Verify task had all APIs called as expected, specifically that Create was still called once
	mockTaskCreateReqs := ts.PopCreateRequests(mockTask.TaskID)
	require.Lenf(t, mockTaskCreateReqs, 1, "Create called unexpected number of times for %q", mockTask.TaskID)
	mockTaskDeleteReqs := ts.PopDeleteRequests(mockTask.TaskID)
	require.Lenf(t, mockTaskDeleteReqs, 0, "Delete called unexpected number of times for %q", mockTask.TaskID)
	mockTaskWaitReqs := ts.PopWaitRequests(mockTask.TaskID)
	require.Lenf(t, mockTaskWaitReqs, 0, "Wait called unexpected number of times for %q", mockTask.TaskID)
}
