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
	"io/ioutil"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/containerd/containerd"
	"github.com/containerd/containerd/namespaces"
	"github.com/containerd/containerd/oci"
	"github.com/opencontainers/runtime-spec/specs-go"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	_ "github.com/firecracker-microvm/firecracker-containerd/firecracker-control"
	"github.com/firecracker-microvm/firecracker-containerd/internal"
	"github.com/firecracker-microvm/firecracker-containerd/proto"
	"github.com/firecracker-microvm/firecracker-containerd/runtime/cpuset"
	"github.com/firecracker-microvm/firecracker-containerd/runtime/firecrackeroci"
)

const (
	jailerUID = 300001
	jailerGID = 300001
)

func TestJailer_Isolated(t *testing.T) {
	prepareIntegTest(t)
	t.Run("Without Jailer", func(t *testing.T) {
		t.Parallel()
		testJailer(t, nil)
	})
	t.Run("With Jailer", func(t *testing.T) {
		t.Parallel()
		testJailer(t, &proto.JailerConfig{
			UID: jailerUID,
			GID: jailerGID,
		})
	})
	t.Run("With Jailer and bind-mount", func(t *testing.T) {
		t.Parallel()
		testJailer(t, &proto.JailerConfig{
			UID:               jailerUID,
			GID:               jailerGID,
			DriveExposePolicy: proto.DriveExposePolicy_BIND,
		})
	})
}

func TestAttachBlockDevice_Isolated(t *testing.T) {
	prepareIntegTest(t)
	t.Run("Without Jailer", func(t *testing.T) {
		t.Parallel()
		testAttachBlockDevice(t, nil)
	})
	t.Run("With Jailer", func(t *testing.T) {
		t.Parallel()
		testAttachBlockDevice(t, &proto.JailerConfig{
			UID: jailerUID,
			GID: jailerGID,
		})
	})
	t.Run("With Jailer and bind-mount", func(t *testing.T) {
		t.Parallel()
		testAttachBlockDevice(t, &proto.JailerConfig{
			UID:               jailerUID,
			GID:               jailerGID,
			DriveExposePolicy: proto.DriveExposePolicy_BIND,
		})
	})
}

func fsSafeTestName(tb testing.TB) string {
	return strings.ReplaceAll(tb.Name(), "/", "-")
}

func testJailer(t *testing.T, jailerConfig *proto.JailerConfig) {
	require := require.New(t)

	client, err := containerd.New(containerdSockPath, containerd.WithDefaultRuntime(firecrackerRuntime))
	require.NoError(err, "unable to create client to containerd service at %s, is containerd running?", containerdSockPath)
	defer client.Close()

	ctx := namespaces.WithNamespace(context.Background(), "default")

	image, err := alpineImage(ctx, client, defaultSnapshotterName)
	require.NoError(err, "failed to get alpine image")

	vmID := testNameToVMID(t.Name())

	additionalDrive := internal.CreateFSImg(ctx, t, "ext4", internal.FSImgFile{
		Subpath:  "dir/hello",
		Contents: "additional drive\n",
	})

	request := proto.CreateVMRequest{
		VMID:         vmID,
		JailerConfig: jailerConfig,
		DriveMounts: []*proto.FirecrackerDriveMount{
			{HostPath: additionalDrive, VMPath: "/mnt", FilesystemType: "ext4"},
		},
	}

	// If the drive files are bind-mounted, the files must be readable from the jailer's user.
	if jailerConfig != nil && jailerConfig.DriveExposePolicy == proto.DriveExposePolicy_BIND {
		f, err := ioutil.TempFile("", fsSafeTestName(t)+"_rootfs")
		require.NoError(err)
		defer f.Close()

		dst := f.Name()

		// Copy the root drive before chown, since the file is used by other tests.
		err = copyFile(defaultRuntimeConfig.RootDrive, dst, 0400)
		require.NoErrorf(err, "failed to copy a rootfs as %q", dst)

		err = os.Chown(dst, int(jailerConfig.UID), int(jailerConfig.GID))
		require.NoError(err, "failed to chown %q", dst)

		request.RootDrive = &proto.FirecrackerRootDrive{HostPath: dst}

		// The additional drive file is only used by this test.
		err = os.Chown(additionalDrive, int(jailerConfig.UID), int(jailerConfig.GID))
		require.NoError(err, "failed to chown %q", additionalDrive)
	}

	fcClient, err := newFCControlClient(containerdSockPath)
	require.NoError(err)

	_, err = fcClient.CreateVM(ctx, &request)
	require.NoError(err)

	c, err := client.NewContainer(ctx,
		vmID+"-container",
		containerd.WithSnapshotter(defaultSnapshotterName),
		containerd.WithNewSnapshot(vmID+"-snapshot", image),
		containerd.WithNewSpec(
			oci.WithProcessArgs(
				"/bin/sh", "-c", "echo hello && cat /mnt/in-container/dir/hello",
			),
			firecrackeroci.WithVMID(vmID),
			oci.WithMounts([]specs.Mount{{
				Source:      "/mnt",
				Destination: "/mnt/in-container",
				Options:     []string{"bind"},
			}}),
		),
	)
	require.NoError(err)

	stdout := startAndWaitTask(ctx, t, c)
	require.Equal("hello\nadditional drive\n", stdout)

	stat, err := os.Stat(filepath.Join(shimBaseDir(), "default#"+vmID))
	require.NoError(err)
	assert.True(t, stat.IsDir())

	err = c.Delete(ctx, containerd.WithSnapshotCleanup)
	require.NoError(err, "failed to delete a container")

	_, err = fcClient.StopVM(ctx, &proto.StopVMRequest{VMID: vmID})
	require.NoError(err)

	_, err = os.Stat(filepath.Join(shimBaseDir(), "default#"+vmID))
	assert.Error(t, err)
	assert.True(t, os.IsNotExist(err))

	shimContents, err := ioutil.ReadDir(shimBaseDir())
	require.NoError(err)
	assert.Len(t, shimContents, 0)
}

