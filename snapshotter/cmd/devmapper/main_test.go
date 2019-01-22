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

package main

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/firecracker-microvm/firecracker-containerd/snapshotter/devmapper"
)

func TestApplyStorageOpt(t *testing.T) {
	var (
		config = &devmapper.Config{}
		ctx    = context.Background()
	)

	opts := map[string]string{
		"dm.basesize":    "10gb",
		"dm.metadatadev": "/meta_dev",
		"dm.datadev":     "/data_dev",
		"dm.thinpooldev": "/pool_dev",
		"dm.blocksize":   "100mb",
	}

	for key, value := range opts {
		err := applyStorageOpt(ctx, key, value, config)
		assert.NoErrorf(t, err, "failed to apply opt %q", key)
	}

	assert.Equal(t, "10gb", config.BaseImageSize)
	assert.EqualValues(t, 10*1024*1024*1024, config.BaseImageSizeBytes)
	assert.Equal(t, "/meta_dev", config.MetadataDevice)
	assert.Equal(t, "/data_dev", config.DataDevice)
	assert.Equal(t, "/pool_dev", config.PoolName)
	assert.Equal(t, "100mb", config.DataBlockSize)
	assert.EqualValues(t, 100*1024*1024/512, config.DataBlockSizeSectors)
}
