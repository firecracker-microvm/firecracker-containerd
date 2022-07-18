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
	"testing"

	"github.com/containerd/containerd"
	"github.com/containerd/containerd/namespaces"
	"github.com/containerd/containerd/oci"
	"github.com/firecracker-microvm/firecracker-containerd/internal/integtest"
	"github.com/firecracker-microvm/firecracker-containerd/proto"
	"github.com/firecracker-microvm/firecracker-containerd/runtime/firecrackeroci"
	"github.com/firecracker-microvm/firecracker-containerd/snapshotter/demux"
	"github.com/firecracker-microvm/firecracker-containerd/snapshotter/internal/integtest/stargz/fs/source"
	"github.com/firecracker-microvm/firecracker-containerd/volume"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const mib = 1024 * 1024

func TestGuestVolumeFrom_Isolated(t *testing.T) {
	integtest.Prepare(t, integtest.WithDefaultNetwork())
	const (
		vmID     = "default"
		postgres = "docker.io/library/postgres:14.3"
		alpine   = "docker.io/library/alpine:3.10.1"
		runtime  = "aws.firecracker"
	)

	ctx := namespaces.WithNamespace(context.Background(), vmID)

	client, err := containerd.New(integtest.ContainerdSockPath, containerd.WithDefaultRuntime(runtime))
	require.NoError(t, err, "unable to create client to containerd service at %s, is containerd running?", integtest.ContainerdSockPath)
	defer client.Close()
	fcClient, err := integtest.NewFCControlClient(integtest.ContainerdSockPath)
	require.NoError(t, err, "Failed to create fccontrol client")

	vs := volume.NewSet(runtime)

	// Add a non-stargz image with volumes.
	localImage := volume.FromImage(client, postgres, "postgres-snapshot", volume.WithSnapshotter("devmapper"))
	err = vs.AddFrom(ctx, localImage)
	require.NoError(t, err)

	// PrepareDriveMount only copies images that are available before starting the VM.
	// In this case, only postgres.
	mount, err := vs.PrepareDriveMount(ctx, 10*mib)
	require.NoError(t, err)

	vminfo, err := fcClient.CreateVM(ctx, &proto.CreateVMRequest{
		VMID: vmID,
		RootDrive: &proto.FirecrackerRootDrive{
			HostPath: "/var/lib/firecracker-containerd/runtime/rootfs-stargz.img",
		},
		NetworkInterfaces: []*proto.FirecrackerNetworkInterface{
			{
				AllowMMDS: true,
				CNIConfig: &proto.CNIConfiguration{
					NetworkName:   "fcnet",
					InterfaceName: "veth0",
				},
			},
		},
		MachineCfg: &proto.FirecrackerMachineConfiguration{
			VcpuCount:  2,
			MemSizeMib: 2048,
		},
		ContainerCount: 1,
		DriveMounts:    []*proto.FirecrackerDriveMount{mount},
	})
	require.NoErrorf(t, err, "Failed to create microVM[%s]", vmID)
	defer fcClient.StopVM(ctx, &proto.StopVMRequest{VMID: vmID})

	// Add a stargz image.
	// The volume directories must be specified since the host's containerd doesn't know about the image.
	remoteImage := volume.FromGuestImage(
		client, vmID, al2stargz, "al2-snapshot", []string{"/etc/yum"},
		volume.WithSnapshotter("demux"),
		volume.WithPullOptions(containerd.WithImageHandlerWrapper(
			source.AppendDefaultLabelsHandlerWrapper(al2stargz, 10*mib),
		)),
		volume.WithSnapshotOptions(
			demux.WithVSockPath(vminfo.VSockPath),
			demux.WithRemoteSnapshotterPort(10000),
		),
	)
	err = vs.AddFrom(ctx, remoteImage)
	require.NoError(t, err)

	_, err = fcClient.SetVMMetadata(ctx, &proto.SetVMMetadataRequest{
		VMID:     vmID,
		Metadata: fmt.Sprintf(dockerMetadataTemplate, "ghcr.io", noAuth, noAuth),
	})
	require.NoError(t, err, "Failed to configure VM metadata for registry resolution")

	// PrepareGuestVolumes only copies images that are only available after starting the VM.
	// In this case, only al2stargz.
	err = vs.PrepareInGuest(ctx, "prepare-in-guest")
	require.NoError(t, err)

	image, err := client.Pull(ctx,
		alpine,
		containerd.WithPullUnpack,
		containerd.WithPullSnapshotter("devmapper"),
	)
	require.NoError(t, err)

	mountsFromAL2, err := vs.WithMountsFromProvider(al2stargz)
	require.NoError(t, err)

	mountsFromPostgres, err := vs.WithMountsFromProvider(postgres)
	require.NoError(t, err)

	name := "cat"
	snapshotName := fmt.Sprintf("%s-snapshot", name)
	container, err := client.NewContainer(ctx,
		name,
		containerd.WithSnapshotter("devmapper"),
		containerd.WithNewSnapshot(snapshotName, image),
		containerd.WithNewSpec(
			firecrackeroci.WithVMID(vmID),
			oci.WithProcessArgs("sh", "-c", "ls -d /var/lib/postgresql/data; ls /etc/yum"),
			oci.WithDefaultPathEnv,
			mountsFromAL2,
			mountsFromPostgres,
		),
	)
	require.NoError(t, err, "failed to create container %s", name)
	defer container.Delete(ctx, containerd.WithSnapshotCleanup)

	result, err := integtest.RunTask(ctx, container)
	require.NoError(t, err)

	assert.Equal(t, uint32(0), result.ExitCode)
	assert.Equal(t, "/var/lib/postgresql/data\nfssnap.d\npluginconf.d\nprotected.d\nvars\nversion-groups.conf\n", result.Stdout)
	assert.Equal(t, "", result.Stderr)
}
