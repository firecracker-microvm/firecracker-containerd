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

package dmsetup

import (
	"io/ioutil"
	"os"
	"strings"
	"testing"

	"github.com/docker/go-units"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/sys/unix"

	"github.com/firecracker-microvm/firecracker-containerd/internal"
	"github.com/firecracker-microvm/firecracker-containerd/snapshotter/pkg/losetup"
)

const (
	testPoolName   = "test-pool"
	testDeviceName = "test-device"
	deviceID       = 1
	snapshotID     = 2
)

func TestDMSetup(t *testing.T) {
	internal.RequiresRoot(t)
	tempDir, err := ioutil.TempDir("", "dmsetup-tests-")
	require.NoErrorf(t, err, "failed to make temp dir for tests")

	defer func() {
		err := os.RemoveAll(tempDir)
		assert.NoError(t, err)
	}()

	dataImage, loopDataDevice := createLoopbackDevice(t, tempDir)
	metaImage, loopMetaDevice := createLoopbackDevice(t, tempDir)

	defer func() {
		err = losetup.RemoveLoopDevicesAssociatedWithImage(dataImage)
		assert.NoErrorf(t, err, "failed to detach loop devices for data image: %s", dataImage)

		err = losetup.RemoveLoopDevicesAssociatedWithImage(metaImage)
		assert.NoErrorf(t, err, "failed to detach loop devices for meta image: %s", metaImage)
	}()

	t.Run("CreatePool", func(t *testing.T) {
		err := CreatePool(testPoolName, loopDataDevice, loopMetaDevice, 128)
		require.NoErrorf(t, err, "failed to create thin-pool")

		table, err := Table(testPoolName)
		t.Logf("table: %s", table)
		assert.NoError(t, err)
		assert.True(t, strings.HasPrefix(table, "0 32768 thin-pool"))
		assert.True(t, strings.HasSuffix(table, "128 32768 1 skip_block_zeroing"))
	})

	t.Run("ReloadPool", func(t *testing.T) {
		err := ReloadPool(testPoolName, loopDataDevice, loopMetaDevice, 256)
		assert.NoErrorf(t, err, "failed to reload thin-pool")
	})

	t.Run("CreateDevice", testCreateDevice)

	t.Run("CreateSnapshot", testCreateSnapshot)
	t.Run("DeleteSnapshot", testDeleteSnapshot)

	t.Run("ActivateDevice", testActivateDevice)
	t.Run("DeviceStatus", testDeviceStatus)
	t.Run("SuspendResumeDevice", testSuspendResumeDevice)
	t.Run("RemoveDevice", testRemoveDevice)

	t.Run("RemovePool", func(t *testing.T) {
		err = RemoveDevice(testPoolName, RemoveWithForce, RemoveWithRetries)
		require.NoErrorf(t, err, "failed to remove thin-pool")
	})

	t.Run("Version", testVersion)
}

func testCreateDevice(t *testing.T) {
	err := CreateDevice(testPoolName, deviceID)
	require.NoError(t, err, "failed to create test device")

	err = CreateDevice(testPoolName, deviceID)
	assert.EqualValues(t, unix.EEXIST, err)

	infos, err := Info(testPoolName)
	require.NoError(t, err)
	require.Lenf(t, infos, 1, "got unexpected number of device infos")
}

func testCreateSnapshot(t *testing.T) {
	err := CreateSnapshot(testPoolName, snapshotID, deviceID)
	require.NoError(t, err)
}

func testDeleteSnapshot(t *testing.T) {
	err := DeleteDevice(testPoolName, snapshotID)
	require.NoErrorf(t, err, "failed to send delete message")

	err = DeleteDevice(testPoolName, snapshotID)
	assert.EqualValues(t, unix.ENODATA, err)
}

func testActivateDevice(t *testing.T) {
	err := ActivateDevice(testPoolName, testDeviceName, 1, 1024, "")
	require.NoErrorf(t, err, "failed to activate device")

	err = ActivateDevice(testPoolName, testDeviceName, 1, 1024, "")
	assert.Equal(t, err, unix.EBUSY)

	if _, err := os.Stat("/dev/mapper/" + testDeviceName); err != nil && !os.IsExist(err) {
		assert.Errorf(t, err, "failed to stat device")
	}

	list, err := Info(testPoolName)
	assert.NoError(t, err)
	require.Len(t, list, 1)

	info := list[0]
	assert.Equal(t, testPoolName, info.Name)
	assert.True(t, info.TableLive)
}

func testDeviceStatus(t *testing.T) {
	status, err := Status(testDeviceName)
	require.NoError(t, err)

	assert.EqualValues(t, 0, status.Offset)
	assert.EqualValues(t, 2, status.Length)
	assert.Equal(t, "thin", status.Target)
	assert.EqualValues(t, status.Params, []string{"0", "-"})
}

func testSuspendResumeDevice(t *testing.T) {
	err := SuspendDevice(testDeviceName)
	assert.NoError(t, err)

	err = SuspendDevice(testDeviceName)
	assert.NoError(t, err)

	list, err := Info(testDeviceName)
	assert.NoError(t, err)
	require.Len(t, list, 1)

	info := list[0]
	assert.True(t, info.Suspended)

	err = ResumeDevice(testDeviceName)
	assert.NoError(t, err)

	err = ResumeDevice(testDeviceName)
	assert.NoError(t, err)
}

func testRemoveDevice(t *testing.T) {
	err := RemoveDevice(testPoolName)
	assert.EqualValues(t, unix.EBUSY, err, "removing thin-pool with dependencies shouldn't be allowed")

	err = RemoveDevice(testDeviceName, RemoveWithRetries)
	assert.NoErrorf(t, err, "failed to remove thin-device")
}

func testVersion(t *testing.T) {
	version, err := Version()
	assert.NoError(t, err)
	assert.NotEmpty(t, version)
}

func createLoopbackDevice(t *testing.T, dir string) (string, string) {
	file, err := ioutil.TempFile(dir, "dmsetup-tests-")
	require.NoError(t, err)

	size, err := units.RAMInBytes("16Mb")
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
