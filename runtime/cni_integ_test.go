// Copyright 2018-2019 Amazon.com, Inc. or its affiliates. All Rights Reserved.
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
	"sync"
	"testing"
	"time"

	"github.com/containerd/containerd"
	"github.com/containerd/containerd/namespaces"
	"github.com/containerd/containerd/oci"
	"github.com/containerd/containerd/pkg/ttrpcutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/firecracker-microvm/firecracker-containerd/internal"
	"github.com/firecracker-microvm/firecracker-containerd/proto"
	fccontrol "github.com/firecracker-microvm/firecracker-containerd/proto/service/fccontrol/ttrpc"
	"github.com/firecracker-microvm/firecracker-containerd/runtime/firecrackeroci"
)

func TestCNISupport_Isolated(t *testing.T) {
	internal.RequiresIsolation(t)

	testTimeout := 120 * time.Second
	ctx, cancel := context.WithTimeout(namespaces.WithNamespace(context.Background(), defaultNamespace), testTimeout)
	defer cancel()

	client, err := containerd.New(containerdSockPath, containerd.WithDefaultRuntime(firecrackerRuntime))
	require.NoError(t, err, "unable to create client to containerd service at %s, is containerd running?", containerdSockPath)
	defer client.Close()

	pluginClient, err := ttrpcutil.NewClient(containerdSockPath + ".ttrpc")
	require.NoError(t, err, "failed to create ttrpc client")

	image, err := alpineImage(ctx, client, naiveSnapshotterName)
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
	err = writeCNIConf("/etc/cni/conf.d/fcnet-test.conflist", cniNetworkName, localServices.DNSServerIP())
	require.NoError(t, err, "failed to write test cni conf")

	go func() {
		defer cancel()
		err := localServices.Serve(ctx)
		if err != nil {
			t.Logf("failed serving local dns and http: %v", err)
		}
	}()

	var vmGroup sync.WaitGroup
	for _, vmID := range vmIDs {
		vmGroup.Add(1)
		go func(vmID string) {
			defer vmGroup.Done()

			fcClient := fccontrol.NewFirecrackerClient(pluginClient.Client())
			_, err = fcClient.CreateVM(ctx, &proto.CreateVMRequest{
				VMID: vmID,
				MachineCfg: &proto.FirecrackerMachineConfiguration{
					MemSizeMib: 512,
				},
				RootDrive: &proto.FirecrackerDrive{
					PathOnHost:   defaultVMRootfsPath,
					IsReadOnly:   false,
					IsRootDevice: true,
				},
				NetworkInterfaces: []*proto.FirecrackerNetworkInterface{{
					CNIConfig: &proto.CNIConfiguration{
						NetworkName:   cniNetworkName,
						InterfaceName: "veth0",
					},
				}},
			})
			require.NoError(t, err, "failed to create vm")

			containerName := fmt.Sprintf("%s-container", vmID)
			snapshotName := fmt.Sprintf("%s-snapshot", vmID)

			newContainer, err := client.NewContainer(ctx,
				containerName,
				containerd.WithSnapshotter(naiveSnapshotterName),
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
	internal.RequiresIsolation(t)

	testTimeout := 120 * time.Second
	ctx, cancel := context.WithTimeout(namespaces.WithNamespace(context.Background(), defaultNamespace), testTimeout)
	defer cancel()

	client, err := containerd.New(containerdSockPath, containerd.WithDefaultRuntime(firecrackerRuntime))
	require.NoError(t, err, "unable to create client to containerd service at %s, is containerd running?", containerdSockPath)
	defer client.Close()

	image, err := alpineImage(ctx, client, naiveSnapshotterName)
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
	err = writeCNIConf("/etc/cni/conf.d/fcnet.conflist", cniNetworkName, localServices.DNSServerIP())
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
				containerd.WithSnapshotter(naiveSnapshotterName),
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

func writeCNIConf(path string, networkName string, nameserver string) error {
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
      "type": "tc-redirect-tap"
    }
  ]
}`, networkName, nameserver)), 0644)
}
