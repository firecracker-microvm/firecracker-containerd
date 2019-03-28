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
	"fmt"
	"path/filepath"
	"syscall"
	"testing"
	"time"

	"github.com/containerd/containerd"
	"github.com/containerd/containerd/api/events"
	"github.com/containerd/containerd/cio"
	"github.com/containerd/containerd/namespaces"
	"github.com/containerd/containerd/oci"
	"github.com/containerd/containerd/runtime"
	"github.com/containerd/typeurl"
	"github.com/shirou/gopsutil/process"
	"github.com/stretchr/testify/require"

	"github.com/firecracker-microvm/firecracker-containerd/internal"
)

const (
	containerdSockPath = "/run/containerd/containerd.sock"
	debianDockerImage  = "docker.io/library/debian:latest"

	firecrackerRuntime   = "aws.firecracker"
	naiveSnapshotterName = "firecracker-naive"
	shimProcessName      = "containerd-shim-aws-firecracker"
)

func TestShimExitsUponContainerKill_Isolated(t *testing.T) {
	internal.RequiresIsolation(t)

	ctx := namespaces.WithNamespace(context.Background(), "default")

	client, err := containerd.New(containerdSockPath)
	require.NoError(t, err, "unable to create client to containerd service at %s, is containerd running?", containerdSockPath)
	defer client.Close()

	pullTimeout := 180 * time.Second
	pullCtx, pullCancel := context.WithTimeout(ctx, pullTimeout)
	defer pullCancel()

	image, err := client.Pull(pullCtx, debianDockerImage, containerd.WithPullUnpack, containerd.WithPullSnapshotter(naiveSnapshotterName))
	require.NoError(t, err, "failed to pull image %s, is the the %s snapshotter running?", debianDockerImage, naiveSnapshotterName)

	testTimeout := 60 * time.Second
	testCtx, testCancel := context.WithTimeout(ctx, testTimeout)
	defer testCancel()

	containerName := fmt.Sprintf("%s-%d", t.Name(), time.Now().UnixNano())
	snapshotName := fmt.Sprintf("%s-snapshot", containerName)
	container, err := client.NewContainer(testCtx,
		containerName,
		containerd.WithRuntime(firecrackerRuntime, nil),
		containerd.WithSnapshotter(naiveSnapshotterName),
		containerd.WithNewSnapshot(snapshotName, image),
		containerd.WithNewSpec(
			oci.WithProcessArgs("sleep", fmt.Sprintf("%d", testTimeout/time.Second)),
		),
	)
	require.NoError(t, err, "failed to create container %s", containerName)

	task, err := container.NewTask(testCtx, cio.NewCreator(cio.WithStdio))
	require.NoError(t, err, "failed to create task for container %s", containerName)

	exitEventCh, exitEventErrCh := client.Subscribe(testCtx, fmt.Sprintf(`topic=="%s"`, runtime.TaskExitEventTopic))

	err = task.Start(testCtx)
	require.NoError(t, err, "failed to start task for container %s", containerName)

	shimProcesses, err := internal.WaitForProcessToExist(testCtx, time.Second,
		func(ctx context.Context, p *process.Process) (bool, error) {
			processExecutable, err := p.ExeWithContext(ctx)
			if err != nil {
				return false, err
			}

			return filepath.Base(processExecutable) == shimProcessName, nil
		},
	)
	require.NoError(t, err, "failed waiting for expected shim process \"%s\" to come up", shimProcessName)
	require.Len(t, shimProcesses, 1, "expected only one shim process to exist")
	shimProcess := shimProcesses[0]

	err = task.Kill(testCtx, syscall.SIGKILL)
	require.NoError(t, err, "failed to SIGKILL containerd task %s", containerName)

	select {
	case envelope := <-exitEventCh:
		unmarshaledEvent, err := typeurl.UnmarshalAny(envelope.Event)
		require.NoError(t, err, "failed to unmarshal event")

		switch event := unmarshaledEvent.(type) {
		case *events.TaskExit:
			require.Equal(t, container.ID(), event.ContainerID, "received exit event from expected container %s", container.ID())
		default:
			require.Fail(t, "unexpected event type", "received unexpected non-exit event type on topic: %s", envelope.Topic)
		}

		err = internal.WaitForPidToExit(testCtx, time.Second, shimProcess.Pid)
		require.NoError(t, err, "failed waiting for shim process \"%s\" to exit", shimProcessName)
	case err = <-exitEventErrCh:
		require.Fail(t, "unexpected error", "unexpectedly received on task exit error channel: %s", err.Error())
	case <-testCtx.Done():
		require.Fail(t, "context canceled", "context canceled while waiting for container \"%s\" exit: %s", containerName, testCtx.Err())
	}
}
