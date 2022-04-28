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
	"bufio"
	"bytes"
	"context"
	"fmt"
	"io/ioutil"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/containerd/containerd"
	"github.com/containerd/containerd/api/events"
	"github.com/containerd/containerd/cio"
	"github.com/containerd/containerd/containers"
	"github.com/containerd/containerd/errdefs"
	"github.com/containerd/containerd/namespaces"
	"github.com/containerd/containerd/oci"
	"github.com/containerd/containerd/runtime"
	"github.com/containerd/go-runc"
	"github.com/containerd/typeurl"
	"github.com/containernetworking/plugins/pkg/ns"
	"github.com/opencontainers/runtime-spec/specs-go"
	"github.com/pkg/errors"
	"github.com/shirou/gopsutil/process"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/sync/errgroup"

	"github.com/firecracker-microvm/firecracker-containerd/config"
	_ "github.com/firecracker-microvm/firecracker-containerd/firecracker-control"
	"github.com/firecracker-microvm/firecracker-containerd/internal"
	"github.com/firecracker-microvm/firecracker-containerd/internal/vm"
	"github.com/firecracker-microvm/firecracker-containerd/proto"
	fccontrol "github.com/firecracker-microvm/firecracker-containerd/proto/service/fccontrol/ttrpc"
	"github.com/firecracker-microvm/firecracker-containerd/runtime/firecrackeroci"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

const (
	defaultNamespace = namespaces.Default

	firecrackerRuntime     = "aws.firecracker"
	shimProcessName        = "containerd-shim-aws-firecracker"
	firecrackerProcessName = "firecracker"

	defaultVMNetDevName = "eth0"
	numberOfVmsEnvName  = "NUMBER_OF_VMS"
	defaultNumberOfVms  = 5

	tapPrefixEnvName = "TAP_PREFIX"

	defaultBalloonMemory         = int64(66)
	defaultStatsPollingIntervals = int64(1)

	containerdSockPathEnvVar = "CONTAINERD_SOCKET"
)

var (
	findShim        = findProcWithName(shimProcessName)
	findFirecracker = findProcWithName(firecrackerProcessName)

	containerdSockPath = "/run/firecracker-containerd/containerd.sock"
)

func init() {
	if v := os.Getenv(containerdSockPathEnvVar); v != "" {
		containerdSockPath = v
	}
}

// Images are presumed by the isolated tests to have already been pulled
// into the content store. This will just unpack the layers into an
// image with the provided snapshotter.
func unpackImage(ctx context.Context, client *containerd.Client, snapshotterName string, imageRef string) (containerd.Image, error) {
	img, err := client.GetImage(ctx, imageRef)
	if err != nil {
		return nil, errors.Wrap(err, "failed to get image")
	}

	err = img.Unpack(ctx, snapshotterName)
	if err != nil {
		return nil, errors.Wrap(err, "failed to unpack image")
	}

	return img, nil
}

func alpineImage(ctx context.Context, client *containerd.Client, snapshotterName string) (containerd.Image, error) {
	return unpackImage(ctx, client, snapshotterName, "docker.io/library/alpine:3.10.1")
}

func iperf3Image(ctx context.Context, client *containerd.Client, snapshotterName string) (containerd.Image, error) {
	return unpackImage(ctx, client, snapshotterName, "docker.io/mlabbe/iperf3:3.6-r0")
}

