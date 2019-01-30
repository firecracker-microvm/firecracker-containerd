// Copyright 2018 Amazon.com, Inc. or its affiliates. All Rights Reserved.
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

package devmapper

import (
	"context"
	"io/ioutil"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/docker/go-units"
	"github.com/firecracker-microvm/firecracker-containerd/internal"
	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/firecracker-microvm/firecracker-containerd/snapshotter/pkg/dmsetup"
	"github.com/firecracker-microvm/firecracker-containerd/snapshotter/pkg/losetup"
)

const (
	thinDevice1 = "thin-1"
	thinDevice2 = "thin-2"
	snapDevice1 = "snap-1"
	device1Size = 100000
	device2Size = 200000
	testsPrefix = "devmapper-snapshotter-tests-"
)

// TestPoolDevice runs integration tests for pool device.
// The following scenario implemented:
// - Create pool device with name 'test-pool-device'
// - Create two thin volumes 'thin-1' and 'thin-2'
// - Write ext4 file system on 'thin-1' and make sure it'errs moutable
// - Write v1 test file on 'thin-1' volume
// - Take 'thin-1' snapshot 'snap-1'
// - Change v1 file to v2 on 'thin-1'
// - Mount 'snap-1' and make sure test file is v1
// - Unmount volumes and remove all devices
func TestPoolDevice(t *testing.T) {
	internal.RequiresRoot(t)
	logrus.SetLevel(logrus.DebugLevel)
	ctx := context.Background()

	tempDir, err := ioutil.TempDir("", "pool-device-test-")
	require.NoErrorf(t, err, "couldn't get temp directory for testing")

	_, loopDataDevice := createLoopbackDevice(t, tempDir)
	_, loopMetaDevice := createLoopbackDevice(t, tempDir)

	defer func() {
		// Detach loop devices and remove images
		err := losetup.DetachLoopDevice(loopDataDevice, loopMetaDevice)
		assert.NoError(t, err)

		err = os.RemoveAll(tempDir)
		assert.NoErrorf(t, err, "couldn't cleanup temp directory")
	}()

	config := &Config{
		PoolName:             "test-pool-device-1",
		RootPath:             tempDir,
		DataDevice:           loopDataDevice,
		MetadataDevice:       loopMetaDevice,
		DataBlockSizeSectors: 128,
	}

	pool, err := NewPoolDevice(ctx, config)
	require.NoError(t, err, "can't create device pool")
	require.NotNil(t, pool)

	defer func() {
		err := pool.RemovePool(ctx)
		require.NoError(t, err, "can't close device pool")
	}()

	// Create thin devices
	t.Run("CreateThinDevice", func(t *testing.T) {
		testCreateThinDevice(t, pool)
	})

	// Make ext4 filesystem on 'thin-1'
	t.Run("MakeFileSystem", func(t *testing.T) {
		testMakeFileSystem(t, pool)
	})

	// Mount 'thin-1'
	thin1MountPath := tempMountPath(t)
	output, err := exec.Command("mount", dmsetup.GetFullDevicePath(thinDevice1), thin1MountPath).CombinedOutput()
	require.NoErrorf(t, err, "failed to mount '%s': %s", thinDevice1, string(output))

	// Write v1 test file on 'thin-1' device
	thin1TestFilePath := filepath.Join(thin1MountPath, "TEST")
	err = ioutil.WriteFile(thin1TestFilePath, []byte("test file (v1)"), 0700)
	require.NoErrorf(t, err, "failed to write test file v1 on '%s' volume", thinDevice1)

	// Take snapshot of 'thin-1'
	t.Run("CreateSnapshotDevice", func(t *testing.T) {
		testCreateSnapshot(t, pool)
	})

	// Update TEST file on 'thin-1' to v2
	err = ioutil.WriteFile(thin1TestFilePath, []byte("test file (v2)"), 0700)
	assert.NoErrorf(t, err, "failed to write test file v2 on 'thin-1' volume after taking snapshot")

	// Mount 'snap-1' and make sure TEST file is v1
	snap1MountPath := tempMountPath(t)
	output, err = exec.Command("mount", dmsetup.GetFullDevicePath(snapDevice1), snap1MountPath).CombinedOutput()
	require.NoErrorf(t, err, "failed to mount '%s' device: %s", snapDevice1, string(output))

	// Read test file from snapshot device and make sure it's v1
	fileData, err := ioutil.ReadFile(filepath.Join(snap1MountPath, "TEST"))
	assert.NoErrorf(t, err, "couldn't read test file from '%s' device", snapDevice1)
	assert.EqualValues(t, "test file (v1)", string(fileData), "test file content is invalid on snapshot")

	// Unmount devices before removing
	output, err = exec.Command("umount", thin1MountPath, snap1MountPath).CombinedOutput()
	assert.NoErrorf(t, err, "failed to unmount devices: %s", string(output))

	t.Run("DeactivateDevice", func(t *testing.T) {
		testDeactivateThinDevice(t, pool)
	})

	t.Run("RemoveDevice", func(t *testing.T) {
		testRemoveThinDevice(t, pool)
	})
}

