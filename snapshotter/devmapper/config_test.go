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
	"encoding/json"
	"io/ioutil"
	"os"
	"strings"
	"testing"

	"github.com/hashicorp/go-multierror"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLoadConfig(t *testing.T) {
	expected := Config{
		RootPath:       "/tmp",
		PoolName:       "test",
		DataDevice:     "/dev/loop0",
		MetadataDevice: "/dev/loop1",
		DataBlockSize:  "1mb",
		BaseImageSize:  "128Mb",
	}

	data, err := json.Marshal(&expected)
	require.NoErrorf(t, err, "failed to serialize config")

	file, err := ioutil.TempFile("", "devmapper-config-")
	require.NoError(t, err)

	defer func() {
		err := file.Close()
		assert.NoError(t, err)

		err = os.Remove(file.Name())
		assert.NoError(t, err)
	}()

	_, err = file.Write(data)
	require.NoError(t, err)

	loaded, err := LoadConfig(file.Name())
	require.NoError(t, err)

	assert.Equal(t, loaded.RootPath, expected.RootPath)
	assert.Equal(t, loaded.PoolName, expected.PoolName)
	assert.Equal(t, loaded.DataDevice, expected.DataDevice)
	assert.Equal(t, loaded.MetadataDevice, expected.MetadataDevice)
	assert.Equal(t, loaded.DataBlockSize, expected.DataBlockSize)
	assert.Equal(t, loaded.BaseImageSize, expected.BaseImageSize)

	assert.EqualValues(t, 1*1024*1024/512, loaded.DataBlockSizeSectors)
	assert.EqualValues(t, 128*1024*1024, loaded.BaseImageSizeBytes)
}

func TestLoadConfigInvalidPath(t *testing.T) {
	_, err := LoadConfig("")
	require.Error(t, err)

	_, err = LoadConfig("/dev/null")
	require.Error(t, err)
}

func TestParseInvalidData(t *testing.T) {
	config := Config{
		DataBlockSize: "x",
		BaseImageSize: "y",
	}

	err := config.parse()
	require.Error(t, err)

	multErr := (err).(*multierror.Error)
	require.Len(t, multErr.Errors, 2)

	assert.True(t, strings.Contains(multErr.Errors[0].Error(), "failed to parse data block size: \"x\""))
	assert.True(t, strings.Contains(multErr.Errors[1].Error(), "failed to parse base image size: \"y\""))
}

func TestFieldValidation(t *testing.T) {
	config := Config{DataBlockSizeSectors: 1}
	err := config.validate()
	require.Error(t, err)

	multErr := (err).(*multierror.Error)
	require.Len(t, multErr.Errors, 6)

	assert.Error(t, multErr.Errors[0], "pool_name is empty")
	assert.Error(t, multErr.Errors[1], "root_path is empty")
	assert.Error(t, multErr.Errors[2], "data_device is empty")
	assert.Error(t, multErr.Errors[3], "meta_device is empty")

	assert.Equal(t, multErr.Errors[4], errInvalidBlockSize)
	assert.Equal(t, multErr.Errors[5], errInvalidBlockAlignment)
}
