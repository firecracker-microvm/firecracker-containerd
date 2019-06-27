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
	"bytes"
	"context"
	"fmt"
	"io/ioutil"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/containerd/containerd"
	"github.com/containerd/containerd/api/events"
	"github.com/containerd/containerd/api/services/tasks/v1"
	"github.com/containerd/containerd/cio"
	"github.com/containerd/containerd/namespaces"
	"github.com/containerd/containerd/oci"
	"github.com/containerd/containerd/pkg/ttrpcutil"
	"github.com/containerd/containerd/runtime"
	"github.com/containerd/typeurl"
	"github.com/opencontainers/runtime-spec/specs-go"
	"github.com/pkg/errors"
	"github.com/shirou/gopsutil/process"
	"github.com/stretchr/testify/require"

	_ "github.com/firecracker-microvm/firecracker-containerd/firecracker-control"
	"github.com/firecracker-microvm/firecracker-containerd/internal"
	"github.com/firecracker-microvm/firecracker-containerd/proto"
	fccontrol "github.com/firecracker-microvm/firecracker-containerd/proto/service/fccontrol/ttrpc"
	"github.com/firecracker-microvm/firecracker-containerd/runtime/firecrackeroci"
)

const (
	defaultNamespace = namespaces.Default

	containerdSockPath = "/run/containerd/containerd.sock"
	debianDockerImage  = "docker.io/library/debian:latest"

	firecrackerRuntime   = "aws.firecracker"
	naiveSnapshotterName = "firecracker-naive"
	shimProcessName      = "containerd-shim-aws-firecracker"

	defaultVMRootfsPath = "/var/lib/firecracker-containerd/runtime/default-rootfs.img"
	defaultVMNetDevName = "eth0"
	varRunDir           = "/var/run/firecracker-containerd"
)

func TestShimExitsUponContainerDelete_Isolated(t *testing.T) {
	internal.RequiresIsolation(t)

	ctx := namespaces.WithNamespace(context.Background(), defaultNamespace)

	client, err := containerd.New(containerdSockPath)
	require.NoError(t, err, "unable to create client to containerd service at %s, is containerd running?", containerdSockPath)
	defer client.Close()

	pullTimeout := 180 * time.Second
	pullCtx, pullCancel := context.WithTimeout(ctx, pullTimeout)
	defer pullCancel()

	image, err := client.Pull(pullCtx, debianDockerImage, containerd.WithPullUnpack, containerd.WithPullSnapshotter(naiveSnapshotterName))
	require.NoError(t, err, "failed to pull image %s, is the the %s snapshotter running?", debianDockerImage, naiveSnapshotterName)

	testTimeout := 60 * time.Second
	testCtx, testCancel := context.WithTimeout(ctx, testTimeout)
	defer testCancel()

	containerName := fmt.Sprintf("%s-%d", t.Name(), time.Now().UnixNano())
	snapshotName := fmt.Sprintf("%s-snapshot", containerName)
	container, err := client.NewContainer(testCtx,
		containerName,
		containerd.WithRuntime(firecrackerRuntime, nil),
		containerd.WithSnapshotter(naiveSnapshotterName),
		containerd.WithNewSnapshot(snapshotName, image),
		containerd.WithNewSpec(
			oci.WithProcessArgs("sleep", fmt.Sprintf("%d", testTimeout/time.Second)),
		),
	)
	require.NoError(t, err, "failed to create container %s", containerName)

	_, err = client.NewContainer(testCtx,
		fmt.Sprintf("should-fail-%s-%d", t.Name(), time.Now().UnixNano()),
		containerd.WithRuntime(firecrackerRuntime, nil),
		containerd.WithSnapshotter(naiveSnapshotterName),
		containerd.WithNewSnapshot(snapshotName, image),
		containerd.WithNewSpec(
			oci.WithProcessArgs("sleep", fmt.Sprintf("%d", testTimeout/time.Second)),
		),
	)
	require.Error(t, err, "should not be able to create additional container when no drives are available")

	task, err := container.NewTask(testCtx, cio.NewCreator(cio.WithStdio))
	require.NoError(t, err, "failed to create task for container %s", containerName)

	exitEventCh, exitEventErrCh := client.Subscribe(testCtx, fmt.Sprintf(`topic=="%s"`, runtime.TaskExitEventTopic))

	err = task.Start(testCtx)
	require.NoError(t, err, "failed to start task for container %s", containerName)

	shimProcesses, err := internal.WaitForProcessToExist(testCtx, time.Second,
		func(ctx context.Context, p *process.Process) (bool, error) {
			processExecutable, err := p.ExeWithContext(ctx)
			if err != nil {
				return false, err
			}

			return filepath.Base(processExecutable) == shimProcessName, nil
		},
	)
	require.NoError(t, err, "failed waiting for expected shim process %q to come up", shimProcessName)
	require.Len(t, shimProcesses, 1, "expected only one shim process to exist")
	shimProcess := shimProcesses[0]

	err = task.Kill(testCtx, syscall.SIGKILL)
	require.NoError(t, err, "failed to SIGKILL containerd task %s", containerName)

	_, err = task.Delete(testCtx)
	require.NoError(t, err, "failed to Delete containerd task %s", containerName)

	select {
	case envelope := <-exitEventCh:
		unmarshaledEvent, err := typeurl.UnmarshalAny(envelope.Event)
		require.NoError(t, err, "failed to unmarshal event")

		switch event := unmarshaledEvent.(type) {
		case *events.TaskExit:
			require.Equal(t, container.ID(), event.ContainerID, "received exit event from expected container %s", container.ID())
		default:
			require.Fail(t, "unexpected event type", "received unexpected non-exit event type on topic: %s", envelope.Topic)
		}

		err = internal.WaitForPidToExit(testCtx, time.Second, shimProcess.Pid)
		require.NoError(t, err, "failed waiting for shim process \"%s\" to exit", shimProcessName)

		namespaceVarRunDir := filepath.Join(varRunDir, namespaces.Default)
		varRunFCContents, err := ioutil.ReadDir(namespaceVarRunDir)
		require.NoError(t, err, `failed to list directory "%s"`, namespaceVarRunDir)
		require.Len(t, varRunFCContents, 0, "expect %s to be cleared after shims shutdown", namespaceVarRunDir)
	case err = <-exitEventErrCh:
		require.Fail(t, "unexpected error", "unexpectedly received on task exit error channel: %s", err.Error())
	case <-testCtx.Done():
		require.Fail(t, "context canceled", "context canceled while waiting for container \"%s\" exit: %s", containerName, testCtx.Err())
	}
}

