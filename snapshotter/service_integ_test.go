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
	"sync"
	"testing"
	"time"

	"github.com/containerd/containerd"
	"github.com/containerd/containerd/namespaces"
	"github.com/containerd/containerd/oci"
	"github.com/firecracker-microvm/firecracker-containerd/internal/integtest"
	"github.com/firecracker-microvm/firecracker-containerd/proto"
	"github.com/firecracker-microvm/firecracker-containerd/runtime/firecrackeroci"
	"github.com/firecracker-microvm/firecracker-containerd/snapshotter/internal/integtest/stargz/fs/source"
	"github.com/stretchr/testify/require"
)

const (
	snapshotterName = "demux"

	imageRef = "ghcr.io/firecracker-microvm/firecracker-containerd/amazonlinux:latest-esgz"

	noAuth = ""

	dockerMetadataTemplate = `
        {
            "docker-credentials": {
                "%s": {
                    "username": "%s",
                    "password": "%s"
                }
            }
        }`
)

func TestLaunchContainerWithRemoteSnapshotter_Isolated(t *testing.T) {
	integtest.Prepare(t, integtest.WithDefaultNetwork())

	vmID := 0

	testTimeout := 300 * time.Second
	ctx, cancel := context.WithTimeout(context.Background(), testTimeout)
	defer cancel()

	launchContainerWithRemoteSnapshotterInVM(ctx, t, strconv.Itoa(vmID))
}

func TestLaunchMultipleContainersWithRemoteSnapshotter_Isolated(t *testing.T) {
	integtest.Prepare(t, integtest.WithDefaultNetwork())

	testTimeout := 600 * time.Second
	ctx, cancel := context.WithTimeout(context.Background(), testTimeout)
	defer cancel()

	var wg sync.WaitGroup

	numberOfVms := integtest.NumberOfVms
	for vmID := 0; vmID < numberOfVms; vmID++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			launchContainerWithRemoteSnapshotterInVM(ctx, t, strconv.Itoa(id))
		}(vmID)
	}
	wg.Wait()
}

func launchContainerWithRemoteSnapshotterInVM(ctx context.Context, t *testing.T, vmID string) {
	// For integration testing, assume the namespace is same as the VM ID.
	namespace := vmID

	ctx = namespaces.WithNamespace(ctx, namespace)

	client, err := containerd.New(integtest.ContainerdSockPath, containerd.WithDefaultRuntime(integtest.FirecrackerRuntime))
	require.NoError(t, err, "Unable to create client to containerd service at %s, is containerd running?", integtest.ContainerdSockPath)

	fcClient, err := integtest.NewFCControlClient(integtest.ContainerdSockPath)
	require.NoError(t, err, "Failed to create fccontrol client")

	_, err = fcClient.CreateVM(ctx, &proto.CreateVMRequest{
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
	})
	require.NoErrorf(t, err, "Failed to create microVM[%s]", vmID)
	defer fcClient.StopVM(ctx, &proto.StopVMRequest{VMID: vmID})

	_, err = fcClient.SetVMMetadata(ctx, &proto.SetVMMetadataRequest{
		VMID:     vmID,
		Metadata: fmt.Sprintf(dockerMetadataTemplate, "ghcr.io", noAuth, noAuth),
	})
	require.NoError(t, err, "Failed to configure VM metadata for registry resolution")

	image, err := client.Pull(ctx, imageRef,
		containerd.WithPullUnpack,
		containerd.WithPullSnapshotter(snapshotterName),
		containerd.WithImageHandlerWrapper(source.AppendDefaultLabelsHandlerWrapper(imageRef, 10*1024*1024)),
	)
	require.NoError(t, err, "Failed to pull image for VM: %s", vmID)
	defer client.ImageService().Delete(ctx, image.Name())

	container, err := client.NewContainer(ctx, fmt.Sprintf("container-%s", vmID),
		containerd.WithSnapshotter(snapshotterName),
		containerd.WithNewSnapshot("snapshot", image),
		containerd.WithNewSpec(
			oci.WithProcessArgs("echo", "hello"),
			oci.WithDefaultPathEnv,
			firecrackeroci.WithVMLocalImageConfig(image),
			firecrackeroci.WithVMID(vmID),
			firecrackeroci.WithVMNetwork,
		),
	)
	require.NoError(t, err, "Failed to create container in VM: %s", vmID)
	defer container.Delete(ctx, containerd.WithSnapshotCleanup)

	_, err = integtest.RunTask(ctx, container)
	require.NoError(t, err, "Failed to run task in VM: %s", vmID)
}
