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
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/containerd/containerd"
	"github.com/containerd/containerd/namespaces"
	"github.com/firecracker-microvm/firecracker-containerd/firecracker-control/client"
	"github.com/firecracker-microvm/firecracker-containerd/internal/integtest"
	"github.com/firecracker-microvm/firecracker-containerd/proto"
	"github.com/stretchr/testify/require"
)

const (
	snapshotterNameMetrics = "demux"

	serviceDiscoveryEndpoint = "http://localhost:8080"
)

func TestSnapshotterMetrics_Isolated(t *testing.T) {
	integtest.Prepare(t, integtest.WithDefaultNetwork())

	vmID := 0

	testTimeout := 30 * time.Second
	ctx, cancel := context.WithTimeout(context.Background(), testTimeout)
	defer cancel()

	ctx = namespaces.WithNamespace(ctx, strconv.Itoa(vmID))

	fcClient, err := integtest.NewFCControlClient(integtest.ContainerdSockPath)
	defer fcClient.StopVM(ctx, &proto.StopVMRequest{VMID: strconv.Itoa(vmID)})
	require.NoError(t, err, "Failed to create fccontrol client")

	pullImageWithRemoteSnapshotterInVM(ctx, t, strconv.Itoa(vmID), fcClient)
	verifyMetricsResponse(t, 1)
}

func TestSnapshotterMetricsMultipleVMs_Isolated(t *testing.T) {
	integtest.Prepare(t, integtest.WithDefaultNetwork())

	testTimeout := 300 * time.Second
	ctx, cancel := context.WithTimeout(context.Background(), testTimeout)
	defer cancel()

	var wg sync.WaitGroup

	numberOfVms := integtest.NumberOfVms
	fcClient, err := integtest.NewFCControlClient(integtest.ContainerdSockPath)
	require.NoError(t, err, "Failed to create fccontrol client")
	for vmID := 0; vmID < numberOfVms; vmID++ {
		// Stop VMs at the end of this function, not at launching container
		// For integration testing, assume the namespace is same as the VM ID.
		defer func(ctxInner context.Context, id int) {
			fcClient.StopVM(ctxInner, &proto.StopVMRequest{VMID: strconv.Itoa(id)})
		}(namespaces.WithNamespace(ctx, strconv.Itoa(vmID)), vmID)

		wg.Add(1)
		go func(ctxInner context.Context, id int) {
			defer wg.Done()
			pullImageWithRemoteSnapshotterInVM(ctxInner, t, strconv.Itoa(id), fcClient)
		}(namespaces.WithNamespace(ctx, strconv.Itoa(vmID)), vmID)

	}

	// wait for all to be launched, pull service discovery endpoint and individual metrics endpoints
	wg.Wait()
	verifyMetricsResponse(t, numberOfVms)
}

func pullImageWithRemoteSnapshotterInVM(ctx context.Context, t *testing.T, vmID string, fcClient *client.Client) {
	client, err := containerd.New(integtest.ContainerdSockPath, containerd.WithDefaultRuntime(integtest.FirecrackerRuntime))
	require.NoError(t, err, "Unable to create client to containerd service at %s, is containerd running?", integtest.ContainerdSockPath)

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
			VcpuCount:  1,
			MemSizeMib: 512,
		},
		ContainerCount: 1,
	})
	require.NoErrorf(t, err, "Failed to create microVM[%s]", vmID)

	_, err = fcClient.SetVMMetadata(ctx, &proto.SetVMMetadataRequest{
		VMID:     vmID,
		Metadata: fmt.Sprintf(dockerMetadataTemplate, "ghcr.io", "", ""),
	})
	require.NoError(t, err)

	image, err := client.Pull(ctx, imageRef,
		containerd.WithPullUnpack,
		containerd.WithPullSnapshotter(snapshotterNameMetrics),
		containerd.WithImageHandlerWrapper(source.AppendDefaultLabelsHandlerWrapper(imageRef, 10*1024*1024)),
	)
	require.NoError(t, err, "Failed to pull alpine image for VM: %s", vmID)
	client.ImageService().Delete(ctx, image.Name()) // this is failing Walk
}

type metricsTarget struct {
	Targets []string          `json:"targets"`
	Labels  map[string]string `json:"labels"`
}

func verifyMetricsResponse(t *testing.T, numberOfVms int) {
	sdResponse, err := http.Get(serviceDiscoveryEndpoint)
	require.NoError(t, err, "get sd failed")
	defer sdResponse.Body.Close()

	bytes, err := io.ReadAll(sdResponse.Body)
	require.NoError(t, err, "Failed to read service discovery response body")

	var response []metricsTarget
	err = json.Unmarshal(bytes, &response)
	require.NoError(t, err, "Failed to unmarshall service discovery response")
	fmt.Println(response)

	require.Len(t, response, numberOfVms)

	// Test pulling individual metrics proxies
	for _, mt := range response {
		metricsResponse, err := http.Get("http://" + mt.Targets[0] + "/metrics")
		require.NoError(t, err, "Failed to get metrics proxy")

		mBytes, err := io.ReadAll(io.LimitReader(metricsResponse.Body, 300))
		require.NoError(t, err, "Failed to read metrics response body")
		require.NotEmpty(t, mBytes)
		metricsResponse.Body.Close()
	}
}
