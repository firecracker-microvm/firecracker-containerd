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
	"fmt"
	"io"
	"net"
	"os"
	"testing"
	"time"

	"github.com/containerd/containerd/cio"
	"github.com/containerd/containerd/runtime/v2/task"
	"github.com/firecracker-microvm/firecracker-containerd/internal/bundle"
	"github.com/firecracker-microvm/firecracker-containerd/proto"
	"github.com/stretchr/testify/require"
)

var (
	mockBundleDir = bundle.Dir("/tmp")

	mockExtraData = &proto.ExtraData{
		JsonSpec:    []byte{},
		RuncOptions: nil,
		StdinPort:   1,
		StdoutPort:  2,
		StderrPort:  3,
	}

	mockFIFOSet = cio.NewFIFOSet(cio.Config{
		Stdin:  "/dev/null",
		Stdout: "/dev/null",
		Stderr: "/dev/null",
	}, nil)
)

type mockTaskService struct {
	task.TaskService
}

func defaultMockTaskArgs(id string) addTaskArgs {
	return addTaskArgs{
		id:        fmt.Sprintf("container-%s", id),
		ts:        &mockTaskService{},
		bundleDir: mockBundleDir,
		extraData: mockExtraData,
		fifoSet:   mockFIFOSet,
	}
}

type addTaskArgs struct {
	id        string
	ts        task.TaskService
	bundleDir bundle.Dir
	extraData *proto.ExtraData
	fifoSet   *cio.FIFOSet
}

func addTaskFromArgs(tm TaskManager, args addTaskArgs) (*Task, error) {
	return tm.AddTask(context.TODO(), args.id, args.ts, args.bundleDir, args.extraData, args.fifoSet)
}

func TestTaskManager_AddRemoveTask(t *testing.T) {
	tm := NewTaskManager()

	taskAArgs := defaultMockTaskArgs("A")
	taskBArgs := defaultMockTaskArgs("B")
	taskCArgs := defaultMockTaskArgs("C")

	assertExpectedTask := func(args addTaskArgs, createdTask *Task) {
		require.Equal(t, args.ts, createdTask.TaskService, "AddTask sets expected task service")
		require.Equal(t, args.id, createdTask.ID, "AddTask sets expected container ID")
		require.Equal(t, args.extraData, createdTask.extraData, "AddTask sets expected container ID")
		require.Equal(t, args.bundleDir, createdTask.bundleDir, "AddTask sets expected container ID")
		require.Equal(t, args.fifoSet, createdTask.fifoSet, "AddTask sets expected container ID")
	}

	// Basic adding of tasks
	taskA, err := addTaskFromArgs(tm, taskAArgs)
	require.NoError(t, err, "add first task")
	assertExpectedTask(taskAArgs, taskA)

	taskB, err := addTaskFromArgs(tm, taskBArgs)
	require.NoError(t, err, "add second task")
	assertExpectedTask(taskBArgs, taskB)

	taskC, err := addTaskFromArgs(tm, taskCArgs)
	require.NoError(t, err, "add third task")
	assertExpectedTask(taskCArgs, taskC)

	_, err = addTaskFromArgs(tm, taskAArgs)
	require.Error(t, err, "add duplicate task should fail")

	require.EqualValues(t, 3, tm.TaskCount(), "task count should return correct number of tasks")

	// We should be able to get return the tasks we added
	supposedlyTaskA, err := tm.Task(taskAArgs.id)
	require.NoError(t, err, "get task")
	require.Exactly(t, taskA, supposedlyTaskA, "get should return expected task")

	supposedlyTaskB, err := tm.Task(taskBArgs.id)
	require.NoError(t, err, "get task")
	require.Exactly(t, taskB, supposedlyTaskB, "get should return expected task")

	supposedlyTaskC, err := tm.Task(taskCArgs.id)
	require.NoError(t, err, "get task")
	require.Exactly(t, taskC, supposedlyTaskC, "get should return expected task")

	_, err = tm.Task("never-existed")
	require.Error(t, err, "get non-existent task should fail")

	// Removing a task should remove the task requested but have no effect on the others
	tm.Remove(context.TODO(), taskBArgs.id)
	require.EqualValues(t, 2, tm.TaskCount(), "task count should return correct number of tasks after remove")

	supposedlyTaskA, err = tm.Task(taskAArgs.id)
	require.NoError(t, err, "get task")
	require.Exactly(t, taskA, supposedlyTaskA, "get should return expected task")

	_, err = tm.Task(taskBArgs.id)
	require.Error(t, err, "get removed task should fail")

	supposedlyTaskC, err = tm.Task(taskCArgs.id)
	require.NoError(t, err, "get task")
	require.Exactly(t, taskC, supposedlyTaskC, "get should return expected task")

	// Remove all should remove all the tasks
	tm.RemoveAll(context.TODO())
	require.EqualValues(t, 0, tm.TaskCount(), "task count should return correct number of tasks after remove all")

	_, err = tm.Task(taskAArgs.id)
	require.Error(t, err, "get removed task should fail")

	_, err = tm.Task(taskCArgs.id)
	require.Error(t, err, "get removed task should fail")

	tm.RemoveAll(context.TODO())
	require.EqualValues(t, 0, tm.TaskCount(), "remove all on empty task manager should have no effect")
	tm.Remove(context.TODO(), taskAArgs.id)
	require.EqualValues(t, 0, tm.TaskCount(), "remove on non-existent task should have no effect")

	// Tasks should still be able to be added after removes
	taskC, err = addTaskFromArgs(tm, taskCArgs)
	require.NoError(t, err, "re-add task after deletion")
	assertExpectedTask(taskCArgs, taskC)

	supposedlyTaskC, err = tm.Task(taskCArgs.id)
	require.NoError(t, err, "get re-added task")
	require.Exactly(t, taskC, supposedlyTaskC, "get should return expected task")

	require.EqualValues(t, 1, tm.TaskCount(), "re-add task should set task count")
}

