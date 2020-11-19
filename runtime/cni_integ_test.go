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
	"io/ioutil"
	"os"
	"os/exec"
	"strconv"
	"sync"
	"testing"
	"time"

	args "github.com/awslabs/tc-redirect-tap/cmd/tc-redirect-tap/args"
	"github.com/containerd/containerd"
	"github.com/containerd/containerd/namespaces"
	"github.com/containerd/containerd/oci"
	"github.com/containerd/containerd/pkg/ttrpcutil"
	"github.com/firecracker-microvm/firecracker-containerd/config"
	"github.com/firecracker-microvm/firecracker-containerd/internal"
	"github.com/firecracker-microvm/firecracker-containerd/proto"
	fccontrol "github.com/firecracker-microvm/firecracker-containerd/proto/service/fccontrol/ttrpc"
	"github.com/firecracker-microvm/firecracker-containerd/runtime/firecrackeroci"
	"github.com/shirou/gopsutil/cpu"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCNISupport_Isolated(t *testing.T) {
	prepareIntegTest(t)
	testTimeout := 120 * time.Second
	ctx, cancel := context.WithTimeout(namespaces.WithNamespace(context.Background(), defaultNamespace), testTimeout)
	defer cancel()

	client, err := containerd.New(containerdSockPath, containerd.WithDefaultRuntime(firecrackerRuntime))
	require.NoError(t, err, "unable to create client to containerd service at %s, is containerd running?", containerdSockPath)
	defer client.Close()

	pluginClient, err := ttrpcutil.NewClient(containerdSockPath + ".ttrpc")
	require.NoError(t, err, "failed to create ttrpc client")

	image, err := alpineImage(ctx, client, defaultSnapshotterName)
	require.NoError(t, err, "failed to get alpine image")

	numVMs := 5
	var vmIDs []string
	webpages := make(map[string]string)
	for i := 0; i < numVMs; i++ {
		vmID := fmt.Sprintf("vm-%d", i)
		vmIDs = append(vmIDs, vmID)
		webpages[vmID] = fmt.Sprintf("Hello, my virtual machine %s\n", vmID)
	}

	localServices, err := internal.NewLocalNetworkServices(webpages)
	require.NoError(t, err, "failed to create local network test services")

	cniNetworkName := "fcnet-test"
	err = writeCNIConf("/etc/cni/conf.d/fcnet-test.conflist",
		"tc-redirect-tap", cniNetworkName, localServices.DNSServerIP())
	require.NoError(t, err, "failed to write test cni conf")

	go func() {
		defer cancel()
		err := localServices.Serve(ctx)
		if err != nil {
			t.Logf("failed serving local dns and http: %v", err)
		}
	}()

	var vmGroup sync.WaitGroup
	const jailerUID = 300000
	const jailerGID = 300000
	for _, vmID := range vmIDs {
		vmGroup.Add(1)
		go func(vmID string) {
			defer vmGroup.Done()

			fcClient := fccontrol.NewFirecrackerClient(pluginClient.Client())
			_, err := fcClient.CreateVM(ctx, &proto.CreateVMRequest{
				VMID: vmID,
				MachineCfg: &proto.FirecrackerMachineConfiguration{
					MemSizeMib: 512,
				},
				NetworkInterfaces: []*proto.FirecrackerNetworkInterface{{
					CNIConfig: &proto.CNIConfiguration{
						NetworkName:   cniNetworkName,
						InterfaceName: "veth0",
						Args: []*proto.CNIConfiguration_CNIArg{
							{
								Key:   "IgnoreUnknown",
								Value: "true",
							},
							{
								Key:   args.TCRedirectTapUID,
								Value: fmt.Sprintf("%d", jailerUID),
							},
							{
								Key:   args.TCRedirectTapGID,
								Value: fmt.Sprintf("%d", jailerGID),
							},
						},
					},
				}},
				JailerConfig: &proto.JailerConfig{
					UID: jailerUID,
					GID: jailerGID,
				},
			})
			require.NoError(t, err, "failed to create vm")

			containerName := fmt.Sprintf("%s-container", vmID)
			snapshotName := fmt.Sprintf("%s-snapshot", vmID)

			newContainer, err := client.NewContainer(ctx,
				containerName,
				containerd.WithSnapshotter(defaultSnapshotterName),
				containerd.WithNewSnapshot(snapshotName, image),
				containerd.WithNewSpec(
					oci.WithProcessArgs("/usr/bin/wget",
						"-q",      // don't print to stderr unless an error occurs
						"-O", "-", // write to stdout
						localServices.URL(vmID)),
					firecrackeroci.WithVMID(vmID),
					firecrackeroci.WithVMNetwork,
				),
			)
			require.NoError(t, err, "failed to create container %s", containerName)

			stdout := startAndWaitTask(ctx, t, newContainer)
			t.Logf("stdout output from task %q: %s", containerName, stdout)
			assert.Equalf(t, webpages[vmID], stdout, "container %q did not emit expected stdout", containerName)
		}(vmID)
	}

	vmGroup.Wait()
}

