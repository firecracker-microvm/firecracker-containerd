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

package main

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"testing"

	"github.com/containerd/containerd"
	"github.com/containerd/containerd/cio"
	"github.com/containerd/containerd/namespaces"
	"github.com/containerd/containerd/oci"
	"github.com/firecracker-microvm/firecracker-containerd/proto"
	"github.com/firecracker-microvm/firecracker-containerd/runtime/firecrackeroci"
	"github.com/firecracker-microvm/firecracker-containerd/volume"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const mib = 1024 * 1024

func TestVolumes_Isolated(t *testing.T) {
	prepareIntegTest(t)

	const vmID = 0

	ctx := namespaces.WithNamespace(context.Background(), "default")

	client, err := containerd.New(containerdSockPath, containerd.WithDefaultRuntime(firecrackerRuntime))
	require.NoError(t, err, "unable to create client to containerd service at %s, is containerd running?", containerdSockPath)
	defer client.Close()

	image, err := alpineImage(ctx, client, defaultSnapshotterName)
	require.NoError(t, err, "failed to get alpine image")

	fcClient, err := newFCControlClient(containerdSockPath)
	require.NoError(t, err, "failed to create fccontrol client")

	// Make volumes.
	path, err := os.MkdirTemp("", t.Name())
	require.NoError(t, err)

	f, err := os.Create(filepath.Join(path, "hello.txt"))
	require.NoError(t, err)

	_, err = f.Write([]byte("hello from host\n"))
	require.NoError(t, err)

	const volName = "volume1"
	vs := volume.NewSet()
	vs.Add(volume.FromHost(volName, path))

	// Since CreateVM doesn't take functional options, we need to explicitly create
	// a FirecrackerDriveMount
	mount, err := vs.PrepareDriveMount(ctx, 10*mib)
	require.NoError(t, err)

	containers := []string{"c1", "c2"}

	_, err = fcClient.CreateVM(ctx, &proto.CreateVMRequest{
		VMID:           strconv.Itoa(vmID),
		ContainerCount: int32(len(containers)),
		DriveMounts:    []*proto.FirecrackerDriveMount{mount},
	})
	require.NoError(t, err, "failed to create VM")

	// Make containers with the volume.
	dir := "/path/in/container"
	mpOpt, err := vs.WithMounts([]volume.Mount{{Source: volName, Destination: dir, ReadOnly: false}})
	require.NoError(t, err)

	for _, name := range containers {
		snapshotName := fmt.Sprintf("%s-snapshot", name)

		sh := fmt.Sprintf("echo hello from %s >> %s/hello.txt", name, dir)
		container, err := client.NewContainer(ctx,
			name,
			containerd.WithSnapshotter(defaultSnapshotterName),
			containerd.WithNewSnapshot(snapshotName, image),
			containerd.WithNewSpec(
				firecrackeroci.WithVMID(strconv.Itoa(vmID)),
				oci.WithProcessArgs("sh", "-c", sh),
				oci.WithDefaultPathEnv,
				mpOpt,
			),
		)
		require.NoError(t, err, "failed to create container %s", name)

		var stdout, stderr bytes.Buffer

		task, err := container.NewTask(ctx, cio.NewCreator(cio.WithStreams(nil, &stdout, &stderr)))
		require.NoError(t, err, "failed to create task for container %s", name)

		exitCh, err := task.Wait(ctx)
		require.NoError(t, err, "failed to wait on task for container %s", name)

		err = task.Start(ctx)
		require.NoError(t, err, "failed to start task for container %s", name)

		exit := <-exitCh
		_, err = task.Delete(ctx)
		require.NoError(t, err)

		assert.Equalf(t, uint32(0), exit.ExitCode(), "stdout=%q stderr=%q", stdout.String(), stderr.String())
	}

	name := "cat"
	snapshotName := fmt.Sprintf("%s-snapshot", name)
	container, err := client.NewContainer(ctx,
		name,
		containerd.WithSnapshotter(defaultSnapshotterName),
		containerd.WithNewSnapshot(snapshotName, image),
		containerd.WithNewSpec(
			firecrackeroci.WithVMID(strconv.Itoa(vmID)),
			oci.WithProcessArgs("cat", fmt.Sprintf("%s/hello.txt", dir)),
			oci.WithDefaultPathEnv,
			mpOpt,
		),
	)
	require.NoError(t, err, "failed to create container %s", name)

	var stdout, stderr bytes.Buffer
	task, err := container.NewTask(ctx, cio.NewCreator(cio.WithStreams(nil, &stdout, &stderr)))
	require.NoError(t, err, "failed to create task for container %s", name)

	exitCh, err := task.Wait(ctx)
	require.NoError(t, err, "failed to wait on task for container %s", name)

	err = task.Start(ctx)
	require.NoError(t, err, "failed to start task for container %s", name)

	exit := <-exitCh
	_, err = task.Delete(ctx)
	require.NoError(t, err)

	assert.Equal(t, uint32(0), exit.ExitCode())
	assert.Equal(t, "hello from host\nhello from c1\nhello from c2\n", stdout.String())
	assert.Equal(t, "", stderr.String())
}
