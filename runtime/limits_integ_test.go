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
	"context"
	"testing"

	"github.com/containerd/containerd"
	"github.com/containerd/containerd/namespaces"
	"github.com/containerd/containerd/oci"
	"github.com/firecracker-microvm/firecracker-containerd/internal/integtest"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDiskLimit_Isolated(t *testing.T) {
	integtest.Prepare(t)

	ctx := namespaces.WithNamespace(context.Background(), "default")

	client, err := containerd.New(integtest.ContainerdSockPath, containerd.WithDefaultRuntime(firecrackerRuntime))
	require.NoError(t, err, "unable to create client to containerd service at %s, is containerd running?", integtest.ContainerdSockPath)
	defer client.Close()

	image, err := alpineImage(ctx, client, defaultSnapshotterName)
	require.NoError(t, err, "failed to get alpine image")

	// Right now, both naive snapshotter and devmapper snapshotter are configured to have 1024MB image size.
	// The former is hard-coded since the snapshotter is not for production. The latter is configured in tools/docker/entrypoint.sh.
	sh := containerd.WithNewSpec(
		oci.WithProcessArgs("dd", "if=/dev/zero", "of=/tmp/fill", "bs=1M", "count=2000"),
		oci.WithDefaultPathEnv,
	)

	container, err := client.NewContainer(ctx,
		"container",
		containerd.WithSnapshotter(defaultSnapshotterName),
		containerd.WithNewSnapshot("snapshot", image),
		sh,
	)
	defer func() {
		err = container.Delete(ctx, containerd.WithSnapshotCleanup)
		require.NoError(t, err, "failed to delete a container")
	}()

	result, err := integtest.RunTask(ctx, container)
	require.NoError(t, err, "failed to create a container")

	assert.Equal(t, uint32(1), result.ExitCode, "writing 2GB must fail")
	assert.Equal(t, `952+0 records in
951+0 records out
`, result.Stderr, "but it must be able to write ~1024MB")
}