func TestShimExitsUponContainerDelete_Isolated(t *testing.T) {
	prepareIntegTest(t)

	ctx := namespaces.WithNamespace(context.Background(), defaultNamespace)

	client, err := containerd.New(containerdSockPath)
	require.NoError(t, err, "unable to create client to containerd service at %s, is containerd running?", containerdSockPath)
	defer client.Close()

	image, err := alpineImage(ctx, client, defaultSnapshotterName)
	require.NoError(t, err, "failed to get alpine image")

	testTimeout := 60 * time.Second
	testCtx, testCancel := context.WithTimeout(ctx, testTimeout)
	defer testCancel()

	containerName := fmt.Sprintf("%s-%d", t.Name(), time.Now().UnixNano())
	snapshotName := fmt.Sprintf("%s-snapshot", containerName)
	container, err := client.NewContainer(testCtx,
		containerName,
		containerd.WithRuntime(firecrackerRuntime, nil),
		containerd.WithSnapshotter(defaultSnapshotterName),
		containerd.WithNewSnapshot(snapshotName, image),
		containerd.WithNewSpec(
			oci.WithProcessArgs("sleep", fmt.Sprintf("%d", testTimeout/time.Second)),
			oci.WithDefaultPathEnv,
		),
	)
	require.NoError(t, err, "failed to create container %s", containerName)

	_, err = client.NewContainer(testCtx,
		fmt.Sprintf("should-fail-%s-%d", t.Name(), time.Now().UnixNano()),
		containerd.WithRuntime(firecrackerRuntime, nil),
		containerd.WithSnapshotter(defaultSnapshotterName),
		containerd.WithNewSnapshot(snapshotName, image),
		containerd.WithNewSpec(
			oci.WithProcessArgs("sleep", fmt.Sprintf("%d", testTimeout/time.Second)),
			oci.WithDefaultPathEnv,
		),
	)
	require.Error(t, err, "should not be able to create additional container when no drives are available")

	task, err := container.NewTask(testCtx, cio.NewCreator(cio.WithStdio))
	require.NoError(t, err, "failed to create task for container %s", containerName)

	exitEventCh, exitEventErrCh := client.Subscribe(testCtx, fmt.Sprintf(`topic=="%s"`, runtime.TaskExitEventTopic))

	err = task.Start(testCtx)
	require.NoError(t, err, "failed to start task for container %s", containerName)

	shimProcesses, err := internal.WaitForProcessToExist(testCtx, time.Second, findShim)
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

		cfg, err := config.LoadConfig("")
		require.NoError(t, err, "failed to load config")
		varRunFCContents, err := ioutil.ReadDir(cfg.ShimBaseDir)
		require.NoError(t, err, `failed to list directory "%s"`, cfg.ShimBaseDir)
		require.Len(t, varRunFCContents, 0, "expect %s to be empty", cfg.ShimBaseDir)
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

func ipCommand(ctx context.Context, args ...string) error {
	out, err := exec.CommandContext(ctx, "ip", args...).CombinedOutput()
	if err != nil {
		s := strings.Trim(string(out), "\n")
		return fmt.Errorf("failed to execute ip %s: %s: %w", strings.Join(args, " "), s, err)
	}
	return nil
}

func deleteTapDevice(ctx context.Context, tapName string) error {
	if err := ipCommand(ctx, "link", "delete", tapName); err != nil {
		return err
	}
	return ipCommand(ctx, "tuntap", "del", tapName, "mode", "tap")
}

func createTapDevice(ctx context.Context, tapName string) error {
	if err := ipCommand(ctx, "tuntap", "add", tapName, "mode", "tap"); err != nil {
		return err
	}
	return ipCommand(ctx, "link", "set", tapName, "up")
}

func TestMultipleVMs_Isolated(t *testing.T) {
	prepareIntegTest(t)

	// This test starts multiple VMs and some may hit firecracker-containerd's
	// default timeout. So overriding the timeout to wait longer.
	// One hour should be enough to start a VM, regardless of the load of
	// the underlying host.
	const createVMTimeout = time.Hour

	netns, err := ns.GetCurrentNS()
	require.NoError(t, err, "failed to get a namespace")

	// numberOfVmsEnvName = NUMBER_OF_VMS ENV and is configurable from buildkite
	numberOfVms := defaultNumberOfVms
	if str := os.Getenv(numberOfVmsEnvName); str != "" {
		numberOfVms, err = strconv.Atoi(str)
		require.NoError(t, err, "failed to get NUMBER_OF_VMS env")
	}
	t.Logf("TestMultipleVMs_Isolated: will run %d vm's", numberOfVms)

	tapPrefix := os.Getenv(tapPrefixEnvName)

	cases := []struct {
		MaxContainers int32
		JailerConfig  *proto.JailerConfig
	}{
		{
			MaxContainers: 5,
		},
		{
			MaxContainers: 3,
			JailerConfig: &proto.JailerConfig{
				UID:   300000,
				GID:   300000,
				NetNS: netns.Path(),
			},
		},
		{
			MaxContainers: 3,
			JailerConfig: &proto.JailerConfig{
				UID:        300000,
				GID:        300000,
				NetNS:      netns.Path(),
				CgroupPath: "/mycgroup",
			},
		},
	}

	testCtx := namespaces.WithNamespace(context.Background(), defaultNamespace)

	client, err := containerd.New(containerdSockPath, containerd.WithDefaultRuntime(firecrackerRuntime))
	require.NoError(t, err, "unable to create client to containerd service at %s, is containerd running?", containerdSockPath)
	defer client.Close()

	image, err := alpineImage(testCtx, client, defaultSnapshotterName)
	require.NoError(t, err, "failed to get alpine image")

	cfg, err := config.LoadConfig("")
	require.NoError(t, err, "failed to load config")

	// This test spawns separate VMs in parallel and ensures containers are spawned within each expected VM. It asserts each
	// container ends up in the right VM by assigning each VM a network device with a unique mac address and having each container
	// print the mac address it sees inside its VM.
	vmEg, vmEgCtx := errgroup.WithContext(testCtx)
	for i := 0; i < numberOfVms; i++ {
		caseTypeNumber := i % len(cases)
		vmID := i
		c := cases[caseTypeNumber]

		f := func(ctx context.Context) error {
			containerCount := c.MaxContainers
			jailerConfig := c.JailerConfig

			tapName := fmt.Sprintf("%stap%d", tapPrefix, vmID)
			err := createTapDevice(ctx, tapName)
			if err != nil {
				return err
			}
			defer deleteTapDevice(ctx, tapName)

			rootfsPath := cfg.RootDrive

			vmIDStr := strconv.Itoa(vmID)
			req := &proto.CreateVMRequest{
				VMID: vmIDStr,
				RootDrive: &proto.FirecrackerRootDrive{
					HostPath: rootfsPath,
				},
				NetworkInterfaces: []*proto.FirecrackerNetworkInterface{
					{
						AllowMMDS: true,
						StaticConfig: &proto.StaticNetworkConfiguration{
							HostDevName: tapName,
							MacAddress:  vmIDtoMacAddr(uint(vmID)),
						},
					},
				},
				ContainerCount: containerCount,
				JailerConfig:   jailerConfig,
				TimeoutSeconds: uint32(createVMTimeout / time.Second),
				// In tests, our in-VM agent has Go's race detector,
				// which makes the agent resource-hoggy than its production build
				// So the default VM size (128MB) is too small.
				MachineCfg: &proto.FirecrackerMachineConfiguration{MemSizeMib: 1024},
			}

			fcClient, err := newFCControlClient(containerdSockPath)
			if err != nil {
				return err
			}

			resp, createVMErr := fcClient.CreateVM(ctx, req)
			if createVMErr != nil {
				matches, err := findProcess(ctx, findFirecracker)
				if err != nil {
					return fmt.Errorf(
						"failed to create a VM and couldn't find Firecracker due to %s: %w",
						createVMErr, err,
					)
				}
				return fmt.Errorf(
					"failed to create a VM while there are %d Firecracker processes: %w",
					len(matches),
					createVMErr,
				)
			}

			containerEg, containerCtx := errgroup.WithContext(vmEgCtx)
			for containerID := 0; containerID < int(containerCount); containerID++ {
				containerID := containerID
				containerEg.Go(func() error {
					return testMultipleExecs(
						containerCtx,
						vmID,
						containerID,
						client, image,
						jailerConfig,
						resp.CgroupPath,
					)
				})
			}

			// verify duplicate CreateVM call fails with right error
			_, err = fcClient.CreateVM(ctx, &proto.CreateVMRequest{VMID: strconv.Itoa(vmID)})
			if err == nil {
				return fmt.Errorf("creating the same VM must return an error")
			}

			// verify GetVMInfo returns expected data
			vmInfoResp, err := fcClient.GetVMInfo(ctx, &proto.GetVMInfoRequest{VMID: strconv.Itoa(vmID)})
			if err != nil {
				return err
			}
			if vmInfoResp.VMID != strconv.Itoa(vmID) {
				return fmt.Errorf("%q must be %q", vmInfoResp.VMID, strconv.Itoa(vmID))
			}

			nspVMid := defaultNamespace + "#" + strconv.Itoa(vmID)
			cfg, err := config.LoadConfig("")
			if err != nil {
				return err
			}
			if vmInfoResp.SocketPath != filepath.Join(cfg.ShimBaseDir, nspVMid, "firecracker.sock") ||
				vmInfoResp.VSockPath != filepath.Join(cfg.ShimBaseDir, nspVMid, "firecracker.vsock") ||
				vmInfoResp.LogFifoPath != filepath.Join(cfg.ShimBaseDir, nspVMid, "fc-logs.fifo") ||
				vmInfoResp.MetricsFifoPath != filepath.Join(cfg.ShimBaseDir, nspVMid, "fc-metrics.fifo") ||
				resp.CgroupPath != vmInfoResp.CgroupPath {
				return fmt.Errorf("unexpected result from GetVMInfo: %+v", vmInfoResp)
			}

			// just verify that updating the metadata doesn't return an error, a separate test case is needed
			// to very the MMDS update propagates to the container correctly
			_, err = fcClient.SetVMMetadata(ctx, &proto.SetVMMetadataRequest{
				VMID:     strconv.Itoa(vmID),
				Metadata: "{}",
			})
			if err != nil {
				return err
			}

			err = containerEg.Wait()
			if err != nil {
				return fmt.Errorf("unexpected error from the containers in VM %d: %w", vmID, err)
			}

			_, err = fcClient.StopVM(ctx, &proto.StopVMRequest{VMID: strconv.Itoa(vmID), TimeoutSeconds: 5})
			if err != nil {
				return err
			}
			return nil
		}

		vmEg.Go(func() error {
			err := f(vmEgCtx)
			if err != nil {
				return fmt.Errorf("unexpected errors from VM %d: %w", vmID, err)
			}
			return nil
		})
	}

	err = vmEg.Wait()
	require.NoError(t, err)
}

func testMultipleExecs(
	ctx context.Context,
	vmID int,
	containerID int,
	client *containerd.Client,
	image containerd.Image,
	jailerConfig *proto.JailerConfig,
	cgroupPath string,
) error {
	vmIDStr := strconv.Itoa(vmID)
	testTimeout := 600 * time.Second

	containerName := fmt.Sprintf("container-%d-%d", vmID, containerID)
	snapshotName := fmt.Sprintf("snapshot-%d-%d", vmID, containerID)
	processArgs := oci.WithProcessArgs("/bin/sh", "-c", strings.Join([]string{
		fmt.Sprintf("/bin/cat /sys/class/net/%s/address", defaultVMNetDevName),
		"/usr/bin/readlink /proc/self/ns/mnt",
		fmt.Sprintf("/bin/sleep %d", testTimeout/time.Second),
	}, " && "))

	// spawn a container that just prints the VM's eth0 mac address (which we have set uniquely per VM)
	newContainer, err := client.NewContainer(ctx,
		containerName,
		containerd.WithSnapshotter(defaultSnapshotterName),
		containerd.WithNewSnapshot(snapshotName, image),
		containerd.WithNewSpec(
			processArgs,
			oci.WithHostNamespace(specs.NetworkNamespace),
			firecrackeroci.WithVMID(vmIDStr),
		),
	)
	if err != nil {
		return err
	}
	defer newContainer.Delete(ctx)

	var taskStdout bytes.Buffer
	var taskStderr bytes.Buffer

	newTask, err := newContainer.NewTask(ctx,
		cio.NewCreator(cio.WithStreams(nil, &taskStdout, &taskStderr)))
	if err != nil {
		return err
	}

	taskExitCh, err := newTask.Wait(ctx)
	if err != nil {
		return err
	}

	err = newTask.Start(ctx)
	if err != nil {
		return err
	}

	// Create a few execs for the task, including one with the same ID as the taskID (to provide
	// regression coverage for a bug related to using the same task and exec ID).
	//
	// Save each of their stdout buffers, which will later be compared to ensure they each have
	// the same output.
	//
	// The output of the exec is the mount namespace in which it found itself executing. This
	// will be compared with the mount namespace the task is executing to ensure they are the same.
	// This is a rudimentary way of asserting that each exec was created in the expected task.
	execIDs := []string{fmt.Sprintf("exec-%d-%d", vmID, containerID), containerName}
	execStdouts := make(chan string, len(execIDs))
	var eg, _ = errgroup.WithContext(ctx)
	for _, execID := range execIDs {
		execID := execID
		eg.Go(func() error {
			ns, err := getMountNamespace(ctx, client, containerName, newTask, execID)
			if err != nil {
				return err
			}
			execStdouts <- ns
			return nil
		})
	}
	err = eg.Wait()
	if err != nil {
		return fmt.Errorf("unexpected error from the execs in container %d: %w", containerID, err)
	}
	close(execStdouts)

	if jailerConfig != nil {
		dir, err := vm.ShimDir(shimBaseDir(), "default", vmIDStr)
		if err != nil {
			return err
		}

		jailer := &runcJailer{
			Config: runcJailerConfig{
				OCIBundlePath: dir.RootPath(),
			},
			vmID: vmIDStr,
		}
		_, err = os.Stat(jailer.RootPath())
		if err != nil {
			return err
		}
		_, err = os.Stat(filepath.Join("/sys/fs/cgroup/cpu", cgroupPath))
		if err != nil {
			return err
		}

		ok, err := regexp.Match(".+/"+vmIDStr, []byte(cgroupPath))
		if err != nil {
			return err
		}
		if !ok {
			return fmt.Errorf("%q doesn't match %q", cgroupPath, vmIDStr)
		}
	}

	// Verify each exec had the same stdout and use that value as the mount namespace that will be compared
	// against that of the task below.
	var execMntNS string
	for execStdout := range execStdouts {
		if execMntNS == "" {
			// This is the first iteration of loop; we do a check that execStdout is not "" via require.NotEmptyf
			// in the execID loop above.
			execMntNS = execStdout
		}
		if execStdout != execMntNS {
			return fmt.Errorf("%q must be %q", execStdout, execMntNS)
		}
	}

	// Now kill the task and verify it was in the right VM and has the same mnt namespace as its execs
	err = newTask.Kill(ctx, syscall.SIGKILL)
	if err != nil {
		return err
	}

	select {
	case <-taskExitCh:
		_, err = newTask.Delete(ctx)
		if err != nil {
			return err
		}

		// if there was anything on stderr, print it to assist debugging
		stderrOutput := taskStderr.String()
		if len(stderrOutput) != 0 {
			fmt.Printf("stderr output from task %q: %q", containerName, stderrOutput)
		}

		stdout := taskStdout.String()
		expected := fmt.Sprintf("%s\n%s\n", vmIDtoMacAddr(uint(vmID)), execMntNS)

		if stdout != expected {
			return fmt.Errorf("%q must be %q", stdout, expected)
		}
	case <-ctx.Done():
		return ctx.Err()
	}

	return nil
}

func getMountNamespace(ctx context.Context, client *containerd.Client, containerName string, newTask containerd.Task, execID string) (string, error) {
	var execStdout bytes.Buffer
	var execStderr bytes.Buffer

	newExec, err := newTask.Exec(ctx, execID, &specs.Process{
		Args: []string{"/usr/bin/readlink", "/proc/self/ns/mnt"},
		Cwd:  "/",
	}, cio.NewCreator(cio.WithStreams(nil, &execStdout, &execStderr)))
	if err != nil {
		return "", err
	}

	execExitCh, err := newExec.Wait(ctx)
	if err != nil {
		return "", err
	}

	err = newExec.Start(ctx)
	if err != nil {
		return "", err
	}

	select {
	case exitStatus := <-execExitCh:
		_, err = newExec.Delete(ctx)
		if err != nil {
			return "", err
		}

		// if there was anything on stderr, print it to assist debugging
		stderrOutput := execStderr.String()
		if len(stderrOutput) != 0 {
			fmt.Printf("stderr output from exec %q: %q", execID, stderrOutput)
		}

		mntNS := strings.TrimSpace(execStdout.String())
		code := exitStatus.ExitCode()
		if code != 0 {
			return "", fmt.Errorf("exit code %d != 0, stdout=%q stderr=%q", code, execStdout.String(), stderrOutput)
		}

		return mntNS, nil
	case <-ctx.Done():
		return "", ctx.Err()
	}
}

func TestLongUnixSocketPath_Isolated(t *testing.T) {
	prepareIntegTest(t)

	cfg, err := config.LoadConfig("")
	require.NoError(t, err, "failed to load config")

	// Verify that if the absolute path of the Firecracker unix sockets are longer
	// than the max length enforced by the kernel (UNIX_PATH_MAX, usually 108), we
	// don't fail (due to the internal implementation using relative paths).
	// We do this by using the max VMID len (64 chars), which in combination with the
	// default location we store state results in a path like
	// "/run/firecracker-containerd/<namespace>/<vmID>" (with len 112).
	const maxUnixSockLen = 108
	namespace := strings.Repeat("n", 20)
	vmID := strings.Repeat("v", 64)

	ctx := namespaces.WithNamespace(context.Background(), namespace)

	fcClient, err := newFCControlClient(containerdSockPath)
	require.NoError(t, err, "failed to create fccontrol client")

	subtests := []struct {
		name    string
		request proto.CreateVMRequest
	}{
		{
			name: "Without Jailer",
			request: proto.CreateVMRequest{
				VMID:              vmID,
				NetworkInterfaces: []*proto.FirecrackerNetworkInterface{},
			},
		},
		{
			name: "With Jailer",
			request: proto.CreateVMRequest{
				VMID:              vmID,
				NetworkInterfaces: []*proto.FirecrackerNetworkInterface{},
				JailerConfig: &proto.JailerConfig{
					UID: 30000,
					GID: 30000,
				},
			},
		},
	}

	for _, subtest := range subtests {
		request := subtest.request
		vmID := request.VMID
		t.Run(subtest.name, func(t *testing.T) {
			_, err = fcClient.CreateVM(ctx, &request)
			require.NoError(t, err, "failed to create VM")

			// double-check that the sockets are at the expected path and that their absolute
			// length exceeds 108 bytes
			shimDir, err := vm.ShimDir(cfg.ShimBaseDir, namespace, vmID)
			require.NoError(t, err, "failed to get shim dir")

			if request.JailerConfig == nil {
				_, err = os.Stat(shimDir.FirecrackerSockPath())
				require.NoError(t, err, "failed to stat firecracker socket path")
				if len(shimDir.FirecrackerSockPath()) <= maxUnixSockLen {
					assert.Failf(t, "firecracker sock absolute path %q is not greater than max unix socket path length", shimDir.FirecrackerSockPath())
				}

				_, err = os.Stat(shimDir.FirecrackerVSockPath())
				require.NoError(t, err, "failed to stat firecracker vsock path")
				if len(shimDir.FirecrackerVSockPath()) <= maxUnixSockLen {
					assert.Failf(t, "firecracker vsock absolute path %q is not greater than max unix socket path length", shimDir.FirecrackerVSockPath())
				}
			}

			_, err = fcClient.StopVM(ctx, &proto.StopVMRequest{VMID: vmID})
			require.NoError(t, err)

			matches, err := findProcess(ctx, findShim)
			require.NoError(t, err)
			require.Empty(t, matches)

			matches, err = findProcess(ctx, findFirecracker)
			require.NoError(t, err)
			require.Empty(t, matches)
		})
	}
}

func allowDeviceAccess(_ context.Context, _ oci.Client, _ *containers.Container, s *oci.Spec) error {
	// By default, all devices accesses are forbidden.
	s.Linux.Resources.Devices = append(
		s.Linux.Resources.Devices,
		specs.LinuxDeviceCgroup{Allow: true, Access: "r"},
	)

	// Exposes the host kernel's /dev as /dev.
	// By default, runc creates own /dev with a minimal set of pseudo devices such as /dev/null.
	s.Mounts = append(s.Mounts, specs.Mount{
		Type:        "bind",
		Options:     []string{"bind"},
		Destination: "/dev",
		Source:      "/dev",
	})
	return nil
}

func TestStubBlockDevices_Isolated(t *testing.T) {
	prepareIntegTest(t)

	const vmID = 0

	ctx := namespaces.WithNamespace(context.Background(), "default")

	client, err := containerd.New(containerdSockPath, containerd.WithDefaultRuntime(firecrackerRuntime))
	require.NoError(t, err, "unable to create client to containerd service at %s, is containerd running?", containerdSockPath)
	defer client.Close()

	image, err := alpineImage(ctx, client, defaultSnapshotterName)
	require.NoError(t, err, "failed to get alpine image")

	tapName := fmt.Sprintf("tap%d", vmID)
	err = createTapDevice(ctx, tapName)
	require.NoError(t, err, "failed to create tap device for vm %d", vmID)

	containerName := fmt.Sprintf("%s-%d", t.Name(), time.Now().UnixNano())
	snapshotName := fmt.Sprintf("%s-snapshot", containerName)

	fcClient, err := newFCControlClient(containerdSockPath)
	require.NoError(t, err, "failed to create fccontrol client")

	_, err = fcClient.CreateVM(ctx, &proto.CreateVMRequest{
		VMID: strconv.Itoa(vmID),
		NetworkInterfaces: []*proto.FirecrackerNetworkInterface{
			{
				AllowMMDS: true,
				StaticConfig: &proto.StaticNetworkConfiguration{
					HostDevName: tapName,
					MacAddress:  vmIDtoMacAddr(uint(vmID)),
				},
			},
		},
		ContainerCount: 5,
	})
	require.NoError(t, err, "failed to create VM")

	newContainer, err := client.NewContainer(ctx,
		containerName,
		containerd.WithSnapshotter(defaultSnapshotterName),
		containerd.WithNewSnapshot(snapshotName, image),
		containerd.WithNewSpec(
			firecrackeroci.WithVMID(strconv.Itoa(vmID)),
			oci.WithProcessArgs("/bin/sh", "/var/firecracker-containerd-test/scripts/lsblk.sh"),

			oci.WithMounts([]specs.Mount{
				// Exposes test scripts from the host kernel
				{
					Type:        "bind",
					Options:     []string{"bind"},
					Destination: "/var/firecracker-containerd-test/scripts",
					Source:      "/var/firecracker-containerd-test/scripts",
				},
			}),
			allowDeviceAccess,
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
		_, err = newTask.Delete(ctx)
		require.NoError(t, err)

		// if there was anything on stderr, print it to assist debugging
		stderrOutput := stderr.String()
		if len(stderrOutput) != 0 {
			fmt.Printf("stderr output from vm %d, container %d: %s", vmID, containerID, stderrOutput)
		}

		const expectedOutput = `
vdb  254:16   0 1073741824B  0 |    0   0   0   0   0   0   0   0
vdc  254:32   0        512B  0 |  214 244 216 245 215 177 177 177
vdd  254:48   0        512B  0 |  214 244 216 245 215 177 177 177
vde  254:64   0        512B  0 |  214 244 216 245 215 177 177 177
vdf  254:80   0        512B  0 |  214 244 216 245 215 177 177 177`

		parts := strings.Split(stdout.String(), "vdb")
		require.Equal(t, strings.TrimSpace(expectedOutput), strings.TrimSpace("vdb"+parts[1]))
		require.NoError(t, exitStatus.Error(), "failed to retrieve exitStatus")
		require.Equal(t, uint32(0), exitStatus.ExitCode())
	case <-ctx.Done():
		require.Fail(t, "context cancelled",
			"context cancelled while waiting for container %s to exit, err: %v", containerName, ctx.Err())
	}
}

func startAndWaitTask(ctx context.Context, t *testing.T, c containerd.Container) string {
	var stdout bytes.Buffer
	var stderr bytes.Buffer

	task, err := c.NewTask(ctx, cio.NewCreator(cio.WithStreams(nil, &stdout, &stderr)))
	require.NoError(t, err, "failed to create task for container %s", c.ID())

	exitCh, err := task.Wait(ctx)
	require.NoError(t, err, "failed to wait on task for container %s", c.ID())

	err = task.Start(ctx)
	require.NoError(t, err, "failed to start task for container %s", c.ID())
	defer func() {
		require.NoError(t, err, "failed to delete task for container %s", c.ID())
	}()

	select {
	case exitStatus := <-exitCh:
		assert.NoError(t, exitStatus.Error(), "failed to retrieve exitStatus")
		assert.Equal(t, uint32(0), exitStatus.ExitCode())

		status, err := task.Delete(ctx)
		assert.NoErrorf(t, err, "failed to delete task %q after exit", c.ID())
		if status != nil {
			assert.NoError(t, status.Error())
		}

		assert.Equal(t, "", stderr.String())
	case <-ctx.Done():
		require.Fail(t, "context cancelled",
			"context cancelled while waiting for container %s to exit, err: %v", c.ID(), ctx.Err())
	}

	return stdout.String()
}

func testCreateContainerWithSameName(t *testing.T, vmID string) {
	ctx := namespaces.WithNamespace(context.Background(), "default")

	// Explicitly specify Container Count = 2 to workaround #230
	if len(vmID) != 0 {
		fcClient, err := newFCControlClient(containerdSockPath)
		require.NoError(t, err, "failed to create fccontrol client")

		_, err = fcClient.CreateVM(ctx, &proto.CreateVMRequest{
			VMID:           vmID,
			ContainerCount: 2,
		})
		require.NoError(t, err)
	}

	withNewSpec := containerd.WithNewSpec(oci.WithProcessArgs("echo", "hello"), firecrackeroci.WithVMID(vmID), oci.WithDefaultPathEnv)

	client, err := containerd.New(containerdSockPath, containerd.WithDefaultRuntime(firecrackerRuntime))
	require.NoError(t, err, "unable to create client to containerd service at %s, is containerd running?", containerdSockPath)
	defer client.Close()

	image, err := alpineImage(ctx, client, defaultSnapshotterName)
	require.NoError(t, err, "failed to get alpine image")

	containerName := fmt.Sprintf("%s-%d", t.Name(), time.Now().UnixNano())
	snapshotName := fmt.Sprintf("%s-snapshot", containerName)

	containerPath := fmt.Sprintf("/run/containerd/io.containerd.runtime.v2.task/default/%s", containerName)

	c1, err := client.NewContainer(ctx,
		containerName,
		containerd.WithSnapshotter(defaultSnapshotterName),
		containerd.WithNewSnapshot(snapshotName, image),
		withNewSpec,
	)
	require.NoError(t, err, "failed to create container %s", containerName)
	require.Equal(t, "hello\n", startAndWaitTask(ctx, t, c1))

	// All resources regarding the container will be deleted
	err = c1.Delete(ctx, containerd.WithSnapshotCleanup)
	require.NoError(t, err, "failed to delete container %s", containerName)

	_, err = os.Stat(containerPath)
	require.True(t, os.IsNotExist(err))

	cfg, err := config.LoadConfig("")
	require.NoError(t, err, "failed to load config")

	if len(vmID) != 0 {
		shimPath := fmt.Sprintf("%s/default#%s/%s/%s", cfg.ShimBaseDir, vmID, vmID, containerName)
		_, err = os.Stat(shimPath)
		require.True(t, os.IsNotExist(err))
	}

	// So, we can launch a new container with the same name
	c2, err := client.NewContainer(ctx,
		containerName,
		containerd.WithSnapshotter(defaultSnapshotterName),
		containerd.WithNewSnapshot(snapshotName, image),
		withNewSpec,
	)
	require.NoError(t, err, "failed to create container %s", containerName)
	require.Equal(t, "hello\n", startAndWaitTask(ctx, t, c2))

	err = c2.Delete(ctx, containerd.WithSnapshotCleanup)
	require.NoError(t, err, "failed to delete container %s", containerName)

	_, err = os.Stat(containerPath)
	require.True(t, os.IsNotExist(err))

	if len(vmID) != 0 {
		shimPath := fmt.Sprintf("%s/default#%s/%s/%s", cfg.ShimBaseDir, vmID, vmID, containerName)
		_, err = os.Stat(shimPath)
		require.True(t, os.IsNotExist(err))
	}
}

func TestCreateContainerWithSameName_Isolated(t *testing.T) {
	prepareIntegTest(t)

	testCreateContainerWithSameName(t, "")

	vmID := fmt.Sprintf("same-vm-%d", time.Now().UnixNano())
	testCreateContainerWithSameName(t, vmID)
}

func TestStubDriveReserveAndReleaseByContainers_Isolated(t *testing.T) {
	prepareIntegTest(t)

	assert := assert.New(t)

	ctx := namespaces.WithNamespace(context.Background(), "default")

	client, err := containerd.New(containerdSockPath, containerd.WithDefaultRuntime(firecrackerRuntime))
	require.NoError(t, err, "unable to create client to containerd service at %s, is containerd running?", containerdSockPath)
	defer client.Close()

	image, err := alpineImage(ctx, client, defaultSnapshotterName)
	require.NoError(t, err, "failed to get alpine image")

	runEchoHello := containerd.WithNewSpec(oci.WithProcessArgs("echo", "-n", "hello"), firecrackeroci.WithVMID("reuse-same-vm"), oci.WithDefaultPathEnv)

	c1, err := client.NewContainer(ctx,
		"c1",
		containerd.WithSnapshotter(defaultSnapshotterName),
		containerd.WithNewSnapshot("c1", image),
		runEchoHello,
	)
	assert.Equal("hello", startAndWaitTask(ctx, t, c1))
	require.NoError(t, err, "failed to create a container")

	defer func() {
		err = c1.Delete(ctx, containerd.WithSnapshotCleanup)
		require.NoError(t, err, "failed to delete a container")
	}()

	c2, err := client.NewContainer(ctx,
		"c2",
		containerd.WithSnapshotter(defaultSnapshotterName),
		containerd.WithNewSnapshot("c2", image),
		runEchoHello,
	)
	require.NoError(t, err, "failed to create a container")

	defer func() {
		err := c2.Delete(ctx, containerd.WithSnapshotCleanup)
		require.NoError(t, err, "failed to delete a container")
	}()

	// With the new behaviour, on previous task deletion, stub drive will be released
	// and now can be reused by new container and task.
	assert.Equal("hello", startAndWaitTask(ctx, t, c2))
}

func TestDriveMount_Isolated(t *testing.T) {
	prepareIntegTest(t, func(cfg *config.Config) {
		cfg.JailerConfig.RuncBinaryPath = "/usr/local/bin/runc"
	})

	testTimeout := 120 * time.Second
	ctx, cancel := context.WithTimeout(namespaces.WithNamespace(context.Background(), defaultNamespace), testTimeout)
	defer cancel()

	ctrdClient, err := containerd.New(containerdSockPath, containerd.WithDefaultRuntime(firecrackerRuntime))
	require.NoError(t, err, "unable to create client to containerd service at %s, is containerd running?", containerdSockPath)

	fcClient, err := newFCControlClient(containerdSockPath)
	require.NoError(t, err, "failed to create fccontrol client")

	image, err := alpineImage(ctx, ctrdClient, defaultSnapshotterName)
	require.NoError(t, err, "failed to get alpine image")

	vmID := "test-drive-mount"

	vmMounts := []struct {
		VMPath         string
		FilesystemType string
		VMMountOptions []string
		ContainerPath  string
		FSImgFile      internal.FSImgFile
		IsWritable     bool
		RateLimiter    *proto.FirecrackerRateLimiter
		CacheType      string
	}{
		{
			// /systemmount meant to make sure logic doesn't ban this just because it begins with /sys
			VMPath:         "/systemmount",
			FilesystemType: "ext4",
			VMMountOptions: []string{"rw", "noatime"},
			ContainerPath:  "/foo",
			FSImgFile: internal.FSImgFile{
				Subpath:  "dir/foo",
				Contents: "foo\n",
			},
			RateLimiter: &proto.FirecrackerRateLimiter{
				Bandwidth: &proto.FirecrackerTokenBucket{
					OneTimeBurst: 111,
					RefillTime:   222,
					Capacity:     333,
				},
				Ops: &proto.FirecrackerTokenBucket{
					OneTimeBurst: 1111,
					RefillTime:   2222,
					Capacity:     3333,
				},
			},
			IsWritable: true,
			CacheType:  "Writeback",
		},
		{
			VMPath:         "/mnt",
			FilesystemType: "ext4",
			// don't specify "ro" to validate it's automatically set via "IsWritable: false"
			VMMountOptions: []string{"relatime"},
			ContainerPath:  "/bar",
			FSImgFile: internal.FSImgFile{
				Subpath:  "dir/bar",
				Contents: "bar\n",
			},
			// you actually get permission denied if you try to mount a ReadOnly block device
			// w/ "rw" mount option, so we can only test IsWritable=false when "ro" is also the
			// mount option, not in isolation
			IsWritable: false,
		},
	}

	vmDriveMounts := []*proto.FirecrackerDriveMount{}
	ctrBindMounts := []specs.Mount{}
	ctrCommands := []string{}
	for _, vmMount := range vmMounts {
		vmDriveMounts = append(vmDriveMounts, &proto.FirecrackerDriveMount{
			HostPath:       internal.CreateFSImg(ctx, t, vmMount.FilesystemType, vmMount.FSImgFile),
			VMPath:         vmMount.VMPath,
			FilesystemType: vmMount.FilesystemType,
			Options:        vmMount.VMMountOptions,
			IsWritable:     vmMount.IsWritable,
			RateLimiter:    vmMount.RateLimiter,
			CacheType:      vmMount.CacheType,
		})

		ctrBindMounts = append(ctrBindMounts, specs.Mount{
			Source:      vmMount.VMPath,
			Destination: vmMount.ContainerPath,
			Options:     []string{"bind"},
		})

		ctrCommands = append(ctrCommands, fmt.Sprintf("/bin/cat %s",
			filepath.Join(vmMount.ContainerPath, vmMount.FSImgFile.Subpath),
		))

		if !vmMount.IsWritable {
			// if read-only is set on the firecracker drive, make sure that you are unable
			// to create a new file
			ctrCommands = append(ctrCommands, fmt.Sprintf(`/bin/sh -c '/bin/touch %s 2>/dev/null && exit 1 || exit 0'`,
				filepath.Join(vmMount.ContainerPath, vmMount.FSImgFile.Subpath+"noexist"),
			))
		}

		// RateLimiter settings are not asserted on in this test right now as there's not a clear simple
		// way to test them. Coverage that RateLimiter settings are passed as expected are covered in unit tests
	}

	_, err = fcClient.CreateVM(ctx, &proto.CreateVMRequest{
		VMID:        vmID,
		DriveMounts: vmDriveMounts,
		JailerConfig: &proto.JailerConfig{
			UID: 300000,
			GID: 300000,
		},
		TimeoutSeconds: 60,
	})
	require.NoError(t, err, "failed to create vm")

	containerName := fmt.Sprintf("%s-container", vmID)
	snapshotName := fmt.Sprintf("%s-snapshot", vmID)

	newContainer, err := ctrdClient.NewContainer(ctx,
		containerName,
		containerd.WithSnapshotter(defaultSnapshotterName),
		containerd.WithNewSnapshot(snapshotName, image),
		containerd.WithNewSpec(
			oci.WithProcessArgs("/bin/sh", "-c", strings.Join(append(ctrCommands,
				"/bin/cat /proc/mounts",
			), " && ")),
			oci.WithMounts(ctrBindMounts),
			firecrackeroci.WithVMID(vmID),
		),
	)
	require.NoError(t, err, "failed to create container %s", containerName)

	outputLines := strings.Split(startAndWaitTask(ctx, t, newContainer), "\n")
	if len(outputLines) < len(vmMounts) {
		require.Fail(t, "unexpected ctr output", "expected at least %d lines: %+v", len(vmMounts), outputLines)
	}

	mountInfos, err := internal.ParseProcMountLines(outputLines[len(vmMounts):]...)
	require.NoError(t, err, "failed to parse /proc/mount")
	// this is n^2, but it's doubtful the number of mounts will reach a point where that matters...
	for _, vmMount := range vmMounts {
		// Make sure that this vmMount's test file was cat'd by a container previously and output the expected
		// file contents. This ensure the filesystem was successfully mounted in the VM and the container.
		assert.Containsf(t, outputLines[:len(vmMounts)], strings.TrimSpace(vmMount.FSImgFile.Contents),
			"did not find expected test file output for vm mount at %q", vmMount.ContainerPath)

		// iterate over /proc/mounts entries, find this vmMount's entry in there and verify it was mounted
		// with the correct options.
		var foundExpectedMount bool
		for _, actualMountInfo := range mountInfos {
			if actualMountInfo.DestPath == vmMount.ContainerPath {
				foundExpectedMount = true
				assert.Equalf(t, vmMount.FilesystemType, actualMountInfo.Type,
					"vm mount at %q did have expected filesystem type", vmMount.ContainerPath)
				for _, vmMountOption := range vmMount.VMMountOptions {
					assert.Containsf(t, actualMountInfo.Options, vmMountOption,
						"vm mount at %q did not have expected option", vmMount.ContainerPath)
				}
				if !vmMount.IsWritable {
					assert.Containsf(t, actualMountInfo.Options, "ro",
						`vm mount at %q with IsWritable=false did not have "ro" option`, vmMount.ContainerPath)
				} else {
					assert.Containsf(t, actualMountInfo.Options, "rw",
						`vm mount at %q with IsWritable=false did not have "rw" option`, vmMount.ContainerPath)
				}
				break
			}
		}
		assert.Truef(t, foundExpectedMount, "did not find expected mount at container path %q", vmMount.ContainerPath)
	}
}

func TestDriveMountFails_Isolated(t *testing.T) {
	prepareIntegTest(t)

	testTimeout := 120 * time.Second
	ctx, cancel := context.WithTimeout(namespaces.WithNamespace(context.Background(), defaultNamespace), testTimeout)
	defer cancel()

	fcClient, err := newFCControlClient(containerdSockPath)
	require.NoError(t, err, "failed to create fccontrol client")

	testImgHostPath := internal.CreateFSImg(ctx, t, "ext4", internal.FSImgFile{
		Subpath:  "idc",
		Contents: "doesn't matter",
	})

	for _, driveMount := range []*proto.FirecrackerDriveMount{
		{
			HostPath:       testImgHostPath,
			VMPath:         "/proc/foo", // invalid due to being under /proc
			FilesystemType: "ext4",
		},
		{
			HostPath:       testImgHostPath,
			VMPath:         "/dev/foo", // invalid due to being under /dev
			FilesystemType: "ext4",
		},
		{
			HostPath:       testImgHostPath,
			VMPath:         "/sys/foo", // invalid due to being under /sys
			FilesystemType: "ext4",
		},
		{
			HostPath:       testImgHostPath,
			VMPath:         "/valid",
			FilesystemType: "ext4",
			// invalid due to "ro" option used with IsWritable=true
			Options:    []string{"ro"},
			IsWritable: true,
		},
		{
			HostPath:       testImgHostPath,
			VMPath:         "/valid",
			FilesystemType: "ext4",
			// invalid due to "rw" option used with IsWritable=false
			Options: []string{"rw"},
		},
		{
			HostPath:       testImgHostPath,
			VMPath:         "/valid",
			FilesystemType: "ext4",
			// invalid since cacheType expects either "Unsafe" or "Writeback"
			CacheType: "invalid-cache-type",
		},
	} {
		_, err = fcClient.CreateVM(ctx, &proto.CreateVMRequest{
			VMID:        "test-drive-mount-fails",
			DriveMounts: []*proto.FirecrackerDriveMount{driveMount},
		})

		// TODO it would be good to check for more specific error types, see #294 for possible improvements:
		// https://github.com/firecracker-microvm/firecracker-containerd/issues/294
		assert.Error(t, err, "unexpectedly succeeded in creating a VM with an invalid drive mount")
	}
}

func TestUpdateVMMetadata_Isolated(t *testing.T) {
	prepareIntegTest(t)

	testTimeout := 60 * time.Second
	ctx, cancel := context.WithTimeout(namespaces.WithNamespace(context.Background(), defaultNamespace), testTimeout)
	defer cancel()

	client, err := containerd.New(containerdSockPath, containerd.WithDefaultRuntime(firecrackerRuntime))
	require.NoError(t, err, "unable to create client to containerd service at %s, is containerd running?", containerdSockPath)
	defer client.Close()

	fcClient, err := newFCControlClient(containerdSockPath)
	require.NoError(t, err, "failed to create fccontrol client")

	cniNetworkName := "fcnet-test"
	err = writeCNIConf("/etc/cni/conf.d/fcnet-test.conflist",
		"tc-redirect-tap", cniNetworkName, "")
	require.NoError(t, err, "failed to write test cni conf")

	_, err = fcClient.CreateVM(ctx, &proto.CreateVMRequest{
		VMID: "1",
		NetworkInterfaces: []*proto.FirecrackerNetworkInterface{{
			AllowMMDS: true,
			CNIConfig: &proto.CNIConfiguration{
				NetworkName:   cniNetworkName,
				InterfaceName: "veth0",
			},
		}},
		ContainerCount: 2,
	})
	require.NoError(t, err)
	metadata := "{\"thing\":\"42\",\"ThreeThing\":\"wow\"}"
	// Update VMM metadata
	_, err = fcClient.SetVMMetadata(ctx, &proto.SetVMMetadataRequest{
		VMID:     "1",
		Metadata: metadata,
	})
	require.NoError(t, err)
	resp, err := fcClient.GetVMMetadata(ctx, &proto.GetVMMetadataRequest{
		VMID: "1",
	})
	require.NoError(t, err)
	expected := "{\"ThreeThing\":\"wow\",\"thing\":\"42\"}"
	assert.Equal(t, expected, resp.Metadata)
	// Update again to ensure patching works
	_, err = fcClient.UpdateVMMetadata(ctx, &proto.UpdateVMMetadataRequest{
		VMID:     "1",
		Metadata: "{\"TwoThing\":\"6*9\",\"thing\":\"45\"}",
	})
	require.NoError(t, err)

	resp, err = fcClient.GetVMMetadata(ctx, &proto.GetVMMetadataRequest{
		VMID: "1",
	})
	require.NoError(t, err)
	expected = "{\"ThreeThing\":\"wow\",\"TwoThing\":\"6*9\",\"thing\":\"45\"}"
	assert.Equal(t, expected, resp.Metadata)

	// Check inside the vm
	image, err := alpineImage(ctx, client, defaultSnapshotterName)
	require.NoError(t, err, "failed to get alpine image")
	containerName := "mmds-test"

	newContainer, err := client.NewContainer(ctx,
		containerName,
		containerd.WithSnapshotter(defaultSnapshotterName),
		containerd.WithNewSnapshot("mmds-test-all", image),
		containerd.WithNewSpec(
			oci.WithProcessArgs("/usr/bin/wget",
				"-q",      // don't print to stderr unless an error occurs
				"-O", "-", // write to stdout
				"http://169.254.169.254/"),
			firecrackeroci.WithVMID("1"),
			firecrackeroci.WithVMNetwork,
		),
	)
	require.NoError(t, err, "failed to create container %s", containerName)

	stdout := startAndWaitTask(ctx, t, newContainer)
	t.Logf("stdout output from task %q: %s", containerName, stdout)
	assert.Equalf(t, "ThreeThing\nTwoThing\nthing", stdout, "container %q did not emit expected stdout", containerName)
	// check a single entry
	containerName += "-entry"
	newContainer, err = client.NewContainer(ctx,
		containerName,
		containerd.WithSnapshotter(defaultSnapshotterName),
		containerd.WithNewSnapshot("mmds-test-entry", image),
		containerd.WithNewSpec(
			oci.WithProcessArgs("/usr/bin/wget",
				"-q",      // don't print to stderr unless an error occurs
				"-O", "-", // write to stdout
				"http://169.254.169.254/thing"),
			firecrackeroci.WithVMID("1"),
			firecrackeroci.WithVMNetwork,
		),
	)
	require.NoError(t, err, "failed to create container %s", containerName)
	stdout = startAndWaitTask(ctx, t, newContainer)
	t.Logf("stdout output from task %q: %s", containerName, stdout)
	assert.Equalf(t, "45", stdout, "container %q did not emit expected stdout", containerName)
}

func TestMemoryBalloon_Isolated(t *testing.T) {
	prepareIntegTest(t)
	ctx := namespaces.WithNamespace(context.Background(), defaultNamespace)

	numberOfVms := defaultNumberOfVms
	if str := os.Getenv(numberOfVmsEnvName); str != "" {
		_, err := strconv.Atoi(str)
		require.NoError(t, err, "failed to get NUMBER_OF_VMS env")
	}
	t.Logf("TestMemoryBalloon_Isolated: will run %d vm's", numberOfVms)

	var vmGroup sync.WaitGroup
	for i := 0; i < numberOfVms; i++ {
		vmGroup.Add(1)
		go func(vmID int) {
			defer vmGroup.Done()

			tapName := fmt.Sprintf("tap%d", vmID)
			err := createTapDevice(ctx, tapName)
			defer deleteTapDevice(ctx, tapName)
			require.NoError(t, err, "failed to create tap device for vm %d", vmID)

			fcClient, err := newFCControlClient(containerdSockPath)
			require.NoError(t, err, "failed to create fccontrol client")

			_, err = fcClient.CreateVM(ctx, &proto.CreateVMRequest{
				VMID: strconv.Itoa(vmID),
				MachineCfg: &proto.FirecrackerMachineConfiguration{
					MemSizeMib: 512,
				},
				NetworkInterfaces: []*proto.FirecrackerNetworkInterface{
					{
						AllowMMDS: true,
						StaticConfig: &proto.StaticNetworkConfiguration{
							HostDevName: tapName,
							MacAddress:  vmIDtoMacAddr(uint(vmID)),
						},
					},
				},
				BalloonDevice: &proto.FirecrackerBalloonDevice{
					AmountMib:             defaultBalloonMemory,
					DeflateOnOom:          true,
					StatsPollingIntervals: defaultStatsPollingIntervals,
				},
			})
			require.NoError(t, err, "failed to create vm")

			// Test UpdateBalloon correctly updates amount of memory for the balloon device
			vmIDStr := strconv.Itoa(vmID)
			newAmountMib := int64(50)
			_, err = fcClient.UpdateBalloon(ctx, &proto.UpdateBalloonRequest{
				VMID:      vmIDStr,
				AmountMib: newAmountMib,
			})
			require.NoError(t, err, "failed to update balloon's AmountMib for VM %d", vmID)

			expectedBalloonDevice := &proto.FirecrackerBalloonDevice{
				AmountMib:             newAmountMib,
				DeflateOnOom:          true,
				StatsPollingIntervals: defaultStatsPollingIntervals,
			}

			// Test GetBalloonConfig gets correct configuration for the balloon device
			resp, err := fcClient.GetBalloonConfig(ctx, &proto.GetBalloonConfigRequest{VMID: vmIDStr})
			require.NoError(t, err, "failed to get balloon configuration for VM %d", vmID)
			require.Equal(t, resp.BalloonConfig, expectedBalloonDevice)

			// Test GetBalloonStats gets correct balloon statistics for the balloon device
			balloonStatResp, err := fcClient.GetBalloonStats(ctx, &proto.GetBalloonStatsRequest{VMID: vmIDStr})
			require.NoError(t, err, "failed to get balloon statistics for VM %d", vmID)
			require.Equal(t, newAmountMib, balloonStatResp.TargetMib)

			// Test UpdateBalloonStats correctly updates statistics polling interval for the balloon device
			newStatsPollingIntervals := int64(6)
			_, err = fcClient.UpdateBalloonStats(ctx, &proto.UpdateBalloonStatsRequest{VMID: vmIDStr, StatsPollingIntervals: newStatsPollingIntervals})
			require.NoError(t, err, "failed to update balloon's statistics polling interval for VM %d", vmID)

			balloonConfigResp, err := fcClient.GetBalloonConfig(ctx, &proto.GetBalloonConfigRequest{VMID: vmIDStr})
			require.NoError(t, err, "failed to get balloon configuration for VM %d", vmID)
			require.Equal(t, newStatsPollingIntervals, balloonConfigResp.BalloonConfig.StatsPollingIntervals)
		}(i)
	}
	vmGroup.Wait()
}

func exitCode(err *exec.ExitError) int {
	if status, ok := err.Sys().(syscall.WaitStatus); ok {
		// As like Go 1.12's ExitStatus and WEXITSTATUS()
		// https://github.com/golang/go/blob/3bea90d84107889aaaaa0089f615d7070951a832/src/syscall/syscall_linux.go#L301
		return (int(status) & 0xff00) >> 8
	}
	return -1
}

// TestRandomness validates that there is a reasonable amount of entropy available to the VM and thus
// randomness available to containers (test reads about 2.5MB from /dev/random w/ an overall test
// timeout of 60 seconds). It also validates that the quality of the randomness passes the rngtest
// utility's suite.
func TestRandomness_Isolated(t *testing.T) {
	prepareIntegTest(t)

	ctx, cancel := context.WithTimeout(namespaces.WithNamespace(context.Background(), defaultNamespace), 60*time.Second)
	defer cancel()

	client, err := containerd.New(containerdSockPath, containerd.WithDefaultRuntime(firecrackerRuntime))
	require.NoError(t, err, "unable to create client to containerd service at %s, is containerd running?", containerdSockPath)
	defer client.Close()

	image, err := alpineImage(ctx, client, defaultSnapshotterName)
	require.NoError(t, err, "failed to get alpine image")
	containerName := "test-entropy"

	const blockCount = 1024
	ddContainer, err := client.NewContainer(ctx,
		containerName,
		containerd.WithSnapshotter(defaultSnapshotterName),
		containerd.WithNewSnapshot("test-entropy-snapshot", image),
		containerd.WithNewSpec(
			oci.WithDefaultUnixDevices,
			// Use blocksize of 2500 as rngtest consumes data in blocks of 2500 bytes.
			oci.WithProcessArgs("/bin/dd", "iflag=fullblock", "if=/dev/random", "of=/dev/stdout", "bs=2500",
				fmt.Sprintf("count=%d", blockCount)),
		),
	)
	require.NoError(t, err, "failed to create container %s", containerName)

	// rngtest is a utility to "check the randomness of data using FIPS 140-2 tests", installed as part of
	// the container image this test is running in. We pipe the output from "dd if=/dev/random" to rngtest
	// to validate the quality of the randomness inside the VM.
	// TODO It would be conceptually simpler to just run rngtest inside the container in the VM, but
	// doing so would require some updates to our test infrastructure to support custom-built container
	// images running in VMs (right now it's only feasible to use publicly available container images).
	// Right now, it's instead run as a subprocess of this test outside the VM.
	var rngtestStdout bytes.Buffer
	var rngtestStderr bytes.Buffer
	rngtestCmd := exec.CommandContext(ctx, "rngtest",
		// we set this to 1 less than the number of blocks read by dd above to account for the fact that
		// the first 32 bits read by rngtest are not used for the tests themselves
		fmt.Sprintf("--blockcount=%d", blockCount-1),
	)
	rngtestCmd.Stdout = &rngtestStdout
	rngtestCmd.Stderr = &rngtestStderr
	rngtestStdin, err := rngtestCmd.StdinPipe()
	require.NoError(t, err, "failed to get pipe to rngtest command's stdin")

	ddStdout := rngtestStdin
	var ddStderr bytes.Buffer

	task, err := ddContainer.NewTask(ctx, cio.NewCreator(cio.WithStreams(nil, ddStdout, &ddStderr)))
	require.NoError(t, err, "failed to create task for dd container")

	exitCh, err := task.Wait(ctx)
	require.NoError(t, err, "failed to wait on task for dd container")

	err = task.Start(ctx)
	require.NoError(t, err, "failed to start task for dd container")

	err = rngtestCmd.Start()
	require.NoError(t, err, "failed to start rngtest")

	select {
	case exitStatus := <-exitCh:
		assert.NoError(t, exitStatus.Error(), "failed to retrieve exitStatus")
		assert.EqualValues(t, 0, exitStatus.ExitCode())

		status, err := task.Delete(ctx)
		assert.NoErrorf(t, err, "failed to delete dd task after exit")
		if status != nil {
			assert.NoError(t, status.Error())
		}

		t.Logf("stderr output from dd:\n %s", ddStderr.String())
	case <-ctx.Done():
		require.Fail(t, "context cancelled",
			"context cancelled while waiting for dd container to exit (is it blocked on reading /dev/random?), err: %v", ctx.Err())
	}

	err = rngtestCmd.Wait()
	t.Logf("stdout output from rngtest:\n %s", rngtestStdout.String())
	t.Logf("stderr output from rngtest:\n %s", rngtestStderr.String())
	if err != nil {
		// rngtest will exit non-zero if any blocks fail its randomness tests.
		// Trials showed an approximate false-negative rate of 27/32863 blocks,
		// so testing on 1023 blocks gives a ~36% chance of there being a single
		// false-negative. The chance of there being 5 or more drops down to
		// about 0.1%, which is an acceptable flakiness rate, so we assert
		// that there are no more than 4 failed blocks.
		// Even though we have a failure tolerance, the test still provides some
		// value in that we can be aware if a change to the rootfs results in a
		// regression.
		exitErr, ok := err.(*exec.ExitError)
		require.True(t, ok, "the error is not ExitError")
		require.EqualValues(t, 1, exitCode(exitErr))

		const failureTolerance = 4
		for _, outputLine := range strings.Split(rngtestStderr.String(), "\n") {
			var failureCount int
			_, err := fmt.Sscanf(strings.TrimSpace(outputLine), "rngtest: FIPS 140-2 failures: %d", &failureCount)
			if err == nil {
				if failureCount > failureTolerance {
					require.Failf(t, "too many d block test failures from rngtest",
						"%d failures is greater than tolerance of up to %d failures", failureCount, failureTolerance)
				}
				break
			}
		}
	}
}

func findProcess(
	ctx context.Context,
	matcher func(context.Context, *process.Process) (bool, error),
) ([]*process.Process, error) {
	processes, err := process.ProcessesWithContext(ctx)
	if err != nil {
		return nil, err
	}

	var matches []*process.Process
	for _, p := range processes {
		isMatch, err := matcher(ctx, p)
		if err != nil {
			return nil, err
		}
		if isMatch {
			matches = append(matches, p)
		}
	}

	return matches, nil
}

func pidExists(pid int) bool {
	return syscall.ESRCH.Is(syscall.Kill(pid, 0))
}

func TestStopVM_Isolated(t *testing.T) {
	prepareIntegTest(t)
	require := require.New(t)
	assert := assert.New(t)

	client, err := containerd.New(containerdSockPath, containerd.WithDefaultRuntime(firecrackerRuntime))
	require.NoError(err, "unable to create client to containerd service at %s, is containerd running?", containerdSockPath)
	defer client.Close()

	ctx := namespaces.WithNamespace(context.Background(), "default")

	image, err := alpineImage(ctx, client, defaultSnapshotterName)
	require.NoError(err, "failed to get alpine image")

	tests := []struct {
		name            string
		createVMRequest proto.CreateVMRequest
		stopFunc        func(ctx context.Context, fcClient fccontrol.FirecrackerService, req proto.CreateVMRequest)
		withStopVM      bool
	}{

		{
			name:       "Successful",
			withStopVM: true,

			createVMRequest: proto.CreateVMRequest{},
			stopFunc: func(ctx context.Context, fcClient fccontrol.FirecrackerService, req proto.CreateVMRequest) {
				_, err = fcClient.StopVM(ctx, &proto.StopVMRequest{VMID: req.VMID})
				require.Equal(status.Code(err), codes.OK)
			},
		},

		// Firecracker is too fast to test a case where we hit the timeout on a StopVMRequest.
		// The rootfs below explicitly sleeps 60 seconds after shutting down the agent to simulate the case.
		{
			name:       "Timeout",
			withStopVM: true,

			createVMRequest: proto.CreateVMRequest{
				RootDrive: &proto.FirecrackerRootDrive{
					HostPath: "/var/lib/firecracker-containerd/runtime/rootfs-slow-reboot.img",
				},
			},
			stopFunc: func(ctx context.Context, fcClient fccontrol.FirecrackerService, req proto.CreateVMRequest) {
				_, err = fcClient.StopVM(ctx, &proto.StopVMRequest{VMID: req.VMID})
				require.Error(err)
				assert.Equal(codes.Internal, status.Code(err))
				assert.Contains(err.Error(), "forcefully terminated")
			},
		},

		// Test that the shim shuts down if the VM stops unexpectedly
		{
			name:       "SIGKILLFirecracker",
			withStopVM: false,

			createVMRequest: proto.CreateVMRequest{},
			stopFunc: func(ctx context.Context, _ fccontrol.FirecrackerService, _ proto.CreateVMRequest) {
				firecrackerProcesses, err := findProcess(ctx, findFirecracker)
				require.NoError(err, "failed waiting for expected firecracker process %q to come up", firecrackerProcessName)
				require.Len(firecrackerProcesses, 1, "expected only one firecracker process to exist")
				firecrackerProcess := firecrackerProcesses[0]

				err = firecrackerProcess.KillWithContext(ctx)
				require.NoError(err, "failed to kill firecracker process")
			},
		},

		// Test that StopVM returns the expected error when the VMM exits with an error (simulated by sending
		// SIGKILL to the VMM in the middle of a StopVM call).
		{
			name:       "ErrorExit",
			withStopVM: true,

			createVMRequest: proto.CreateVMRequest{
				RootDrive: &proto.FirecrackerRootDrive{
					HostPath: "/var/lib/firecracker-containerd/runtime/rootfs-slow-reboot.img",
				},
			},
			stopFunc: func(ctx context.Context, fcClient fccontrol.FirecrackerService, req proto.CreateVMRequest) {
				firecrackerProcesses, err := findProcess(ctx, findFirecracker)
				require.NoError(err, "failed waiting for expected firecracker process %q to come up", firecrackerProcessName)
				require.Len(firecrackerProcesses, 1, "expected only one firecracker process to exist")
				firecrackerProcess := firecrackerProcesses[0]

				go func() {
					time.Sleep(5 * time.Second)
					err := firecrackerProcess.KillWithContext(ctx)
					require.NoError(err, "failed to kill firecracker process")
				}()

				_, err = fcClient.StopVM(ctx, &proto.StopVMRequest{
					VMID:           req.VMID,
					TimeoutSeconds: 10,
				})

				require.Error(err)
				assert.Equal(codes.Internal, status.Code(err))
				// This is technically not accurate (the test is terminating the VM) though.
				assert.Contains(err.Error(), "forcefully terminated")
			},
		},

		// Test that StopVM returns success if the VM is in paused state, instead of hanging forever.
		// VM is force shutdown in this case, so we expect no Error or hang.
		{
			name:       "PauseStop",
			withStopVM: true,

			createVMRequest: proto.CreateVMRequest{},
			stopFunc: func(ctx context.Context, fcClient fccontrol.FirecrackerService, req proto.CreateVMRequest) {
				_, err = fcClient.PauseVM(ctx, &proto.PauseVMRequest{VMID: req.VMID})
				require.Equal(status.Code(err), codes.OK)

				_, err = fcClient.StopVM(ctx, &proto.StopVMRequest{VMID: req.VMID})
				require.Error(err)
				assert.Equal(codes.Internal, status.Code(err))
				assert.Contains(err.Error(), "forcefully terminated")
			},
		},

		{
			name:       "Suspend",
			withStopVM: true,

			createVMRequest: proto.CreateVMRequest{},
			stopFunc: func(ctx context.Context, fcClient fccontrol.FirecrackerService, req proto.CreateVMRequest) {
				firecrackerProcesses, err := findProcess(ctx, findFirecracker)
				require.NoError(err, "failed waiting for expected firecracker process %q to come up", firecrackerProcessName)
				require.Len(firecrackerProcesses, 1, "expected only one firecracker process to exist")
				firecrackerProcess := firecrackerProcesses[0]

				err = firecrackerProcess.Suspend()
				require.NoError(err, "failed to suspend Firecracker")

				_, err = fcClient.StopVM(ctx, &proto.StopVMRequest{VMID: req.VMID})
				require.Error(err)
				assert.Equal(codes.Internal, status.Code(err))
				assert.Contains(err.Error(), "forcefully terminated")
			},
		},
	}

	fcClient, err := newFCControlClient(containerdSockPath)
	require.NoError(err)

	for _, test := range tests {
		test := test

		testFunc := func(t *testing.T, createVMRequest proto.CreateVMRequest) {
			ctx, cancel := context.WithTimeout(ctx, 60*time.Second)
			defer cancel()

			vmID := createVMRequest.VMID

			_, err = fcClient.CreateVM(ctx, &createVMRequest)
			require.NoError(err)

			c, err := client.NewContainer(ctx,
				"container-"+vmID,
				containerd.WithSnapshotter(defaultSnapshotterName),
				containerd.WithNewSnapshot("snapshot-"+vmID, image),
				containerd.WithNewSpec(oci.WithProcessArgs("/bin/echo", "-n", "hello"), firecrackeroci.WithVMID(vmID)),
			)
			require.NoError(err)

			stdout := startAndWaitTask(ctx, t, c)
			require.Equal("hello", stdout)

			shimProcesses, err := findProcess(ctx, findShim)
			require.NoError(err, "failed to find shim process %q", shimProcessName)
			require.Len(shimProcesses, 1, "expected only one shim process to exist")
			shimProcess := shimProcesses[0]

			fcProcesses, err := findProcess(ctx, findFirecracker)
			require.NoError(err, "failed to find firecracker")
			require.Len(fcProcesses, 1, "expected only one firecracker process to exist")
			fcProcess := fcProcesses[0]

			test.stopFunc(ctx, fcClient, createVMRequest)

			// If the function above uses StopVMRequest, all underlying processes must be dead
			// (either gracefully or forcibly) at the end of the request.
			if test.withStopVM {
				fcExists := pidExists(int(fcProcess.Pid))
				assert.NoError(err, "failed to find firecracker")
				assert.False(fcExists, "firecracker %s (pid=%d) is still there", vmID, fcProcess.Pid)

				shimExists := pidExists(int(shimProcess.Pid))
				assert.NoError(err, "failed to find shim")
				assert.False(shimExists, "shim %s (pid=%d) is still there", vmID, shimProcess.Pid)
			}

			err = internal.WaitForPidToExit(ctx, time.Second, shimProcess.Pid)
			require.NoError(err, "shim hasn't been terminated")
		}

		t.Run(test.name, func(t *testing.T) {
			req := test.createVMRequest
			req.VMID = testNameToVMID(t.Name())
			testFunc(t, req)
		})

		t.Run(test.name+"/Jailer", func(t *testing.T) {
			req := test.createVMRequest
			req.VMID = testNameToVMID(t.Name())
			req.JailerConfig = &proto.JailerConfig{
				UID: 300000,
				GID: 300000,
			}
			testFunc(t, req)

			runcClient := &runc.Runc{}
			containers, err := runcClient.List(ctx)
			require.NoError(err, "failed to run 'runc list'")
			assert.Equal(0, len(containers))
		})
	}
}

func TestExec_Isolated(t *testing.T) {
	prepareIntegTest(t)

	client, err := containerd.New(containerdSockPath, containerd.WithDefaultRuntime(firecrackerRuntime))
	require.NoError(t, err, "unable to create client to containerd service at %s, is containerd running?", containerdSockPath)
	defer client.Close()

	ctx := namespaces.WithNamespace(context.Background(), "default")

	image, err := alpineImage(ctx, client, defaultSnapshotterName)
	require.NoError(t, err, "failed to get alpine image")

	fcClient, err := newFCControlClient(containerdSockPath)
	require.NoError(t, err, "failed to create ttrpc client")

	vmID := testNameToVMID(t.Name())

	_, err = fcClient.CreateVM(ctx, &proto.CreateVMRequest{VMID: vmID})
	require.NoError(t, err)

	c, err := client.NewContainer(ctx,
		"container-"+vmID,
		containerd.WithSnapshotter(defaultSnapshotterName),
		containerd.WithNewSnapshot("snapshot-"+vmID, image),
		containerd.WithNewSpec(oci.WithProcessArgs("/bin/sleep", "3"), firecrackeroci.WithVMID(vmID)),
	)
	require.NoError(t, err)

	var taskStdout bytes.Buffer
	var taskStderr bytes.Buffer

	task, err := c.NewTask(ctx, cio.NewCreator(cio.WithStreams(os.Stdin, &taskStdout, &taskStderr)))
	require.NoError(t, err, "failed to create task for container %s", c.ID())

	taskExitCh, err := task.Wait(ctx)
	require.NoError(t, err, "failed to wait on task for container %s", c.ID())

	err = task.Start(ctx)
	require.NoError(t, err, "failed to start task for container %s", c.ID())

	var execStdout bytes.Buffer
	var execStderr bytes.Buffer

	const execID = "date"
	taskExec, err := task.Exec(ctx, execID, &specs.Process{
		Args: []string{"/bin/date"},
		Cwd:  "/",
	}, cio.NewCreator(cio.WithStreams(os.Stdin, &execStdout, &execStderr)))
	require.NoError(t, err)

	// Intentionally reuse execID.
	_, err = task.Exec(ctx, execID, &specs.Process{
		Args: []string{"/bin/date"},
		Cwd:  "/",
	}, cio.NewCreator(cio.WithStreams(os.Stdin, ioutil.Discard, ioutil.Discard)))
	assert.Error(t, err)
	assert.Truef(t, errdefs.IsAlreadyExists(err), "%q's cause must be %q", err, errdefs.ErrAlreadyExists)

	execExitCh, err := taskExec.Wait(ctx)
	require.NoError(t, err, "failed to wait on exec %s", "exec")

	execBegin := time.Now()
	err = taskExec.Start(ctx)
	require.NoError(t, err, "failed to start exec %s", "exec")

	select {
	case execStatus := <-execExitCh:
		assert.NoError(t, execStatus.Error())
		assert.Equal(t, uint32(0), execStatus.ExitCode())

		execElapsed := time.Since(execBegin)
		assert.Truef(
			t, execElapsed < 1*time.Second,
			"The exec took %s to finish, which was too slow.", execElapsed,
		)

		_, err := taskExec.Delete(ctx)
		assert.NoError(t, err)

		deleteElapsed := time.Since(execBegin)
		assert.Truef(
			t, deleteElapsed < 1*time.Second,
			"The deletion of the exec took %s to finish, which was too slow.", deleteElapsed,
		)

		assert.NotEqual(t, "", execStdout.String())
		assert.Equal(t, "", execStderr.String())
	case <-ctx.Done():
		require.Fail(t, "context cancelled",
			"context cancelled while waiting for container %s to exit, err: %v", c.ID(), ctx.Err())
	}

	select {
	case taskStatus := <-taskExitCh:
		assert.NoError(t, taskStatus.Error())
		assert.Equal(t, uint32(0), taskStatus.ExitCode())

		deleteResult, err := task.Delete(ctx)
		assert.NoErrorf(t, err, "failed to delete task %q after exit", c.ID())
		assert.NoError(t, deleteResult.Error())

		assert.Equal(t, "", taskStdout.String())
		assert.Equal(t, "", taskStderr.String())
	case <-ctx.Done():
		require.Fail(t, "context cancelled",
			"context cancelled while waiting for container %s to exit, err: %v", c.ID(), ctx.Err())
	}

	_, err = fcClient.StopVM(ctx, &proto.StopVMRequest{VMID: vmID})
	assert.NoError(t, err)
	require.Equal(t, status.Code(err), codes.OK)
}

func TestEvents_Isolated(t *testing.T) {
	prepareIntegTest(t)
	require := require.New(t)

	client, err := containerd.New(containerdSockPath, containerd.WithDefaultRuntime(firecrackerRuntime))
	require.NoError(err, "unable to create client to containerd service at %s, is containerd running?", containerdSockPath)
	defer client.Close()

	ctx := namespaces.WithNamespace(context.Background(), "default")

	// If we don't have enough events within 30 seconds, the context will be cancelled and the loop below will be interrupted
	subscribeCtx, subscribeCancel := context.WithTimeout(ctx, 30*time.Second)
	defer subscribeCancel()
	eventCh, errCh := client.Subscribe(subscribeCtx, "topic")

	image, err := alpineImage(ctx, client, defaultSnapshotterName)
	require.NoError(err, "failed to get alpine image")

	vmID := testNameToVMID(t.Name())

	fcClient, err := newFCControlClient(containerdSockPath)
	require.NoError(err, "failed to create ttrpc client")

	_, err = fcClient.CreateVM(ctx, &proto.CreateVMRequest{VMID: vmID})
	require.NoError(err)

	c, err := client.NewContainer(ctx,
		"container-"+vmID,
		containerd.WithSnapshotter(defaultSnapshotterName),
		containerd.WithNewSnapshot("snapshot-"+vmID, image),
		containerd.WithNewSpec(oci.WithProcessArgs("/bin/echo", "-n", "hello"), firecrackeroci.WithVMID(vmID)),
	)
	require.NoError(err)

	stdout := startAndWaitTask(ctx, t, c)
	require.Equal("hello", stdout)

	_, err = fcClient.StopVM(ctx, &proto.StopVMRequest{VMID: vmID})
	require.Equal(status.Code(err), codes.OK)

	expected := []string{
		"/snapshot/prepare",
		"/snapshot/commit",
		"/firecracker-vm/start",
		"/snapshot/prepare",
		"/containers/create",
		"/tasks/create",
		"/tasks/start",
		"/tasks/exit",
		"/tasks/delete",
		"/firecracker-vm/stop",
	}
	var actual []string

loop:
	for len(actual) < len(expected) {
		select {
		case event := <-eventCh:
			actual = append(actual, event.Topic)
		case err := <-errCh:
			assert.NoError(t, err)
			break loop
		}
	}
	require.Equal(expected, actual)
}

func findProcWithName(name string) func(context.Context, *process.Process) (bool, error) {
	return func(ctx context.Context, p *process.Process) (bool, error) {
		processExecutable, err := p.ExeWithContext(ctx)
		if err != nil {
			// The call above reads /proc filesystem.
			// If the process is died before reading the filesystem,
			// the call would return ENOENT and that's fine.
			if os.IsNotExist(err) {
				return false, nil
			}
			return false, err
		}

		return filepath.Base(processExecutable) == name, nil
	}
}

func TestOOM_Isolated(t *testing.T) {
	prepareIntegTest(t)

	client, err := containerd.New(containerdSockPath, containerd.WithDefaultRuntime(firecrackerRuntime))
	require.NoError(t, err, "unable to create client to containerd service at %s, is containerd running?", containerdSockPath)
	defer client.Close()

	ctx := namespaces.WithNamespace(context.Background(), "default")

	// If we don't have enough events within 30 seconds, the context will be cancelled and the loop below will be interrupted
	subscribeCtx, subscribeCancel := context.WithTimeout(ctx, 30*time.Second)
	defer subscribeCancel()
	eventCh, errCh := client.Subscribe(subscribeCtx, "topic")

	image, err := alpineImage(ctx, client, defaultSnapshotterName)
	require.NoError(t, err, "failed to get alpine image")

	vmID := testNameToVMID(t.Name())

	fcClient, err := newFCControlClient(containerdSockPath)
	require.NoError(t, err, "failed to create ttrpc client")

	_, err = fcClient.CreateVM(ctx, &proto.CreateVMRequest{VMID: vmID})
	require.NoError(t, err)

	c, err := client.NewContainer(ctx,
		"container-"+vmID,
		containerd.WithSnapshotter(defaultSnapshotterName),
		containerd.WithNewSnapshot("snapshot-"+vmID, image),
		containerd.WithNewSpec(
			// The container is having 3MB of memory.
			oci.WithMemoryLimit(3*1024*1024),
			// But the dd command allocates 10MB of data on memory, which will be OOM killed.
			oci.WithProcessArgs("/bin/dd", "if=/dev/zero", "ibs=10M", "of=/dev/null"),
			firecrackeroci.WithVMID(vmID),
		),
	)
	require.NoError(t, err)

	task, err := c.NewTask(ctx, cio.NewCreator(cio.WithStreams(nil, os.Stdout, os.Stderr)))
	require.NoError(t, err, "failed to create task for container %s", c.ID())

	err = task.Start(ctx)
	require.NoError(t, err, "failed to create task for container %s", c.ID())

	_, err = task.Wait(ctx)
	require.NoError(t, err)

	expected := []string{
		"/snapshot/prepare",
		"/snapshot/commit",
		"/firecracker-vm/start",
		"/snapshot/prepare",
		"/containers/create",
		"/tasks/create",
		"/tasks/start",
		"/tasks/oom",
		"/tasks/exit",
	}
	var actual []string

loop:
	for len(actual) < len(expected) {
		select {
		case event := <-eventCh:
			actual = append(actual, event.Topic)
		case err := <-errCh:
			t.Logf("events = %v", actual)
			assert.NoError(t, err)
			break loop
		}
	}
	require.Equal(t, expected, actual)
}

func requireNonEmptyFifo(t testing.TB, path string) {
	file, err := os.Open(path)
	require.NoError(t, err)
	defer file.Close()

	reader := bufio.NewReader(file)

	// Since this is a FIFO, reading till EOF would block if its writer-end is still open.
	line, err := reader.ReadString('\n')
	require.NoError(t, err)
	require.NotEqualf(t, "", line, "%s must not be empty", path)

	info, err := os.Stat(path)
	require.NoError(t, err)
	require.Equal(t, os.ModeNamedPipe, info.Mode()&os.ModeType, "%s is a FIFO file", path)
}

func TestCreateVM_Isolated(t *testing.T) {
	prepareIntegTest(t)
	client, err := containerd.New(containerdSockPath, containerd.WithDefaultRuntime(firecrackerRuntime))
	require.NoError(t, err, "unable to create client to containerd service at %s, is containerd running?", containerdSockPath)
	defer client.Close()

	ctx := namespaces.WithNamespace(context.Background(), "default")

	fcClient, err := newFCControlClient(containerdSockPath)
	require.NoError(t, err, "failed to create ttrpc client")

	type subtest struct {
		name                    string
		request                 proto.CreateVMRequest
		validate                func(*testing.T, error)
		validateUsesFindProcess bool
		stopVM                  bool
	}

	subtests := []subtest{
		{
			name:    "Happy Case",
			request: proto.CreateVMRequest{},
			validate: func(t *testing.T, err error) {
				require.NoError(t, err)
			},
			stopVM: true,
		},
		{
			name: "Error Case",
			request: proto.CreateVMRequest{
				TimeoutSeconds: 5,
				RootDrive: &proto.FirecrackerRootDrive{
					HostPath: "/var/lib/firecracker-containerd/runtime/rootfs-no-agent.img",
				},
			},
			validate: func(t *testing.T, err error) {
				require.NotNil(t, err, "expected an error but did not receive any")
				time.Sleep(5 * time.Second)
				firecrackerProcesses, err := findProcess(ctx, findFirecracker)
				require.NoError(t, err, "failed waiting for expected firecracker process %q to come up", firecrackerProcessName)
				require.Len(t, firecrackerProcesses, 0, "expected only no firecracker processes to exist")
			},
			validateUsesFindProcess: true,
			stopVM:                  false,
		},
		{
			name: "Slow Root FS",
			request: proto.CreateVMRequest{
				RootDrive: &proto.FirecrackerRootDrive{
					HostPath: "/var/lib/firecracker-containerd/runtime/rootfs-slow-boot.img",
				},
			},
			validate: func(t *testing.T, err error) {
				require.Error(t, err)
				assert.Equal(t, codes.DeadlineExceeded, status.Code(err))
				assert.Contains(t, err.Error(), "didn't start within 20s")
			},
			stopVM: true,
		},
		{
			name: "Slow Root FS and Longer Timeout",
			request: proto.CreateVMRequest{
				RootDrive: &proto.FirecrackerRootDrive{
					HostPath: "/var/lib/firecracker-containerd/runtime/rootfs-slow-boot.img",
				},
				TimeoutSeconds: 60,
			},
			validate: func(t *testing.T, err error) {
				require.NoError(t, err)
			},
			stopVM: true,
		},
	}

	runTest := func(t *testing.T, request proto.CreateVMRequest, s subtest) {
		// If this test checks the number of the processes on the host
		// (e.g. the number of Firecracker processes), running the test with
		// others in parallel messes up the result.
		if !s.validateUsesFindProcess {
			t.Parallel()
		}
		vmID := testNameToVMID(t.Name())

		tempDir := t.TempDir()

		logFile := filepath.Join(tempDir, "log.fifo")
		metricsFile := filepath.Join(tempDir, "metrics.fifo")

		request.VMID = vmID
		request.LogFifoPath = logFile
		request.MetricsFifoPath = metricsFile

		resp, createVMErr := fcClient.CreateVM(ctx, &request)

		// Even CreateVM fails, the log file and the metrics file must have some data.
		requireNonEmptyFifo(t, logFile)
		requireNonEmptyFifo(t, metricsFile)

		// Some test cases are expected to have an error, some are not.
		s.validate(t, createVMErr)

		if createVMErr == nil && s.stopVM {
			// Ensure the response fields are populated correctly
			assert.Equal(t, request.VMID, resp.VMID)

			_, err := fcClient.StopVM(ctx, &proto.StopVMRequest{VMID: request.VMID})
			require.Equal(t, status.Code(err), codes.OK)
		}
	}

	for _, _s := range subtests {
		s := _s
		request := s.request
		t.Run(s.name, func(t *testing.T) {
			runTest(t, request, s)
		})

		requestWithJailer := s.request
		requestWithJailer.JailerConfig = &proto.JailerConfig{
			UID: 30000,
			GID: 30000,
		}
		t.Run(s.name+"/Jailer", func(t *testing.T) {
			runTest(t, requestWithJailer, s)
		})
	}
}

func TestPauseResume_Isolated(t *testing.T) {
	prepareIntegTest(t)

	client, err := containerd.New(containerdSockPath, containerd.WithDefaultRuntime(firecrackerRuntime))
	require.NoError(t, err, "unable to create client to containerd service at %s, is containerd running?", containerdSockPath)
	defer client.Close()

	ctx := namespaces.WithNamespace(context.Background(), "default")

	fcClient, err := newFCControlClient(containerdSockPath)
	require.NoError(t, err, "failed to create ttrpc client")

	subtests := []struct {
		name  string
		state func(ctx context.Context, resp *proto.CreateVMResponse)
	}{
		{
			name: "PauseVM",
			state: func(ctx context.Context, resp *proto.CreateVMResponse) {
				_, err := fcClient.PauseVM(ctx, &proto.PauseVMRequest{VMID: resp.VMID})
				require.NoError(t, err)
			},
		},
		{
			name: "ResumeVM",
			state: func(ctx context.Context, resp *proto.CreateVMResponse) {
				_, err := fcClient.ResumeVM(ctx, &proto.ResumeVMRequest{VMID: resp.VMID})
				require.NoError(t, err)
			},
		},
		{
			name: "Consecutive PauseVM",
			state: func(ctx context.Context, resp *proto.CreateVMResponse) {
				_, err := fcClient.PauseVM(ctx, &proto.PauseVMRequest{VMID: resp.VMID})
				require.NoError(t, err)

				_, err = fcClient.PauseVM(ctx, &proto.PauseVMRequest{VMID: resp.VMID})
				require.NoError(t, err)
			},
		},
		{
			name: "Consecutive ResumeVM",
			state: func(ctx context.Context, resp *proto.CreateVMResponse) {
				_, err := fcClient.ResumeVM(ctx, &proto.ResumeVMRequest{VMID: resp.VMID})
				require.NoError(t, err)

				_, err = fcClient.ResumeVM(ctx, &proto.ResumeVMRequest{VMID: resp.VMID})
				require.NoError(t, err)
			},
		},
		{
			name: "PauseResume",
			state: func(ctx context.Context, resp *proto.CreateVMResponse) {
				_, err := fcClient.PauseVM(ctx, &proto.PauseVMRequest{VMID: resp.VMID})
				require.NoError(t, err)

				_, err = fcClient.ResumeVM(ctx, &proto.ResumeVMRequest{VMID: resp.VMID})
				require.NoError(t, err)
			},
		},
		{
			name: "ResumePause",
			state: func(ctx context.Context, resp *proto.CreateVMResponse) {
				_, err := fcClient.ResumeVM(ctx, &proto.ResumeVMRequest{VMID: resp.VMID})
				require.NoError(t, err)

				_, err = fcClient.PauseVM(ctx, &proto.PauseVMRequest{VMID: resp.VMID})
				require.NoError(t, err)
			},
		},
	}

	runTest := func(t *testing.T, request *proto.CreateVMRequest, state func(ctx context.Context, resp *proto.CreateVMResponse)) {
		vmID := testNameToVMID(t.Name())

		tempDir := t.TempDir()

		logFile := filepath.Join(tempDir, "log.fifo")
		metricsFile := filepath.Join(tempDir, "metrics.fifo")

		request.VMID = vmID
		request.LogFifoPath = logFile
		request.MetricsFifoPath = metricsFile

		resp, createVMErr := fcClient.CreateVM(ctx, request)

		// Even CreateVM fails, the log file and the metrics file must have some data.
		requireNonEmptyFifo(t, logFile)
		requireNonEmptyFifo(t, metricsFile)

		// Run test case
		state(ctx, resp)

		// No VM to stop.
		if createVMErr != nil {
			return
		}

		// Ensure the response fields are populated correctly
		assert.Equal(t, request.VMID, resp.VMID)

		// Resume the VM since state() may pause the VM.
		_, err := fcClient.ResumeVM(ctx, &proto.ResumeVMRequest{VMID: vmID})
		require.NoError(t, err)

		_, err = fcClient.StopVM(ctx, &proto.StopVMRequest{VMID: vmID})
		require.NoError(t, err)
	}

	for _, subtest := range subtests {
		state := subtest.state
		t.Run(subtest.name, func(t *testing.T) {
			t.Parallel()
			runTest(t, &proto.CreateVMRequest{}, state)
		})

		t.Run(subtest.name+"/Jailer", func(t *testing.T) {
			t.Parallel()
			runTest(t, &proto.CreateVMRequest{JailerConfig: &proto.JailerConfig{UID: 30000, GID: 30000}}, state)
		})
	}
}
func TestAttach_Isolated(t *testing.T) {
	prepareIntegTest(t)

	client, err := containerd.New(containerdSockPath, containerd.WithDefaultRuntime(firecrackerRuntime))
	require.NoError(t, err, "unable to create client to containerd service at %s, is containerd running?", containerdSockPath)
	defer client.Close()

	ctx := namespaces.WithNamespace(context.Background(), "default")

	image, err := alpineImage(ctx, client, defaultSnapshotterName)
	require.NoError(t, err, "failed to get alpine image")

	testcases := []struct {
		name     string
		newIO    func(context.Context, string) (cio.IO, error)
		expected string
	}{
		{
			name: "attach",
			newIO: func(ctx context.Context, id string) (cio.IO, error) {
				set, err := cio.NewFIFOSetInDir("", id, false)
				if err != nil {
					return nil, err
				}

				return cio.NewDirectIO(ctx, set)
			},
			expected: "hello\n",
		},
		{
			name: "null io",

			// firecracker-containerd doesn't create IO Proxy objects in this case.
			newIO: func(ctx context.Context, id string) (cio.IO, error) {
				return cio.NullIO(id)
			},

			// So, attaching new IOs doesn't work.
			// While it looks odd, containerd's v2 shim has the same behavior.
			expected: "",
		},
	}

	for _, tc := range testcases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			name := testNameToVMID(t.Name())

			c, err := client.NewContainer(ctx,
				"container-"+name,
				containerd.WithSnapshotter(defaultSnapshotterName),
				containerd.WithNewSnapshot("snapshot-"+name, image),
				containerd.WithNewSpec(oci.WithProcessArgs("/bin/cat")),
			)
			require.NoError(t, err)

			io, err := tc.newIO(ctx, name)
			require.NoError(t, err)

			t1, err := c.NewTask(ctx, func(id string) (cio.IO, error) {
				return io, nil
			})
			require.NoError(t, err)

			ch, err := t1.Wait(ctx)
			require.NoError(t, err)

			err = t1.Start(ctx)
			require.NoError(t, err)

			stdin := bytes.NewBufferString("hello\n")
			var stdout bytes.Buffer
			t2, err := c.Task(
				ctx,
				cio.NewAttach(cio.WithStreams(stdin, &stdout, nil)),
			)
			require.NoError(t, err)
			assert.Equal(t, t1.ID(), t2.ID())

			err = io.Close()
			assert.NoError(t, err)

			err = t2.CloseIO(ctx, containerd.WithStdinCloser)
			assert.NoError(t, err)

			<-ch

			_, err = t2.Delete(ctx)
			require.NoError(t, err)

			assert.Equal(t, tc.expected, stdout.String())
		})
	}
}

