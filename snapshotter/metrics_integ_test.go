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
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"testing"

	"github.com/containerd/containerd"
	"github.com/containerd/containerd/namespaces"
	"github.com/firecracker-microvm/firecracker-containerd/firecracker-control/client"
	"github.com/firecracker-microvm/firecracker-containerd/internal/integtest"
	"github.com/firecracker-microvm/firecracker-containerd/proto"
	"github.com/firecracker-microvm/firecracker-containerd/snapshotter/internal/integtest/stargz/fs/source"
	"github.com/stretchr/testify/require"
	"golang.org/x/sync/errgroup"
)

const (
	serviceDiscoveryEndpoint = "http://localhost:8080"

	stargzFsOperation     = "stargz_fs_operation_duration_milliseconds_bucket"
	stargzFsOperationHelp = "# HELP stargz_fs_operation_duration_milliseconds Latency"
	stargzFsOperationType = "# TYPE stargz_fs_operation_duration_milliseconds histogram"
)

func TestSnapshotterMetrics_Isolated(t *testing.T) {
	integtest.Prepare(t)

	gen, err := integtest.NewVMIDGen()
	require.NoError(t, err, "Failed to create a VMIDGen")
	vmID := gen.VMID(0)

	ctx := namespaces.WithNamespace(context.Background(), vmID)

	fcClient, err := integtest.NewFCControlClient(integtest.ContainerdSockPath)
	defer fcClient.StopVM(ctx, &proto.StopVMRequest{VMID: vmID})
	require.NoError(t, err, "Failed to create fccontrol client")

	require.NoError(t, pullImageWithRemoteSnapshotterInVM(ctx, vmID, fcClient))
	verifyMetricsResponse(t, 1)
}

func TestSnapshotterMetricsMultipleVMs_Isolated(t *testing.T) {
	integtest.Prepare(t)

	numberOfVms := integtest.NumberOfVms
	fcClient, err := integtest.NewFCControlClient(integtest.ContainerdSockPath)
	require.NoError(t, err, "Failed to create fccontrol client")

	group, ctx := errgroup.WithContext(context.Background())
	gen, err := integtest.NewVMIDGen()
	require.NoError(t, err, "Failed to create a VMIDGen")

	for vmID := 0; vmID < numberOfVms; vmID++ {
		id := gen.VMID(vmID)
		ctxNamespace := namespaces.WithNamespace(ctx, id)
		defer fcClient.StopVM(ctxNamespace, &proto.StopVMRequest{VMID: id})

		group.Go(
			func() error {
				return pullImageWithRemoteSnapshotterInVM(ctxNamespace, id, fcClient)
			},
		)

	}

	err = group.Wait()
	require.NoError(t, err)
	verifyMetricsResponse(t, numberOfVms)
}

// TODO (ginglis13): ensure service discovery response changes as VMs are started and stopped.
// pending https://github.com/firecracker-microvm/firecracker-containerd/issues/651
// func TestSnapshotterMetricsMultipleVMsStopStart_Isolated(t *testing.T) {}

func pullImageWithRemoteSnapshotterInVM(ctx context.Context, vmID string, fcClient *client.Client) error {
	client, err := containerd.New(integtest.ContainerdSockPath, containerd.WithDefaultRuntime(integtest.FirecrackerRuntime))
	if err != nil {
		return fmt.Errorf("Unable to create client to containerd service at %s, is containerd running?: %v", integtest.ContainerdSockPath, err)
	}
	if _, err = fcClient.CreateVM(ctx, &proto.CreateVMRequest{
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
	}); err != nil {
		return fmt.Errorf("Failed to create microVM[%s]: %v", vmID, err)
	}

	if _, err = fcClient.SetVMMetadata(ctx, &proto.SetVMMetadataRequest{
		VMID:     vmID,
		Metadata: fmt.Sprintf(dockerMetadataTemplate, "ghcr.io", noAuth, noAuth),
	}); err != nil {
		return fmt.Errorf("Failed to set metadata on microVM[%s]: %v", vmID, err)
	}

	image, err := client.Pull(ctx, al2stargz,
		containerd.WithPullUnpack,
		containerd.WithPullSnapshotter(snapshotterName),
		containerd.WithImageHandlerWrapper(source.AppendDefaultLabelsHandlerWrapper(al2stargz, 10*1024*1024)),
	)
	if err != nil {
		return fmt.Errorf("Failed to pull image on microVM[%s]: %v", vmID, err)
	}
	if err = client.ImageService().Delete(ctx, image.Name()); err != nil {
		return fmt.Errorf("Failed to delete image on microVM[%s]: %v", vmID, err)
	}

	return nil
}

type metricsTarget struct {
	Targets []string          `json:"targets"`
	Labels  map[string]string `json:"labels"`
}

func verifyMetricsResponse(t *testing.T, numberOfVms int) {
	sdResponse, err := http.Get(serviceDiscoveryEndpoint)
	require.NoError(t, err, "Get service discovery failed")
	defer sdResponse.Body.Close()

	sdBytes, err := io.ReadAll(sdResponse.Body)
	require.NoError(t, err, "Failed to read service discovery response body")

	var metricsTargets []metricsTarget
	err = json.Unmarshal(sdBytes, &metricsTargets)
	require.NoError(t, err, "Failed to unmarshall service discovery response")

	require.Len(t, metricsTargets, numberOfVms)

	// Test pulling individual metrics proxies
	// uniqueTargets acts as a set to ensure each metrics target is a unique host:port
	uniqueTargets := make(map[string]struct{})
	for _, mt := range metricsTargets {
		uniqueTargets[mt.Targets[0]] = struct{}{}
		metricsResponse, err := http.Get("http://" + mt.Targets[0] + "/metrics")
		require.NoError(t, err, "Failed to get metrics proxy")
		require.Equal(t, metricsResponse.StatusCode, http.StatusOK)
		defer metricsResponse.Body.Close()

		mBytes, err := io.ReadAll(metricsResponse.Body)
		require.NoError(t, err, "Failed to read metrics response body")
		require.NotEmpty(t, mBytes, "Empty metrics response body")
		require.True(t, bytes.Contains(mBytes, []byte(stargzFsOperation)), "metrics response missing fs operations bucket")
		require.True(t, bytes.Contains(mBytes, []byte(stargzFsOperationHelp)), "metrics response missing fs operations HELP")
		require.True(t, bytes.Contains(mBytes, []byte(stargzFsOperationType)), "metrics response missing fs operations TYPE")
	}
	require.Len(t, uniqueTargets, numberOfVms)
}