func testCreateThinDevice(t *testing.T, pool *PoolDevice) {
	ctx := context.Background()

	err := pool.CreateThinDevice(ctx, thinDevice1, device1Size)
	require.NoError(t, err, "can't create first thin device")

	err = pool.CreateThinDevice(ctx, thinDevice1, device1Size)
	require.Error(t, err, "device pool allows duplicated device names")

	err = pool.CreateThinDevice(ctx, thinDevice2, device2Size)
	require.NoError(t, err, "can't create second thin device")

	deviceInfo1, err := pool.metadata.GetDevice(ctx, thinDevice1)
	assert.NoError(t, err)

	deviceInfo2, err := pool.metadata.GetDevice(ctx, thinDevice2)
	assert.NoError(t, err)

	assert.NotEqual(t, deviceInfo1.DeviceID, deviceInfo2.DeviceID, "assigned device ids should be different")
}

func testMakeFileSystem(t *testing.T, pool *PoolDevice) {
	devicePath := dmsetup.GetFullDevicePath(thinDevice1)
	args := []string{
		devicePath,
		"-E",
		"nodiscard,lazy_itable_init=0,lazy_journal_init=0",
	}

	output, err := exec.Command("mkfs.ext4", args...).CombinedOutput()
	require.NoErrorf(t, err, "failed to make filesystem on '%s': %s", thinDevice1, string(output))
}

func testCreateSnapshot(t *testing.T, pool *PoolDevice) {
	err := pool.CreateSnapshotDevice(context.Background(), thinDevice1, snapDevice1, device1Size)
	assert.NoErrorf(t, err, "failed to create snapshot from '%s' volume", thinDevice1)
}

func testDeactivateThinDevice(t *testing.T, pool *PoolDevice) {
	deviceList := []string{
		thinDevice2,
		snapDevice1,
	}

	for _, deviceName := range deviceList {
		err := pool.DeactivateDevice(context.Background(), deviceName, false)
		assert.NoErrorf(t, err, "failed to remove '%s'", deviceName)
	}

	err := pool.DeactivateDevice(context.Background(), "not-existing-device", false)
	assert.Error(t, err, "should return an error if trying to remove not existing device")
}

func testRemoveThinDevice(t *testing.T, pool *PoolDevice) {
	err := pool.RemoveDevice(testCtx, thinDevice1)
	assert.NoErrorf(t, err, "should delete thin device from pool")
}

func tempMountPath(t *testing.T) string {
	path, err := ioutil.TempDir("", "devmapper-snapshotter-mount-")
	require.NoError(t, err, "failed to get temp directory for mount")

	return path
}

func createLoopbackDevice(t *testing.T, dir string) (string, string) {
	file, err := ioutil.TempFile(dir, testsPrefix)
	require.NoError(t, err)

	size, err := units.RAMInBytes("128Mb")
	require.NoError(t, err)

	err = file.Truncate(size)
	require.NoError(t, err)

	err = file.Close()
	require.NoError(t, err)

	imagePath := file.Name()

	loopDevice, err := losetup.AttachLoopDevice(imagePath)
	require.NoError(t, err)

	return imagePath, loopDevice
}