func TestAutomaticCNISupport_Isolated(t *testing.T) {
	prepareIntegTest(t, withDefaultNetwork())

	testTimeout := 120 * time.Second
	ctx, cancel := context.WithTimeout(namespaces.WithNamespace(context.Background(), defaultNamespace), testTimeout)
	defer cancel()

	client, err := containerd.New(containerdSockPath, containerd.WithDefaultRuntime(firecrackerRuntime))
	require.NoError(t, err, "unable to create client to containerd service at %s, is containerd running?", containerdSockPath)
	defer client.Close()

	image, err := alpineImage(ctx, client, defaultSnapshotterName)
	require.NoError(t, err, "failed to get alpine image")

	numVMs := 5
	var taskIDs []string
	webpages := make(map[string]string)
	for i := 0; i < numVMs; i++ {
		taskID := fmt.Sprintf("task-%d", i)
		taskIDs = append(taskIDs, taskID)
		webpages[taskID] = fmt.Sprintf("Hello, my task %s\n", taskID)
	}

	localServices, err := internal.NewLocalNetworkServices(webpages)
	require.NoError(t, err, "failed to create local network test services")

	cniNetworkName := "fcnet"
	err = writeCNIConf("/etc/cni/conf.d/fcnet.conflist",
		"tc-redirect-tap", cniNetworkName, localServices.DNSServerIP())
	require.NoError(t, err, "failed to write test cni conf")

	go func() {
		require.NoError(t, localServices.Serve(ctx), "failed serving local dns and http")
	}()

	var taskGroup sync.WaitGroup
	for _, taskID := range taskIDs {
		taskGroup.Add(1)
		go func(taskID string) {
			defer taskGroup.Done()

			snapshotName := fmt.Sprintf("%s-snapshot", taskID)

			newContainer, err := client.NewContainer(ctx,
				taskID,
				containerd.WithSnapshotter(defaultSnapshotterName),
				containerd.WithNewSnapshot(snapshotName, image),
				containerd.WithNewSpec(
					oci.WithProcessArgs("/usr/bin/wget",
						"-q",      // don't print to stderr unless an error occurs
						"-O", "-", // write to stdout
						localServices.URL(taskID)),
					firecrackeroci.WithVMNetwork,
				),
			)
			require.NoError(t, err, "failed to create container %s", taskID)

			stdout := startAndWaitTask(ctx, t, newContainer)
			t.Logf("stdout output from task %q: %s", taskID, stdout)
			assert.Equalf(t, webpages[taskID], stdout, "container %q did not emit expected stdout", taskID)
		}(taskID)
	}

	taskGroup.Wait()
}