// errorBuffer simulates a broken pipe (EPIPE) case.
type errorBuffer struct {
}

func (errorBuffer) Write(b []byte) (int, error) {
	return 0, errors.Errorf("failed to write %d bytes", len(b))
}

func TestBrokenPipe_Isolated(t *testing.T) {
	prepareIntegTest(t)

	client, err := containerd.New(containerdSockPath, containerd.WithDefaultRuntime(firecrackerRuntime))
	require.NoError(t, err, "unable to create client to containerd service at %s, is containerd running?", containerdSockPath)
	defer client.Close()

	ctx := namespaces.WithNamespace(context.Background(), "default")

	image, err := alpineImage(ctx, client, defaultSnapshotterName)
	require.NoError(t, err, "failed to get alpine image")

	name := testNameToVMID(t.Name())

	c, err := client.NewContainer(ctx,
		"container-"+name,
		containerd.WithSnapshotter(defaultSnapshotterName),
		containerd.WithNewSnapshot("snapshot-"+name, image),
		containerd.WithNewSpec(oci.WithProcessArgs("/usr/bin/yes")),
	)
	require.NoError(t, err)

	var stdout1 errorBuffer
	var stderr1 errorBuffer
	t1, err := c.NewTask(ctx, cio.NewCreator(cio.WithStreams(nil, &stdout1, &stderr1)))
	require.NoError(t, err)

	ch, err := t1.Wait(ctx)
	require.NoError(t, err)

	err = t1.Start(ctx)
	require.NoError(t, err)

	time.Sleep(10 * time.Second)

	err = t1.CloseIO(ctx, containerd.WithStdinCloser)
	require.NoError(t, err)

	var stdout2 bytes.Buffer
	var stderr2 bytes.Buffer
	t2, err := c.Task(
		ctx,
		cio.NewAttach(cio.WithStreams(nil, &stdout2, &stderr2)),
	)
	require.NoError(t, err)
	assert.Equal(t, t1.ID(), t2.ID())

	time.Sleep(10 * time.Second)

	err = t2.Kill(ctx, syscall.SIGKILL)
	assert.NoError(t, err)

	<-ch

	_, err = t2.Delete(ctx)
	require.NoError(t, err)

	assert.NotEqual(t, "", stdout2.String())
}
