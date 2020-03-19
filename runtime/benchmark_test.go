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
	"time"

	"github.com/containerd/containerd"
	"github.com/containerd/containerd/namespaces"
	"github.com/containerd/containerd/pkg/ttrpcutil"
	"github.com/firecracker-microvm/firecracker-containerd/proto"
	fccontrol "github.com/firecracker-microvm/firecracker-containerd/proto/service/fccontrol/ttrpc"
	"github.com/gofrs/uuid"
	"github.com/stretchr/testify/require"
)

func createAndStopVM(
	ctx context.Context,
	fcClient fccontrol.FirecrackerService,
	request proto.CreateVMRequest,
) error {
	uuid, err := uuid.NewV4()
	if err != nil {
		return err
	}

	request.VMID = uuid.String()

	_, err = fcClient.CreateVM(ctx, &request)
	if err != nil {
		return err
	}

	_, err = fcClient.StopVM(ctx, &proto.StopVMRequest{VMID: request.VMID})
	if err != nil {
		return err
	}

	return nil
}

func benchmarkCreateAndStopVM(b *testing.B, vcpuCount uint32, kernelArgs string, parallel int) {
	require := require.New(b)

	client, err := containerd.New(containerdSockPath, containerd.WithDefaultRuntime(firecrackerRuntime))
	require.NoError(err, "unable to create client to containerd service at %s, is containerd running?", containerdSockPath)
	defer client.Close()

	ctx := namespaces.WithNamespace(context.Background(), "default")

	pluginClient, err := ttrpcutil.NewClient(containerdSockPath + ".ttrpc")
	require.NoError(err, "failed to create ttrpc client")

	fcClient := fccontrol.NewFirecrackerClient(pluginClient.Client())
	request := proto.CreateVMRequest{
		KernelArgs: kernelArgs,
		MachineCfg: &proto.FirecrackerMachineConfiguration{
			VcpuCount: vcpuCount,
		},
	}

	ch := make(chan error, parallel)
	for i := 0; i < parallel; i++ {
		go func() {
			var err error
			defer func() {
				ch <- err
			}()

			for i := 0; i < b.N; i++ {
				ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
				defer cancel()

				err = createAndStopVM(ctx, fcClient, request)
				if err != nil {
					return
				}
			}
		}()
	}

	// Make sure all goroutines are finished successfully.
	for i := 0; i < parallel; i++ {
		require.NoError(<-ch)
	}
}

func BenchmarkCreateAndStopVM_Vcpu1_Isolated(b *testing.B) {
	prepareIntegTest(b)
	benchmarkCreateAndStopVM(b, 1, defaultRuntimeConfig.KernelArgs, 1)
}
func BenchmarkCreateAndStopVM_Vcpu5_Isolated(b *testing.B) {
	prepareIntegTest(b)
	benchmarkCreateAndStopVM(b, 5, defaultRuntimeConfig.KernelArgs, 1)
}

func BenchmarkCreateAndStopVM_Quiet_Isolated(b *testing.B) {
	prepareIntegTest(b)
	benchmarkCreateAndStopVM(
		b,
		1,
		// Same as https://github.com/firecracker-microvm/firecracker-demo/blob/c22499567b63b4edd85e19ca9b0e9fa398b3300b/start-firecracker.sh#L9
		"ro noapic reboot=k panic=1 pci=off nomodules systemd.log_color=false systemd.unit=firecracker.target init=/sbin/overlay-init tsc=reliable quiet 8250.nr_uarts=0 ipv6.disable=1",
		1,
	)
}
func BenchmarkCreateAndStopVM_Parallel10_Isolated(b *testing.B) {
	prepareIntegTest(b)
	benchmarkCreateAndStopVM(b, 1, defaultRuntimeConfig.KernelArgs, 10)
}
