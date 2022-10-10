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
	"flag"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"syscall"
	"time"

	"github.com/containerd/containerd"
	"github.com/containerd/containerd/cio"
	"github.com/containerd/containerd/namespaces"
	"github.com/containerd/containerd/oci"

	fcclient "github.com/firecracker-microvm/firecracker-containerd/firecracker-control/client"
	"github.com/firecracker-microvm/firecracker-containerd/proto"
	"github.com/firecracker-microvm/firecracker-containerd/runtime/firecrackeroci"
)

const (
	containerdAddress      = "/run/firecracker-containerd/containerd.sock"
	containerdTTRPCAddress = containerdAddress + ".ttrpc"
	namespaceName          = "firecracker-containerd-example"
	macAddress             = "AA:FC:00:00:00:01"
	hostDevName            = "tap0"
)

func main() {
	var containerCIDR = flag.String("ip", "", "ip address and subnet assigned to the container in CIDR notation. Example: -ip 172.16.0.2/24")
	var gatewayIP = flag.String("gw", "", "gateway ip address. Example: -gw 172.16.0.1")
	var snapshotter = flag.String("ss", "devmapper", "snapshotter")

	log.SetFlags(log.Ldate | log.Ltime | log.Lmicroseconds)
	flag.Parse()

	if *containerCIDR != "" && *gatewayIP == "" {
		log.Fatal("Incorrect usage. 'gw' needs to be specified when 'ip' is specified")
	}
	if err := taskWorkflow(*containerCIDR, *gatewayIP, *snapshotter); err != nil {
		log.Fatal(err)
	}
}

func taskWorkflow(containerCIDR, gateway, snapshotter string) (err error) {
	log.Println("Creating containerd client")
	client, err := containerd.New(containerdAddress)
	if err != nil {
		return fmt.Errorf("creating client: %w", err)
	}

	defer client.Close()
	log.Println("Created containerd client")

	ctx := namespaces.WithNamespace(context.Background(), namespaceName)
	image, err := client.Pull(ctx, "docker.io/library/nginx:1.17-alpine",
		containerd.WithPullUnpack,
		containerd.WithPullSnapshotter(snapshotter),
	)
	if err != nil {
		return fmt.Errorf("creating container: %w", err)
	}

	fcClient, err := fcclient.New(containerdTTRPCAddress)
	if err != nil {
		return err
	}

	defer fcClient.Close()

	vmID := "fc-example"
	createVMRequest := &proto.CreateVMRequest{
		VMID: vmID,
		// Enabling Go Race Detector makes in-microVM binaries heavy in terms of CPU and memory.
		MachineCfg: &proto.FirecrackerMachineConfiguration{
			VcpuCount:  2,
			MemSizeMib: 2048,
		},
	}

	if containerCIDR != "" {
		createVMRequest.NetworkInterfaces = []*proto.FirecrackerNetworkInterface{{
			StaticConfig: &proto.StaticNetworkConfiguration{
				MacAddress:  macAddress,
				HostDevName: hostDevName,
				IPConfig: &proto.IPConfiguration{
					PrimaryAddr: containerCIDR,
					GatewayAddr: gateway,
				},
			},
		}}
	}

	_, err = fcClient.CreateVM(ctx, createVMRequest)
	if err != nil {
		return fmt.Errorf("failed to create VM: %w", err)
	}

	defer func() {
		_, stopErr := fcClient.StopVM(ctx, &proto.StopVMRequest{VMID: vmID})
		if stopErr != nil {
			log.Printf("failed to stop VM, err: %v\n", stopErr)
		}
		if err == nil {
			err = stopErr
		}
	}()

	log.Printf("Successfully pulled %s image with %s\n", image.Name(), snapshotter)
	container, err := client.NewContainer(
		ctx,
		"demo",
		containerd.WithSnapshotter(snapshotter),
		containerd.WithNewSnapshot("demo-snapshot", image),
		containerd.WithNewSpec(
			oci.WithImageConfig(image),
			firecrackeroci.WithVMID(vmID),
			firecrackeroci.WithVMNetwork,
		),
		containerd.WithRuntime("aws.firecracker", nil),
	)
	if err != nil {
		return err
	}
	defer container.Delete(ctx, containerd.WithSnapshotCleanup)

	task, err := container.NewTask(ctx, cio.NewCreator(cio.WithStdio))
	if err != nil {
		return fmt.Errorf("creating task: %w", err)

	}
	defer task.Delete(ctx)

	log.Printf("Successfully created task: %s for the container\n", task.ID())
	exitStatusC, err := task.Wait(ctx)
	if err != nil {
		return fmt.Errorf("waiting for task: %w", err)
	}

	log.Println("Completed waiting for the container task")
	if err := task.Start(ctx); err != nil {
		return fmt.Errorf("starting task: %w", err)
	}

	log.Println("Successfully started the container task")
	time.Sleep(3 * time.Second)

	var httpGetErr error
	if containerCIDR != "" {
		ip, _, err := net.ParseCIDR(containerCIDR)
		if err != nil {
			// this is validated as part of the CreateVM call, should never happen
			return fmt.Errorf("failed parsing CIDR %q: %w", containerCIDR, err)
		}

		log.Println("Executing http GET on " + ip.String())
		httpGetErr = getResponse(ip.String())
		if httpGetErr != nil {
			log.Printf("error making http GET request: %v\n", err)
		}
	}

	if err := task.Kill(ctx, syscall.SIGTERM); err != nil {
		return fmt.Errorf("killing task: %w", err)
	}

	status := <-exitStatusC
	code, _, err := status.Result()
	if err != nil {
		return fmt.Errorf("getting task's exit code: %w", err)
	}
	log.Printf("task exited with status: %d\n", code)

	return httpGetErr
}

func getResponse(containerIP string) error {
	response, err := http.Get(fmt.Sprintf("http://%s/", containerIP))
	if err != nil {
		return fmt.Errorf("Unable to get response from %s: %w", containerIP, err)
	}
	defer response.Body.Close()

	contents, err := io.ReadAll(response.Body)
	if err != nil {
		return fmt.Errorf("Unable to read response body from %s: %w", containerIP, err)
	}

	log.Printf("Response from [%s]: \n[%s]\n", containerIP, contents)
	return nil
}
