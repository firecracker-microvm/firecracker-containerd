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
	"fmt"
	"strconv"
	"testing"

	"github.com/containerd/containerd"
	"github.com/containerd/containerd/namespaces"
	"github.com/containerd/containerd/oci"
	"github.com/firecracker-microvm/firecracker-containerd/proto"
	"github.com/firecracker-microvm/firecracker-containerd/runtime/firecrackeroci"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRemoteSnapshotter_Isolated(t *testing.T) {
	prepareIntegTest(t)

	ssName := "demux"
	vmID := 0
	ns := strconv.Itoa(vmID)

	ctx := namespaces.WithNamespace(context.Background(), ns)

	client, err := containerd.New(containerdSockPath, containerd.WithDefaultRuntime(firecrackerRuntime))
	require.NoError(t, err, "unable to create client to containerd service at %s, is containerd running?", containerdSockPath)
	defer client.Close()

	fcClient, err := newFCControlClient(containerdSockPath)
	require.NoError(t, err, "failed to create fccontrol client")

	tapName := fmt.Sprintf("tap%d", vmID)
	err = createTapDevice(ctx, tapName)
	require.NoError(t, err)

	_, err = fcClient.CreateVM(ctx, &proto.CreateVMRequest{
		RootDrive: &proto.FirecrackerRootDrive{
			HostPath: "/var/lib/firecracker-containerd/runtime/rootfs-stargz.img",
		},
		VMID: strconv.Itoa(vmID),
		NetworkInterfaces: []*proto.FirecrackerNetworkInterface{
			{
				AllowMMDS: true,
				StaticConfig: &proto.StaticNetworkConfiguration{
					HostDevName: tapName,
					MacAddress:  vmIDtoMacAddr(uint(vmID)),
				},
			},
		},
		ContainerCount: 1,
	})
	require.NoError(t, err)

	image, err := client.Pull(ctx, "docker.io/library/alpine:3.10.1", containerd.WithPullUnpack)
	require.NoError(t, err, "failed to get alpine image")

	// Right now, both naive snapshotter and devmapper snapshotter are configured to have 1024MB image size.
	// The former is hard-coded since the snapshotter is not for production. The latter is configured in tools/docker/entrypoint.sh.
	sh := containerd.WithNewSpec(
		oci.WithProcessArgs("echo", "hello"),
		oci.WithDefaultPathEnv,
		firecrackeroci.WithVMID(strconv.Itoa(vmID)),
		firecrackeroci.WithVMNetwork,
	)

	container, err := client.NewContainer(ctx,
		"container",
		containerd.WithSnapshotter(ssName),
		containerd.WithNewSnapshot("snapshot", image),
		sh,
	)
	require.NoError(t, err)
	defer container.Delete(ctx, containerd.WithSnapshotCleanup)

	result, err := runTask(ctx, container)
	require.NoError(t, err, "failed to create a container")

	assert.Equal(t, uint32(1), result.exitCode, "writing 2GB must fail")
	assert.Equal(t, `952+0 records in
951+0 records out
`, result.stderr, "but it must be able to write ~1024MB")
}