func TestJailerCPUSet_Isolated(t *testing.T) {
	prepareIntegTest(t)

	b := cpuset.Builder{}
	cset := b.AddCPU(0).AddMem(0).Build()
	config := &proto.JailerConfig{
		CPUs: cset.CPUs(),
		Mems: cset.Mems(),
		UID:  300000,
		GID:  300000,
	}
	testJailer(t, config)
}

func testAttachBlockDevice(t *testing.T, jailerConfig *proto.JailerConfig) {
	require := require.New(t)
	client, err := containerd.New(containerdSockPath, containerd.WithDefaultRuntime(firecrackerRuntime))
	require.NoError(err, "unable to create client to containerd service at %s, is containerd running?", containerdSockPath)
	defer client.Close()

	ctx := namespaces.WithNamespace(context.Background(), "default")

	image, err := alpineImage(ctx, client, defaultSnapshotterName)
	require.NoError(err, "failed to get alpine image")

	fcClient, err := newFCControlClient(containerdSockPath)
	require.NoError(err)

	vmID := testNameToVMID(t.Name())

	device, cleanup := internal.CreateBlockDevice(ctx, t)
	defer cleanup()

	if jailerConfig != nil {
		err := os.Chown(device, int(jailerConfig.UID), int(jailerConfig.GID))
		require.NoError(err)
	}

	request := proto.CreateVMRequest{
		VMID:         vmID,
		JailerConfig: jailerConfig,
		DriveMounts: []*proto.FirecrackerDriveMount{
			{HostPath: device, VMPath: "/home/driveMount", FilesystemType: "ext4"},
		},
	}

	// If the drive files are bind-mounted, the files must be readable from the jailer's user.
	if jailerConfig != nil && jailerConfig.DriveExposePolicy == proto.DriveExposePolicy_BIND {
		f, err := ioutil.TempFile("", fsSafeTestName(t)+"_rootfs")
		require.NoError(err)
		defer f.Close()

		dst := f.Name()

		// Copy the root drive before chown, since the file is used by other tests.
		err = copyFile(defaultRuntimeConfig.RootDrive, dst, 0400)
		require.NoErrorf(err, "failed to copy a rootfs as %q", dst)

		err = os.Chown(dst, int(jailerConfig.UID), int(jailerConfig.GID))
		require.NoError(err, "failed to chown %q", dst)

		request.RootDrive = &proto.FirecrackerRootDrive{HostPath: dst}

		err = os.Chown(device, int(jailerConfig.UID), int(jailerConfig.GID))
		require.NoError(err, "failed to chown %q", device)
	}

	_, err = fcClient.CreateVM(ctx, &request)
	require.NoError(err)

	// create a container to test bind mount block device into the container
	c, err := client.NewContainer(ctx,
		vmID+"-container",
		containerd.WithSnapshotter(defaultSnapshotterName),
		containerd.WithNewSnapshot(vmID+"-snapshot", image),
		containerd.WithNewSpec(
			oci.WithProcessArgs(
				"/bin/sh", "-c", "echo heyhey && cd /mnt/blockDeviceTest",
			),
			firecrackeroci.WithVMID(vmID),
			oci.WithMounts([]specs.Mount{{
				Source:      "/home/driveMount",
				Destination: "/mnt/blockDeviceTest",
				Options:     []string{"bind"},
			}}),
		),
	)
	require.NoError(err)

	stdout := startAndWaitTask(ctx, t, c)
	require.Equal("heyhey\n", stdout)

	stat, err := os.Stat(filepath.Join(shimBaseDir(), "default#"+vmID))
	require.NoError(err)
	assert.True(t, stat.IsDir())

	err = c.Delete(ctx, containerd.WithSnapshotCleanup)
	require.NoError(err, "failed to delete a container-block-device")

	_, err = fcClient.StopVM(ctx, &proto.StopVMRequest{VMID: vmID})
	require.NoError(err)

	_, err = os.Stat(filepath.Join(shimBaseDir(), "default#"+vmID))
	assert.Error(t, err)
	assert.True(t, os.IsNotExist(err))

	shimContents, err := ioutil.ReadDir(shimBaseDir())
	require.NoError(err)
	assert.Len(t, shimContents, 0)
}