// vmIDtoMacAddr converts a provided VMID to a unique Mac Address. This is a convenient way of providing the VMID to clients within
// the VM without the extra complication of alternative approaches like MMDS.
func vmIDtoMacAddr(vmID uint) string {
	var addrParts []string

	// mac addresses have 6 hex components separate by ":", i.e. "11:22:33:44:55:66"
	numMacAddrComponents := uint(6)

	for n := uint(0); n < numMacAddrComponents; n++ {
		// To isolate the value of the nth component, right bit shift the vmID by 8*n (there are 8 bits per component) and
		// mask out any upper bits leftover (bitwise AND with 255)
		addrComponent := (vmID >> (8 * n)) & 255

		// format the component as a two-digit hex string
		addrParts = append(addrParts, fmt.Sprintf("%02x", addrComponent))
	}

	return strings.Join(addrParts, ":")
}

func createTapDevice(ctx context.Context, tapName string) error {
	err := exec.CommandContext(ctx, "ip", "tuntap", "add", tapName, "mode", "tap").Run()
	if err != nil {
		return errors.Wrapf(err, "failed to create tap device %s", tapName)
	}

	err = exec.CommandContext(ctx, "ip", "link", "set", tapName, "up").Run()
	if err != nil {
		return errors.Wrapf(err, "failed to up tap device %s", tapName)
	}

	return nil
}

