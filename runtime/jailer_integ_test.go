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
	"os"
	"path/filepath"
	"testing"

	"github.com/containerd/containerd"
	"github.com/containerd/containerd/namespaces"
	"github.com/containerd/containerd/oci"
	"github.com/containerd/containerd/pkg/ttrpcutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	_ "github.com/firecracker-microvm/firecracker-containerd/firecracker-control"
	"github.com/firecracker-microvm/firecracker-containerd/proto"
	fccontrol "github.com/firecracker-microvm/firecracker-containerd/proto/service/fccontrol/ttrpc"
	"github.com/firecracker-microvm/firecracker-containerd/runtime/cpuset"
	"github.com/firecracker-microvm/firecracker-containerd/runtime/firecrackeroci"
)

func TestJailer_Isolated(t *testing.T) {
	prepareIntegTest(t)
	t.Run("Without Jailer", func(t *testing.T) {
		testJailer(t, nil)
	})
	t.Run("With Jailer", func(t *testing.T) {
		testJailer(t, &proto.JailerConfig{
			UID: 300001,
			GID: 300001,
		})
	})
}

func testJailer(t *testing.T, jailerConfig *proto.JailerConfig) {
	require := require.New(t)

	client, err := containerd.New(containerdSockPath, containerd.WithDefaultRuntime(firecrackerRuntime))
	require.NoError(err, "unable to create client to containerd service at %s, is containerd running?", containerdSockPath)
	defer client.Close()

	ctx := namespaces.WithNamespace(context.Background(), "default")

	image, err := alpineImage(ctx, client, defaultSnapshotterName)
	require.NoError(err, "failed to get alpine image")

	pluginClient, err := ttrpcutil.NewClient(containerdSockPath + ".ttrpc")
	require.NoError(err, "failed to create ttrpc client")

	vmID := testNameToVMID(t.Name())

	fcClient := fccontrol.NewFirecrackerClient(pluginClient.Client())
	_, err = fcClient.CreateVM(ctx, &proto.CreateVMRequest{
		VMID:         vmID,
		JailerConfig: jailerConfig,
	})
	require.NoError(err)

	c, err := client.NewContainer(ctx,
		"container",
		containerd.WithSnapshotter(defaultSnapshotterName),
		containerd.WithNewSnapshot("snapshot", image),
		containerd.WithNewSpec(oci.WithProcessArgs("/bin/echo", "-n", "hello"), firecrackeroci.WithVMID(vmID)),
	)
	require.NoError(err)

	stdout := startAndWaitTask(ctx, t, c)
	require.Equal("hello", stdout)

	stat, err := os.Stat(filepath.Join(shimBaseDir(), "default", vmID))
	require.NoError(err)
	assert.True(t, stat.IsDir())

	err = c.Delete(ctx, containerd.WithSnapshotCleanup)
	require.NoError(err, "failed to delete a container")

	_, err = fcClient.StopVM(ctx, &proto.StopVMRequest{VMID: vmID})
	require.NoError(err)

	_, err = os.Stat(filepath.Join(shimBaseDir(), "default", vmID))
	assert.Error(t, err)
	assert.True(t, os.IsNotExist(err))
}

func TestJailerCPUSet_Isolated(t *testing.T) {
	prepareIntegTest(t)

	b := cpuset.Builder{}
	cset := b.AddCPU(0).AddMem(0).Build()
	config := &proto.JailerConfig{
		CPUs: cset.CPUs(),
		Mems: cset.Mems(),
		UID:  300000,
		GID:  300000,
	}
	testJailer(t, config)
}
