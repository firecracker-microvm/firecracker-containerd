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
	"strings"
	"testing"

	"github.com/docker/go-units"
	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	thinDevice1 = "thin-1"
	thinDevice2 = "thin-2"
	snapDevice1 = "snap-1"
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
	logrus.SetLevel(logrus.DebugLevel)
	ctx := context.Background()

	dataImagePath, loopDataDevice := createLoopbackDevice(t)
	metaImagePath, loopMetaDevice := createLoopbackDevice(t)

	defer func() {
		// Detach loop devices and remove images
		err := exec.Command("losetup", "--detach", loopDataDevice, loopMetaDevice).Run()
		assert.NoError(t, err)

		err = os.Remove(metaImagePath)
		assert.NoErrorf(t, err, "failed to remove metadata file: %s", metaImagePath)

		err = os.Remove(dataImagePath)
		assert.NoErrorf(t, err, "failed to remove data file: %s", dataImagePath)
	}()

	pool, err := NewPoolDevice(ctx, "test-pool-device", loopDataDevice, loopMetaDevice, 128)
	require.NoError(t, err, "can't create device pool")
	require.NotNil(t, pool)

	defer func() {
		err := pool.Close(ctx, true)
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
	output, err := exec.Command("mount", pool.GetDevicePath(thinDevice1), thin1MountPath).CombinedOutput()
	require.NoErrorf(t, err, "failed to mount '%s': %s", thinDevice1, string(output))

	// Write v1 test file on 'thin-1' device
	thin1TestFilePath := filepath.Join(thin1MountPath, "TEST")
	err = ioutil.WriteFile(thin1TestFilePath, []byte("test file (v1)"), 700)
	require.NoErrorf(t, err, "failed to write test file v1 on '%s' volume", thinDevice1)

	// Take snapshot of 'thin-1'
	t.Run("CreateSnapshotDevice", func(t *testing.T) {
		testCreateSnapshot(t, pool)
	})

	// Update TEST file on 'thin-1' to v2
	err = ioutil.WriteFile(thin1TestFilePath, []byte("test file (v2)"), 700)
	assert.NoErrorf(t, err, "failed to write test file v2 on 'thin-1' volume after taking snapshot")

	// Mount 'snap-1' and make sure TEST file is v1
	snap1MountPath := tempMountPath(t)
	output, err = exec.Command("mount", pool.GetDevicePath(snapDevice1), snap1MountPath).CombinedOutput()
	require.NoErrorf(t, err, "failed to mount '%s' device: %s", snapDevice1, string(output))

	// Read test file from snapshot device and make sure it's v1
	fileData, err := ioutil.ReadFile(filepath.Join(snap1MountPath, "TEST"))
	assert.NoErrorf(t, err, "couldn't read test file from '%s' device", snapDevice1)
	assert.EqualValues(t, "test file (v1)", string(fileData), "test file content is invalid on snapshot")

	// Unmount devices before removing
	output, err = exec.Command("umount", thin1MountPath, snap1MountPath).CombinedOutput()
	assert.NoErrorf(t, err, "failed to unmount devices: %s", string(output))

	t.Run("RemoveDevice", func(t *testing.T) {
		testRemoveThinDevice(t, pool)
	})
}

func testCreateThinDevice(t *testing.T, pool *PoolDevice) {
	const (
		device1Size = 100000
		device2Size = 200000
	)

	deviceID1, err := pool.CreateThinDevice(thinDevice1, device1Size)
	require.NoError(t, err, "can't create first thin device")

	_, err = pool.CreateThinDevice(thinDevice1, device1Size)
	require.Error(t, err, "device pool allows duplicated device names")

	deviceID2, err := pool.CreateThinDevice(thinDevice2, device2Size)
	require.NoError(t, err, "can't create second thin device")

	require.NotEqual(t, deviceID1, deviceID2, "device id not incremented properly")
}

func testMakeFileSystem(t *testing.T, pool *PoolDevice) {
	devicePath := pool.GetDevicePath(thinDevice1)
	args := []string{
		devicePath,
		"-E",
		"nodiscard,lazy_itable_init=0,lazy_journal_init=0",
	}

	output, err := exec.Command("mkfs.ext4", args...).CombinedOutput()
	require.NoErrorf(t, err, "failed to make filesystem on '%s': %s", thinDevice1, string(output))
}

func testCreateSnapshot(t *testing.T, pool *PoolDevice) {
	err := pool.CreateSnapshotDevice(thinDevice1, snapDevice1, 100000)
	assert.NoErrorf(t, err, "failed to create snapshot from '%s' volume", thinDevice1)
}

func testRemoveThinDevice(t *testing.T, pool *PoolDevice) {
	deviceList := []string{
		thinDevice1,
		thinDevice2,
		snapDevice1,
	}

	for _, deviceName := range deviceList {
		err := pool.RemoveDevice(deviceName)
		assert.NoErrorf(t, err, "failed to remove '%s'", deviceName)
	}

	err := pool.RemoveDevice("not-existing-device")
	assert.Error(t, err, "should return an error if trying to remove not existing device")
}

func tempMountPath(t *testing.T) string {
	path, err := ioutil.TempDir("", "devmapper-snapshotter-mount-")
	require.NoError(t, err, "failed to get temp directory for mount")

	return path
}

func createLoopbackDevice(t *testing.T) (string, string) {
	file, err := ioutil.TempFile("", "devmapper-snapshotter-")
	require.NoError(t, err)

	size, err := units.RAMInBytes("100Mb")
	require.NoError(t, err)

	err = file.Truncate(size)
	require.NoError(t, err)

	err = file.Close()
	require.NoError(t, err)

	imagePath := file.Name()

	output, err := exec.Command("losetup", "--find", "--show", imagePath).CombinedOutput()
	require.NoErrorf(t, err, "losetup error: %s", string(output))

	return imagePath, strings.TrimRight(string(output), "\n")
}