func TestMultipleVMs_Isolated(t *testing.T) {
	internal.RequiresIsolation(t)

	const (
		numVMs          = 3
		containersPerVM = 5
	)

	testTimeout := 600 * time.Second
	ctx, cancel := context.WithTimeout(namespaces.WithNamespace(context.Background(), defaultNamespace), testTimeout)
	defer cancel()

	client, err := containerd.New(containerdSockPath, containerd.WithDefaultRuntime(firecrackerRuntime))
	require.NoError(t, err, "unable to create client to containerd service at %s, is containerd running?", containerdSockPath)
	defer client.Close()

	image, err := client.Pull(ctx, debianDockerImage, containerd.WithPullUnpack, containerd.WithPullSnapshotter(naiveSnapshotterName))
	require.NoError(t, err, "failed to pull image %s, is the the %s snapshotter running?", debianDockerImage, naiveSnapshotterName)

	rootfsBytes, err := ioutil.ReadFile(defaultVMRootfsPath)
	require.NoError(t, err, "failed to read rootfs file")

	pluginClient, err := ttrpcutil.NewClient(containerdSockPath + ".ttrpc")
	require.NoError(t, err, "failed to create ttrpc client")

	// This test spawns separate VMs in parallel and ensures containers are spawned within each expected VM. It asserts each
	// container ends up in the right VM by assigning each VM a network device with a unique mac address and having each container
	// print the mac address it sees inside its VM.
	var vmWg sync.WaitGroup
	for vmID := 0; vmID < numVMs; vmID++ {
		vmWg.Add(1)
		go func(vmID int) {
			defer vmWg.Done()

			tapName := fmt.Sprintf("tap%d", vmID)
			err = createTapDevice(ctx, tapName)
			require.NoError(t, err, "failed to create tap device for vm %d", vmID)

			// TODO once Noah's immutable rootfs change is merged, we can use that as our rootfs for all VMs instead of copying
			// one per-VM
			rootfsPath := fmt.Sprintf("%s.%d", defaultVMRootfsPath, vmID)
			err = ioutil.WriteFile(rootfsPath, rootfsBytes, 0600)
			require.NoError(t, err, "failed to copy vm rootfs to %s", rootfsPath)

			fcClient := fccontrol.NewFirecrackerClient(pluginClient.Client())
			_, err = fcClient.CreateVM(ctx, &proto.CreateVMRequest{
				VMID: strconv.Itoa(vmID),
				MachineCfg: &proto.FirecrackerMachineConfiguration{
					MemSizeMib: 512,
				},
				RootDrive: &proto.FirecrackerDrive{
					PathOnHost:   rootfsPath,
					IsReadOnly:   false,
					IsRootDevice: true,
				},
				NetworkInterfaces: []*proto.FirecrackerNetworkInterface{
					{
						HostDevName: tapName,
						MacAddress:  vmIDtoMacAddr(uint(vmID)),
						AllowMMDS:   true,
					},
				},
				ContainerCount: containersPerVM,
			})
			require.NoError(t, err, "failed to create vm")

			var containerWg sync.WaitGroup
			for containerID := 0; containerID < containersPerVM; containerID++ {
				containerWg.Add(1)
				go func(containerID int) {
					defer containerWg.Done()
					containerName := fmt.Sprintf("container-%d-%d", vmID, containerID)
					execName := fmt.Sprintf("exec-%d-%d", vmID, containerID)
					snapshotName := fmt.Sprintf("snapshot-%d-%d", vmID, containerID)

					// spawn a container that just prints the VM's eth0 mac address (which we have set uniquely per VM)
					newContainer, err := client.NewContainer(ctx,
						containerName,
						containerd.WithSnapshotter(naiveSnapshotterName),
						containerd.WithNewSnapshot(snapshotName, image),
						containerd.WithNewSpec(
							oci.WithProcessArgs("/bin/sh", "-c", strings.Join([]string{
								fmt.Sprintf("/bin/cat /sys/class/net/%s/address", defaultVMNetDevName),
								"/bin/readlink /proc/self/ns/mnt",
								fmt.Sprintf("/bin/sleep %d", testTimeout/time.Second),
							}, " && ")),
							oci.WithHostNamespace(specs.NetworkNamespace),
							firecrackeroci.WithVMID(strconv.Itoa(vmID)),
						),
					)
					require.NoError(t, err, "failed to create container %s", containerName)

					var taskStdout bytes.Buffer
					var taskStderr bytes.Buffer

					newTask, err := newContainer.NewTask(ctx,
						cio.NewCreator(cio.WithStreams(nil, &taskStdout, &taskStderr)))
					require.NoError(t, err, "failed to create task for container %s", containerName)

					taskExitCh, err := newTask.Wait(ctx)
					require.NoError(t, err, "failed to wait on task for container %s", containerName)

					var execStdout bytes.Buffer
					var execStderr bytes.Buffer

					newExec, err := newTask.Exec(ctx, execName, &specs.Process{
						Args: []string{"/bin/readlink", "/proc/self/ns/mnt"},
						Cwd:  "/",
					}, cio.NewCreator(cio.WithStreams(nil, &execStdout, &execStderr)))
					require.NoError(t, err, "failed to exec %s", execName)

					execExitCh, err := newExec.Wait(ctx)
					require.NoError(t, err, "failed to wait on exec %s", execName)

					err = newTask.Start(ctx)
					require.NoError(t, err, "failed to start task for container %s", containerName)

					err = newExec.Start(ctx)
					require.NoError(t, err, "failed to start exec %s", execName)

					// First wait for the exec to exit, getting what it saw as its mnt namespace from the stdout
					// so that we can make sure its the same namespace as the task
					select {
					case exitStatus := <-execExitCh:
						_, err = client.TaskService().DeleteProcess(ctx, &tasks.DeleteProcessRequest{
							ContainerID: containerName,
							ExecID:      execName,
						})
						require.NoError(t, err, "failed to delete exec %q", execName)

						// if there was anything on stderr, print it to assist debugging
						stderrOutput := execStderr.String()
						if len(stderrOutput) != 0 {
							fmt.Printf("stderr output from exec %q: %q", execName, stderrOutput)
						}

						require.Equal(t, uint32(0), exitStatus.ExitCode())
					case <-ctx.Done():
						require.Fail(t, "context cancelled",
							"context cancelled while waiting for exec %s to exit, err: %v", execName, ctx.Err())
					}

					// Now kill the task and verify it was in the right VM and has the same mnt namespace as its exec
					err = newTask.Kill(ctx, syscall.SIGKILL)
					require.NoError(t, err, "failed to kill task %q", containerName)

					select {
					case <-taskExitCh:
						_, err = client.TaskService().DeleteProcess(ctx, &tasks.DeleteProcessRequest{
							ContainerID: containerName,
						})
						require.NoError(t, err, "failed to delete task %q", containerName)

						// if there was anything on stderr, print it to assist debugging
						stderrOutput := taskStderr.String()
						if len(stderrOutput) != 0 {
							fmt.Printf("stderr output from task %q: %q", containerName, stderrOutput)
						}

						stdoutLines := strings.Split(strings.TrimSpace(taskStdout.String()), "\n")
						require.Len(t, stdoutLines, 2)

						printedVMID := strings.TrimSpace(stdoutLines[0])
						require.Equal(t, vmIDtoMacAddr(uint(vmID)), printedVMID, "unexpected VMID output from container %q", containerName)

						taskMntNS := strings.TrimSpace(stdoutLines[1])
						execMntNS := strings.TrimSpace(execStdout.String())
						require.Equal(t, execMntNS, taskMntNS, "unexpected mnt NS output from container %q", containerName)

					case <-ctx.Done():
						require.Fail(t, "context cancelled",
							"context cancelled while waiting for container %s to exit, err: %v", containerName, ctx.Err())
					}
				}(containerID)
			}

			// verify duplicate CreateVM call fails with right error
			_, err = fcClient.CreateVM(ctx, &proto.CreateVMRequest{VMID: strconv.Itoa(vmID)})
			require.Error(t, err, "did not receive expected error for duplicate CreateVM call")

			// verify GetVMInfo returns expected data
			vmInfoResp, err := fcClient.GetVMInfo(ctx, &proto.GetVMInfoRequest{VMID: strconv.Itoa(vmID)})
			require.NoError(t, err, "failed to get VM Info for VM %d", vmID)
			require.Equal(t, vmInfoResp.VMID, strconv.Itoa(vmID))
			require.Equal(t, vmInfoResp.SocketPath, filepath.Join(varRunDir, defaultNamespace, strconv.Itoa(vmID), "firecracker.sock"))
			require.Equal(t, vmInfoResp.LogFifoPath, filepath.Join(varRunDir, defaultNamespace, strconv.Itoa(vmID), "fc-logs.fifo"))
			require.Equal(t, vmInfoResp.MetricsFifoPath, filepath.Join(varRunDir, defaultNamespace, strconv.Itoa(vmID), "fc-metrics.fifo"))

			// just verify that updating the metadata doesn't return an error, a separate test case is needed
			// to very the MMDS update propagates to the container correctly
			_, err = fcClient.SetVMMetadata(ctx, &proto.SetVMMetadataRequest{
				VMID:     strconv.Itoa(vmID),
				Metadata: "{}",
			})
			require.NoError(t, err, "failed to set VM Metadata for VM %d", vmID)

			containerWg.Wait()

			_, err = fcClient.StopVM(ctx, &proto.StopVMRequest{VMID: strconv.Itoa(vmID)})
			require.NoError(t, err, "failed to stop VM %d", vmID)
		}(vmID)
	}

	vmWg.Wait()
}

