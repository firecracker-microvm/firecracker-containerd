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

const benchmarkCount = 10

func createAndStopVM(
	ctx context.Context,
	fcClient fccontrol.FirecrackerService,
	request proto.CreateVMRequest,
) (time.Duration, error) {
	uuid, err := uuid.NewV4()
	if err != nil {
		return 0, err
	}

	request.VMID = uuid.String()

	t0 := time.Now()
	_, err = fcClient.CreateVM(ctx, &request)
	if err != nil {
		return 0, err
	}
	elapsed := time.Since(t0)

	_, err = fcClient.StopVM(ctx, &proto.StopVMRequest{VMID: request.VMID})
	if err != nil {
		return 0, err
	}

	return elapsed, nil
}

func benchmarkCreateAndStopVM(t *testing.T, vcpuCount uint32, kernelArgs string) {
	client, err := containerd.New(containerdSockPath, containerd.WithDefaultRuntime(firecrackerRuntime))
	require.NoError(t, err, "unable to create client to containerd service at %s, is containerd running?", containerdSockPath)
	defer client.Close()

	ctx := namespaces.WithNamespace(context.Background(), "default")

	pluginClient, err := ttrpcutil.NewClient(containerdSockPath + ".ttrpc")
	require.NoError(t, err, "failed to create ttrpc client")

	fcClient := fccontrol.NewFirecrackerClient(pluginClient.Client())
	request := proto.CreateVMRequest{
		KernelArgs: kernelArgs,
		MachineCfg: &proto.FirecrackerMachineConfiguration{
			VcpuCount: vcpuCount,
		},
	}

	var samples []time.Duration
	for i := 0; i < benchmarkCount; i++ {
		ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
		defer cancel()

		elapsed, err := createAndStopVM(ctx, fcClient, request)
		if err != nil {
			return
		}
		samples = append(samples, elapsed)
	}

	var average time.Duration
	for _, x := range samples {
		average += x
	}
	average = time.Duration(int64(average) / int64(len(samples)))

	t.Logf("%s: avg:%v samples:%v", t.Name(), average, samples)
}

func TestPerf_CreateVM(t *testing.T) {
	prepareIntegTest(t)

	t.Run("vcpu=1", func(subtest *testing.T) {
		benchmarkCreateAndStopVM(subtest, 1, defaultRuntimeConfig.KernelArgs)
	})
	t.Run("vcpu=5", func(subtest *testing.T) {
		benchmarkCreateAndStopVM(subtest, 1, defaultRuntimeConfig.KernelArgs)
	})
	t.Run("firecracker-demo", func(subtest *testing.T) {
		benchmarkCreateAndStopVM(
			subtest,
			1,
			// Same as https://github.com/firecracker-microvm/firecracker-demo/blob/c22499567b63b4edd85e19ca9b0e9fa398b3300b/start-firecracker.sh#L9
			"ro noapic reboot=k panic=1 pci=off nomodules systemd.log_color=false systemd.unit=firecracker.target init=/sbin/overlay-init tsc=reliable quiet 8250.nr_uarts=0 ipv6.disable=1",
		)
	})
}
