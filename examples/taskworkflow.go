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
	"flag"
	"fmt"
	"io/ioutil"
	"log"
	"net/http"
	"syscall"
	"time"

	"github.com/containerd/containerd"
	"github.com/containerd/containerd/cio"
	"github.com/containerd/containerd/namespaces"
	"github.com/containerd/containerd/oci"
	"github.com/firecracker-microvm/firecracker-containerd/proto"
	specs "github.com/opencontainers/runtime-spec/specs-go"
	"github.com/pkg/errors"
)

const (
	kernelArgsFormat   = "console=ttyS0 noapic reboot=k panic=1 pci=off nomodules rw ip=%s::%s:%s:::off::::"
	defaultMacAddress  = "AA:FC:00:00:00:01"
	defaultHostDevName = "tap0"
)

func main() {
	var ip = flag.String("ip", "", "ip address assigned to the container. Example: -ip 172.16.0.1")
	var gateway = flag.String("gw", "", "gateway ip address. Example: -gw 172.16.0.1")
	var netMask = flag.String("mask", "", "subnet gatway mask. Example: -mask 255.255.255.0")
	var netNS = flag.String("netns", "", "firecracker VM network namespace. Example: -netNS testing")
	var hostDevName = flag.String("host_device", defaultHostDevName,
		"the host device name for the network interface, required when specifying 'netns'. Example: -host_device tap0")
	var macAddress = flag.String("mac", defaultMacAddress,
		"the mac address for the network interface, required when specifying 'netns'. Example: -mac AA:FC:00:00:00:01")
	var kernelNetworkArgs = flag.Bool("kernel_nw_args", false,
		"specifies if network params for the VMs should be included with kernel args. Example: -kernel_nw_args true")
	log.SetFlags(log.Ldate | log.Ltime | log.Lmicroseconds)
	flag.Parse()
	if *kernelNetworkArgs && (*gateway == "" || *netMask == "" || *ip == "") {
		log.Fatal("Incorrect usage. 'ip', 'gw' and 'mask' need to be specified when 'kernel_nw_args' is set")
	}
	if err := taskWorkflow(*ip, *kernelNetworkArgs, *gateway, *netMask, *macAddress, *hostDevName, *netNS); err != nil {
		log.Fatal(err)
	}
}

func taskWorkflow(
	containerIP string,
	kernelNetworkArgs bool,
	gateway string,
	netMask string,
	macAddress string,
	hostDevName string,
	netNS string,
) error {
	log.Println("Creating containerd client")
	client, err := containerd.New("/run/containerd/containerd.sock")
	if err != nil {
		return errors.Wrapf(err, "creating client")

	}
	defer client.Close()
	log.Println("Created containerd client")

	ctx := namespaces.WithNamespace(context.Background(), "firecracker-containerd-example")
	image, err := client.Pull(ctx, "docker.io/library/nginx:latest",
		containerd.WithPullUnpack,
		containerd.WithPullSnapshotter("firecracker-naive"),
	)
	if err != nil {
		return errors.Wrapf(err, "creating container")

	}
	log.Printf("Successfully pulled %s image\n", image.Name())
	container, err := client.NewContainer(
		ctx,
		"demo",
		containerd.WithSnapshotter("firecracker-naive"),
		containerd.WithNewSnapshot("demo-snapshot", image),
		containerd.WithNewSpec(
			oci.WithImageConfig(image),
			oci.WithHostNamespace(specs.NetworkNamespace),
			oci.WithHostHostsFile,
			oci.WithHostResolvconf,
		),
		containerd.WithRuntime("aws.firecracker", nil),
	)
	if err != nil {
		return err
	}
	defer container.Delete(ctx, containerd.WithSnapshotCleanup)

	task, err := container.NewTask(ctx,
		cio.NewCreator(cio.WithStdio),
		func(ctx context.Context, _ *containerd.Client, ti *containerd.TaskInfo) error {
			if containerIP == "" && netNS == "" {
				// No params to configure, return.
				return nil
			}
			// An IP address or the network namespace for the container has
			// been provided. Configure VM opts accordingly.
			builder := newFirecrackerConfigBuilder()
			if containerIP != "" {
				builder.setNetworkConfig(macAddress, hostDevName)
			}
			if kernelNetworkArgs {
				builder.setKernelNetworkArgs(containerIP, gateway, netMask)
			}
			if netNS != "" {
				builder.setNetNS(netNS)
			}
			ti.Options = builder.build()
			return nil
		})
	if err != nil {
		return errors.Wrapf(err, "creating task")

	}
	defer task.Delete(ctx)

	log.Printf("Successfully created task: %s for the container\n", task.ID())
	exitStatusC, err := task.Wait(ctx)
	if err != nil {
		return errors.Wrapf(err, "waiting for task")

	}

	log.Println("Completed waiting for the container task")
	if err := task.Start(ctx); err != nil {
		return errors.Wrapf(err, "starting task")

	}

	log.Println("Successfully started the container task")
	time.Sleep(3 * time.Second)

	if containerIP != "" {
		log.Println("Executing http GET on " + containerIP)
		getResponse(containerIP)
	}

	if err := task.Kill(ctx, syscall.SIGTERM); err != nil {
		return errors.Wrapf(err, "killing task")

	}
	status := <-exitStatusC
	code, _, err := status.Result()
	if err != nil {
		return errors.Wrapf(err, "getting task's exit code")

	}
	log.Printf("task exited with status: %d\n", code)
	return nil
}

type firecrackerConfigBuilder struct {
	vmConfig *proto.FirecrackerConfig
}

func newFirecrackerConfigBuilder() *firecrackerConfigBuilder {
	return &firecrackerConfigBuilder{
		vmConfig: &proto.FirecrackerConfig{},
	}
}

func (builder *firecrackerConfigBuilder) build() *proto.FirecrackerConfig {
	return builder.vmConfig
}

// setNetworkConfig sets the fields in the protobuf message required to
// configure the VM with the container IP, gateway and netmask sepcified.
func (builder *firecrackerConfigBuilder) setNetworkConfig(
	macAddress string,
	hostDevName string,
) {
	builder.vmConfig.NetworkInterfaces = []*proto.FirecrackerNetworkInterface{
		{
			MacAddress:  macAddress,
			HostDevName: hostDevName,
		},
	}
}

func (builder *firecrackerConfigBuilder) setKernelNetworkArgs(
	containerIP string,
	gateway string,
	netMask string,
) {
	builder.vmConfig.KernelArgs = fmt.Sprintf(kernelArgsFormat, containerIP, gateway, netMask)
}

// setNetNS sets the network namespace field in the protobuf message.
func (builder *firecrackerConfigBuilder) setNetNS(netNS string) {
	builder.vmConfig.FirecrackerNetworkNamespace = netNS
}

func getResponse(containerIP string) {
	response, err := http.Get("http://" + containerIP)
	if err != nil {
		log.Println("Unable to get response from " + containerIP)
		return
	}
	defer response.Body.Close()
	contents, err := ioutil.ReadAll(response.Body)
	if err != nil {
		log.Printf("Unable to read response body: %v\n", err)
		return
	}

	log.Printf("Response from [%s]: \n[%s]\n", containerIP, contents)
}