func TestStubBlockDevices_Isolated(t *testing.T) {
	internal.RequiresIsolation(t)
	const vmID = 0

	ctx := namespaces.WithNamespace(context.Background(), "default")

	client, err := containerd.New(containerdSockPath, containerd.WithDefaultRuntime(firecrackerRuntime))
	require.NoError(t, err, "unable to create client to containerd service at %s, is containerd running?", containerdSockPath)
	defer client.Close()

	image, err := client.Pull(ctx, debianDockerImage, containerd.WithPullUnpack, containerd.WithPullSnapshotter(naiveSnapshotterName))
	require.NoError(t, err, "failed to pull image %s, is the the %s snapshotter running?", debianDockerImage, naiveSnapshotterName)

	// TODO once Noah's immutable rootfs change is merged, we can use that as our rootfs for all VMs instead of copying
	// one per-VM
	rootfsBytes, err := ioutil.ReadFile(defaultVMRootfsPath)
	require.NoError(t, err, "failed to read rootfs file")
	rootfsPath := fmt.Sprintf("%s.%d", defaultVMRootfsPath, vmID)
	err = ioutil.WriteFile(rootfsPath, rootfsBytes, 0600)
	require.NoError(t, err, "failed to copy vm rootfs to %s", rootfsPath)

	tapName := fmt.Sprintf("tap%d", vmID)
	err = createTapDevice(ctx, tapName)
	require.NoError(t, err, "failed to create tap device for vm %d", vmID)

	containerName := fmt.Sprintf("%s-%d", t.Name(), time.Now().UnixNano())
	snapshotName := fmt.Sprintf("%s-snapshot", containerName)

	pluginClient, err := ttrpcutil.NewClient(containerdSockPath + ".ttrpc")
	require.NoError(t, err, "failed to create ttrpc client")

	fcClient := fccontrol.NewFirecrackerClient(pluginClient.Client())
	_, err = fcClient.CreateVM(ctx, &proto.CreateVMRequest{
		VMID: strconv.Itoa(vmID),
		RootDrive: &proto.FirecrackerDrive{
			PathOnHost:   rootfsPath,
			IsReadOnly:   false,
			IsRootDevice: true,
		},
		NetworkInterfaces: []*proto.FirecrackerNetworkInterface{
			{
				HostDevName: tapName,
				MacAddress:  vmIDtoMacAddr(uint(vmID)),
				AllowMMDS:   true,
			},
		},
		ContainerCount: 5,
	})
	require.NoError(t, err, "failed to create VM")

	newContainer, err := client.NewContainer(ctx,
		containerName,
		containerd.WithSnapshotter(naiveSnapshotterName),
		containerd.WithNewSnapshot(snapshotName, image),
		containerd.WithNewSpec(
			oci.WithProcessArgs("lsblk"),
			firecrackeroci.WithVMID(strconv.Itoa(vmID)),
		),
	)
	require.NoError(t, err, "failed to create container %s", containerName)

	var stdout bytes.Buffer
	var stderr bytes.Buffer

	newTask, err := newContainer.NewTask(ctx,
		cio.NewCreator(cio.WithStreams(nil, &stdout, &stderr)))
	require.NoError(t, err, "failed to create task for container %s", containerName)

	exitCh, err := newTask.Wait(ctx)
	require.NoError(t, err, "failed to wait on task for container %s", containerName)

	err = newTask.Start(ctx)
	require.NoError(t, err, "failed to start task for container %s", containerName)

	const containerID = 0

	select {
	case exitStatus := <-exitCh:
		// if there was anything on stderr, print it to assist debugging
		stderrOutput := stderr.String()
		if len(stderrOutput) != 0 {
			fmt.Printf("stderr output from vm %d, container %d: %s", vmID, containerID, stderrOutput)
		}

		const expectedOutput = `NAME MAJ:MIN RM  SIZE RO TYPE MOUNTPOINT
vda  254:0    0   64M  0 disk 
vdc  254:32   0  512B  0 disk 
vdd  254:48   0  512B  0 disk 
vde  254:64   0  512B  0 disk 
vdf  254:80   0  512B  0 disk`

		require.NoError(t, exitStatus.Error(), "failed to retrieve exitStatus")
		require.Equal(t, uint32(0), exitStatus.ExitCode())
		require.Equal(t, expectedOutput, strings.TrimSpace(stdout.String()))
	case <-ctx.Done():
		require.Fail(t, "context cancelled",
			"context cancelled while waiting for container %s to exit, err: %v", containerName, ctx.Err())
	}
}
