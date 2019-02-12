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

func main() {
	var ip = flag.String("ip", "", "ip address assigned to the container")
	log.SetFlags(log.Ldate | log.Ltime | log.Lmicroseconds)
	flag.Parse()
	if err := taskWorkflow(*ip); err != nil {
		log.Fatal(err)
	}
}

func taskWorkflow(containerIP string) error {
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
		withFirecrackerConfig(containerIP, "AA:FC:00:00:00:01", "tap0"),
	)
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

func withFirecrackerConfig(containerIP string, mac string, hostDevName string) containerd.NewTaskOpts {
	return func(ctx context.Context, _ *containerd.Client, ti *containerd.TaskInfo) error {
		if containerIP != "" {
			firecrackerConfig := &proto.FirecrackerConfig{
				NetworkInterfaces: []*proto.FirecrackerNetworkInterface{
					{
						MacAddress:  mac,
						HostDevName: hostDevName,
					},
				},
			}
			ti.Options = firecrackerConfig
		}
		return nil
	}
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