func TestCNIPlugin_Performance(t *testing.T) {
	prepareIntegTest(t)

	numVMs := perfTestVMCount(t)
	runtimeDuration := perfTestRuntime(t)
	vmMemSizeMB := perfTestVMMemsizeMB(t)
	targetBandwidth := perfTestTargetBandwidth(t)
	pluginName := perfTestChainedPluginName(t)

	testTimeout := runtimeDuration + 20*time.Minute
	ctx, cancel := context.WithTimeout(namespaces.WithNamespace(context.Background(), defaultNamespace), testTimeout)
	defer cancel()

	client, err := containerd.New(containerdSockPath, containerd.WithDefaultRuntime(firecrackerRuntime))
	require.NoError(t, err, "unable to create client to containerd service at %s, is containerd running?", containerdSockPath)
	defer client.Close()

	pluginClient, err := ttrpcutil.NewClient(containerdSockPath + ".ttrpc")
	require.NoError(t, err, "failed to create ttrpc client")

	fcClient := fccontrol.NewFirecrackerClient(pluginClient.Client())

	image, err := iperf3Image(ctx, client, defaultSnapshotterName)
	require.NoError(t, err, "failed to get iperf3 image")

	cniNetworkName := "fcnet"
	err = writeCNIConf("/etc/cni/conf.d/fcnet.conflist",
		pluginName, cniNetworkName, "")
	require.NoError(t, err, "failed to write test cni conf")

	// create an endpoint in the host netns for the iperf servers to bind/listen to
	testDevName := "testdev0"
	ipAddr := "10.0.0.1"
	ipCidr := fmt.Sprintf("%s/32", ipAddr)
	runCommand(ctx, t, "ip", "tuntap", "add", testDevName, "mode", "tun")
	runCommand(ctx, t, "ip", "addr", "add", ipCidr, "dev", testDevName)
	runCommand(ctx, t, "ip", "link", "set", "dev", testDevName, "up")

	vmID := func(vmIndex int) string {
		return fmt.Sprintf("vm-%d", vmIndex)
	}

	var vmGroup sync.WaitGroup
	containers := make(chan containerd.Container, numVMs)
	for i := 0; i < numVMs; i++ {
		vmGroup.Add(1)
		go func(vmIndex int) {
			defer vmGroup.Done()

			// use a unique port for each VM
			iperfPort := fmt.Sprintf("34%03d", vmIndex)
			ifName := fmt.Sprintf("veth%d", vmIndex)

			go func() {
				runCommand(ctx, t, "iperf3",
					"--bind", ipAddr,
					"--port", iperfPort,
					"--interval", "60",
					"--server",
				)
			}()

			_, err = fcClient.CreateVM(ctx, &proto.CreateVMRequest{
				VMID: vmID(vmIndex),
				MachineCfg: &proto.FirecrackerMachineConfiguration{
					MemSizeMib: uint32(vmMemSizeMB),
				},
				NetworkInterfaces: []*proto.FirecrackerNetworkInterface{{
					CNIConfig: &proto.CNIConfiguration{
						NetworkName:   cniNetworkName,
						InterfaceName: ifName,
					},
				}},
			})
			require.NoError(t, err, "failed to create vm")

			containerName := fmt.Sprintf("%s-container", vmID(vmIndex))
			snapshotName := fmt.Sprintf("%s-snapshot", vmID(vmIndex))

			newContainer, err := client.NewContainer(ctx,
				containerName,
				containerd.WithSnapshotter(defaultSnapshotterName),
				containerd.WithNewSnapshot(snapshotName, image),
				containerd.WithNewSpec(
					oci.WithProcessArgs("/usr/bin/iperf3",
						"--port", iperfPort,
						"--time", strconv.Itoa(int(runtimeDuration/time.Second)),
						"--bitrate", targetBandwidth,
						"--interval", "60",
						"--client", ipAddr,
					),
					firecrackeroci.WithVMID(vmID(vmIndex)),
					firecrackeroci.WithVMNetwork,
				),
			)
			require.NoError(t, err, "failed to create container %s", containerName)

			containers <- newContainer
		}(i)
	}

	vmGroup.Wait()
	close(containers)

	avgCPUDeltas := make(chan cpu.TimesStat)
	cpuCtx, cpuCtxCancel := context.WithCancel(ctx)
	go func() {
		defer close(avgCPUDeltas)
		result, err := internal.AverageCPUDeltas(cpuCtx, 1*time.Second)
		require.NoError(t, err, "failed collecting cpu times")
		avgCPUDeltas <- *result
	}()

	var taskGroup sync.WaitGroup
	for container := range containers {
		taskGroup.Add(1)
		go func(container containerd.Container) {
			defer taskGroup.Done()
			stdout := startAndWaitTask(ctx, t, container)
			t.Logf("stdout output from task %q: %s", container.ID(), stdout)
		}(container)
	}

	taskGroup.Wait()
	cpuCtxCancel()

	t.Logf("%+v", <-avgCPUDeltas)
}

func writeCNIConf(path, chainedPluginName, networkName, nameserver string) error {
	return ioutil.WriteFile(path, []byte(fmt.Sprintf(`{
  "cniVersion": "0.3.1",
  "name": "%s",
  "plugins": [
    {
      "type": "ptp",
      "ipMasq": true,
      "mtu": 1500,
      "ipam": {
        "type": "host-local",
        "subnet": "192.168.1.0/24"
      },
      "dns": {"nameservers": ["%s"]}
    },
    {
      "type": "%s"
    }
  ]
}`, networkName, nameserver, chainedPluginName)), 0644)
}

func withDefaultNetwork() func(c *config.Config) {
	return func(c *config.Config) {
		c.DefaultNetworkInterfaces = []proto.FirecrackerNetworkInterface{
			{
				CNIConfig: &proto.CNIConfiguration{
					NetworkName:   "fcnet",
					InterfaceName: "veth0",
				},
			},
		}
	}
}

func runCommand(ctx context.Context, t *testing.T, name string, args ...string) {
	t.Helper()
	output, err := exec.CommandContext(ctx, name, args...).CombinedOutput()
	require.NoErrorf(t, err, "command %+v failed, output: %s", append([]string{name}, args...), string(output))
}

func perfTestVMCount(t *testing.T) int {
	t.Helper()
	return requiredEnvInt(t, "PERF_VMCOUNT")
}

func perfTestRuntime(t *testing.T) time.Duration {
	t.Helper()
	return time.Duration(requiredEnvInt(t, "PERF_RUNTIME_SECONDS")) * time.Second
}

func perfTestVMMemsizeMB(t *testing.T) int {
	t.Helper()
	return requiredEnvInt(t, "PERF_VM_MEMSIZE_MB")
}

func perfTestTargetBandwidth(t *testing.T) string {
	t.Helper()
	return requiredEnv(t, "PERF_TARGET_BANDWIDTH")
}

func perfTestChainedPluginName(t *testing.T) string {
	t.Helper()
	return requiredEnv(t, "PERF_PLUGIN_NAME")
}

func requiredEnvInt(t *testing.T, key string) int {
	t.Helper()
	val, err := strconv.Atoi(requiredEnv(t, key))
	require.NoErrorf(t, err, "%s env is not an int: %q", key, val)
	return val
}

func requiredEnv(t *testing.T, key string) string {
	t.Helper()
	envVal := os.Getenv(key)
	require.NotEmpty(t, envVal, "%s env is not set", key)
	return envVal
}

