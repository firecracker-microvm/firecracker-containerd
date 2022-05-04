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
	"os"

	"github.com/containerd/containerd"
	"github.com/containerd/containerd/cio"
	"github.com/containerd/containerd/namespaces"
	"github.com/containerd/containerd/reference"
	"github.com/containerd/stargz-snapshotter/fs/source"
	fcclient "github.com/firecracker-microvm/firecracker-containerd/firecracker-control/client"
	"github.com/firecracker-microvm/firecracker-containerd/proto"
	"github.com/firecracker-microvm/firecracker-containerd/runtime/firecrackeroci"
)

const (
	vmID                   = "vm1"
	socketAddress          = "/run/firecracker-containerd/containerd.sock"
	snapshotter            = "proxy"
	containerName          = "remoteSnapshotterDemo"
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

func main() {
	// The example http-address-resolver assumes that the containerd namespace
	// is the sames as the VM ID.
	ctx := namespaces.WithNamespace(context.Background(), vmID)

	if len(os.Args) != 2 {
		fmt.Println("Usage: remote-snapshotter stargz-image-ref")
		os.Exit(1)
	}
	imageRef := os.Args[1]
	refSpec, err := reference.Parse(imageRef)
	if err != nil {
		fmt.Printf("%s is not a valid image reference\n", imageRef)
	}
	dockerHost := refSpec.Hostname()
	dockerUser, ok := os.LookupEnv("DOCKER_USERNAME")
	if !ok {
		fmt.Print("Docker username: ")
		_, err := fmt.Scanln(&dockerUser)
		if err != nil {
			fmt.Println(err)
			return
		}
	}
	dockerPass, ok := os.LookupEnv("DOCKER_PASSWORD")
	if !ok {
		fmt.Print("Docker password: ")
		_, err := fmt.Scanln(&dockerPass)
		if err != nil {
			fmt.Println(err)
			return
		}
	}
	client, err := containerd.New(socketAddress)
	if err != nil {
		fmt.Println(err)
		return
	}
	defer client.Close()

	fcClient, err := fcclient.New(socketAddress + ".ttrpc")
	if err != nil {
		fmt.Println(err)
		return
	}
	defer fcClient.Close()

	fmt.Println("Creating VM")
	_, err = fcClient.CreateVM(ctx, &proto.CreateVMRequest{
		VMID: vmID,
		NetworkInterfaces: []*proto.FirecrackerNetworkInterface{{
			AllowMMDS: true,
			// This assumes the demo CNI network has been installed
			CNIConfig: &proto.CNIConfiguration{
				NetworkName:   "fcnet",
				InterfaceName: "veth0",
			},
		}},
	})
	if err != nil {
		fmt.Println(err)
		return
	}
	defer fcClient.StopVM(ctx, &proto.StopVMRequest{VMID: vmID})

	fmt.Println("Setting docker credential metadata")
	_, err = fcClient.SetVMMetadata(ctx, &proto.SetVMMetadataRequest{
		VMID:     vmID,
		Metadata: fmt.Sprintf(dockerMetadataTemplate, dockerHost, dockerUser, dockerPass),
	})
	if err != nil {
		fmt.Println(err)
		return
	}

	fmt.Println("Pulling the image")
	image, err := client.Pull(ctx, imageRef,
		containerd.WithPullSnapshotter(snapshotter),
		containerd.WithPullUnpack,
		// stargz labels to tell the snapshotter to lazily load the image
		containerd.WithImageHandlerWrapper(source.AppendDefaultLabelsHandlerWrapper(imageRef, 10*1024*1024)))
	if err != nil {
		fmt.Println(err)
		return
	}
	defer client.ImageService().Delete(ctx, image.Name())

	fmt.Println("Creating a container")
	container, err := client.NewContainer(ctx, containerName,
		containerd.WithSnapshotter(snapshotter),
		containerd.WithNewSnapshot(containerName+"-snap", image),
		containerd.WithNewSpec(
			// We can't use the regular oci.WithImageConfig from containerd
			// because it will attempt to get UIDs and GIDs from inside the
			// container by mounting the container's filesystem. With remote
			// snapshotters, that filesystem is inside a VM and inaccessible
			// to the host. The firecrackeroci variation instructs the
			// firecracker-containerd agent that runs inside the VM to perform
			// those UID/GID lookups because it has access to the container's filesystem
			firecrackeroci.WithVMLocalImageConfig(image),
			firecrackeroci.WithVMID(vmID),
			firecrackeroci.WithVMNetwork,
		),
		containerd.WithRuntime("aws.firecracker", nil),
	)
	if err != nil {
		fmt.Println(err)
		return
	}
	defer container.Delete(ctx)

	fmt.Println("Creating a task")
	task, err := container.NewTask(ctx, cio.NewCreator(cio.WithStdio))
	if err != nil {
		fmt.Println(err)
		return
	}
	defer task.Delete(ctx)
	errC, err := task.Wait(ctx)
	if err != nil {
		fmt.Println(err)
		return
	}
	fmt.Println("Starting the task")
	err = task.Start(ctx)
	if err != nil {
		fmt.Println(err)
		return
	}
	<-errC
}
