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

package losetup

import (
	"io/ioutil"
	"os"
	"testing"

	"github.com/docker/go-units"
	"github.com/firecracker-microvm/firecracker-containerd/internal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLosetup(t *testing.T) {
	internal.RequiresRoot(t)
	var (
		imagePath   = createSparseImage(t)
		loopDevice1 string
		loopDevice2 string
	)

	defer func() {
		err := os.Remove(imagePath)
		assert.NoError(t, err)
	}()

	t.Run("AttachLoopDevice", func(t *testing.T) {
		dev1, err := AttachLoopDevice(imagePath)
		require.NoError(t, err)
		require.NotEmpty(t, dev1)

		dev2, err := AttachLoopDevice(imagePath)
		assert.NoError(t, err)
		assert.NotEqualf(t, dev2, dev1, "should attach different loop device")

		loopDevice1 = dev1
		loopDevice2 = dev2
	})

	t.Run("AttachEmptyLoopDevice", func(t *testing.T) {
		_, err := AttachLoopDevice("")
		assert.Error(t, err, "shouldn't attach empty path")
	})

	t.Run("FindAssociatedLoopDevices", func(t *testing.T) {
		devices, err := FindAssociatedLoopDevices(imagePath)
		assert.NoError(t, err)
		assert.Lenf(t, devices, 2, "unexpected number of attached devices")
		assert.ElementsMatch(t, devices, []string{loopDevice1, loopDevice2})
	})

	t.Run("FindAssociatedLoopDevicesForInvalidImage", func(t *testing.T) {
		devices, err := FindAssociatedLoopDevices("")
		assert.NoError(t, err)
		assert.Empty(t, devices)
	})

	t.Run("DetachLoopDevice", func(t *testing.T) {
		err := DetachLoopDevice(loopDevice2)
		require.NoErrorf(t, err, "failed to detach %q", loopDevice2)
	})

	t.Run("DetachEmptyDevice", func(t *testing.T) {
		err := DetachLoopDevice("")
		assert.Error(t, err, "shouldn't detach empty path")
	})

	t.Run("RemoveLoopDevicesAssociatedWithImage", func(t *testing.T) {
		err := RemoveLoopDevicesAssociatedWithImage(imagePath)
		assert.NoError(t, err)

		devices, err := FindAssociatedLoopDevices(imagePath)
		assert.NoError(t, err)
		assert.Empty(t, devices)
	})

	t.Run("RemoveLoopDevicesAssociatedWithInvalidImage", func(t *testing.T) {
		err := RemoveLoopDevicesAssociatedWithImage("")
		assert.NoError(t, err)
	})
}

func createSparseImage(t *testing.T) string {
	file, err := ioutil.TempFile("", "losetup-tests-")
	require.NoError(t, err)

	size, err := units.RAMInBytes("16Mb")
	require.NoError(t, err)

	err = file.Truncate(size)
	require.NoError(t, err)

	err = file.Close()
	require.NoError(t, err)

	return file.Name()
}