func pathOfFile(f *os.File) string {
	return fmt.Sprintf("/proc/self/fd/%d", f.Fd())
}

func TestTaskManager_StartStdioProxy_FIFOtoVSock(t *testing.T) {
	testStdioProxy(t, FIFOtoVSock)
}

func TestTaskManager_StartStdioProxy_VSockToFIFO(t *testing.T) {
	testStdioProxy(t, VSockToFIFO)
}

func testStdioProxy(t *testing.T, inputDirection IODirection) {
	tm := NewTaskManager()

	taskArgs := defaultMockTaskArgs("A")

	// Use pipes to serve as fake FIFOs. We provide the io proxy
	// paths to their /proc/self/fd
	stdinR, stdinW, err := os.Pipe()
	require.NoError(t, err, "get os pipe FDs")

	stdoutR, stdoutW, err := os.Pipe()
	require.NoError(t, err, "get os pipe FDs")

	stderrR, stderrW, err := os.Pipe()
	require.NoError(t, err, "get os pipe FDs")

	switch inputDirection {
	case FIFOtoVSock:
		taskArgs.fifoSet = cio.NewFIFOSet(cio.Config{
			Stdin:  pathOfFile(stdinR),
			Stdout: pathOfFile(stdoutW),
			Stderr: pathOfFile(stderrW),
		}, nil)

	case VSockToFIFO:
		taskArgs.fifoSet = cio.NewFIFOSet(cio.Config{
			Stdin:  pathOfFile(stdinW),
			Stdout: pathOfFile(stdoutR),
			Stderr: pathOfFile(stderrR),
		}, nil)
	}

	// Use net.Conn as fake vsocks. *Local represents the side the io proxy
	// will interact with. We will fake remote data by reading/writing from/to
	// the *Remote conns in the test case code itself.
	stdinSockLocal, stdinSockRemote := net.Pipe()
	stdoutSockLocal, stdoutSockRemote := net.Pipe()
	stderrSockLocal, stderrSockRemote := net.Pipe()

	vsockConnector := func(ctx context.Context, port uint32) (net.Conn, error) {
		switch port {
		case taskArgs.extraData.StdinPort:
			return stdinSockLocal, nil
		case taskArgs.extraData.StdoutPort:
			return stdoutSockLocal, nil
		case taskArgs.extraData.StderrPort:
			return stderrSockLocal, nil
		default:
			require.Failf(t, "unexpected vsock port", "port: %d", port)
			return nil, nil // just here to appease the compiler, Failf exits
		}
	}

	// add the task and start the io proxy
	task, err := addTaskFromArgs(tm, taskArgs)
	require.NoError(t, err, "add first task")

	readyCh := task.StartStdioProxy(context.TODO(), inputDirection, vsockConnector)
	select {
	case err := <-readyCh:
		require.NoError(t, err, "initialize stdio")
	case <-time.After(10 * time.Second):
		require.Fail(t, "timed out waiting for stdio initialization to finish")
	}

	// stdin
	var stdinWriter io.WriteCloser
	var stdinReader io.ReadCloser
	switch inputDirection {
	case FIFOtoVSock:
		stdinWriter = stdinW
		stdinReader = stdinSockRemote
	case VSockToFIFO:
		stdinWriter = stdinSockRemote
		stdinReader = stdinR
	}

	writtenStdinBytes := []byte("stdin")
	readStdinBytes := make([]byte, len(writtenStdinBytes))

	_, err = stdinWriter.Write(writtenStdinBytes)
	require.NoError(t, err, "write to stdin")

	err = stdinWriter.Close()
	require.NoError(t, err, "close stdin socket")

	_, err = stdinReader.Read(readStdinBytes)
	require.NoError(t, err, "read stdout")
	require.Equal(t, writtenStdinBytes, readStdinBytes, "stdin proxied transparently")

	// stdout
	var stdoutWriter io.WriteCloser
	var stdoutReader io.ReadCloser
	switch inputDirection {
	case FIFOtoVSock:
		stdoutWriter = stdoutSockRemote
		stdoutReader = stdoutR
	case VSockToFIFO:
		stdoutWriter = stdoutW
		stdoutReader = stdoutSockRemote
	}

	writtenStdoutBytes := []byte("stdout")
	readStdoutBytes := make([]byte, len(writtenStdoutBytes))

	stdoutWriter.Write(writtenStdoutBytes)
	require.NoError(t, err, "write stdout")

	err = stdoutWriter.Close()
	require.NoError(t, err, "close stdout socket")

	_, err = stdoutReader.Read(readStdoutBytes)
	require.NoError(t, err, "read stdout")
	require.Equal(t, writtenStdoutBytes, readStdoutBytes, "stdout proxied transparently")

	// stderr
	var stderrWriter io.WriteCloser
	var stderrReader io.ReadCloser
	switch inputDirection {
	case FIFOtoVSock:
		stderrWriter = stderrSockRemote
		stderrReader = stderrR
	case VSockToFIFO:
		stderrWriter = stderrW
		stderrReader = stderrSockRemote
	}

	writtenStderrBytes := []byte("stderr")
	readStderrBytes := make([]byte, len(writtenStderrBytes))

	stderrWriter.Write(writtenStderrBytes)
	require.NoError(t, err, "write stderr")

	err = stderrWriter.Close()
	require.NoError(t, err, "close stderr socket")

	_, err = stderrReader.Read(readStderrBytes)
	require.NoError(t, err, "read stderr")
	require.Equal(t, writtenStderrBytes, readStderrBytes, "stderr proxied transparently")
}
