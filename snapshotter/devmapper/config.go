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

	"github.com/docker/go-units"
	"github.com/hashicorp/go-multierror"
	"github.com/pkg/errors"

	"github.com/firecracker-microvm/firecracker-containerd/snapshotter/pkg/dmsetup"
)

const (
	// See https://www.kernel.org/doc/Documentation/device-mapper/thin-provisioning.txt for details
	dataBlockMinSize = 128
	dataBlockMaxSize = 2097152
)

var (
	errInvalidBlockSize      = errors.Errorf("block size should be between %d and %d", dataBlockMinSize, dataBlockMaxSize)
	errInvalidBlockAlignment = errors.Errorf("block size should be multiple of %d sectors", dataBlockMinSize)
)

// Config represents device mapper configuration loaded from file.
// Size units can be specified in human-readable string format (like "32KIB", "32GB", "32Tb")
type Config struct {
	// Device snapshotter root directory for metadata
	RootPath string `json:"root_path"`

	// Name for 'thin-pool' device to be used by snapshotter (without /dev/mapper/ prefix)
	PoolName string `json:"pool_name"`

	// Path to data volume to be used by thin-pool
	DataDevice string `json:"data_device"`

	// Path to metadata volume to be used by thin-pool
	MetadataDevice string `json:"meta_device"`

	// The size of allocation chunks in data file.
	// Must be between 128 sectors (64KB) and 2097152 sectors (1GB) and a multiple of 128 sectors (64KB)
	// Block size can't be changed after pool created.
	// See https://www.kernel.org/doc/Documentation/device-mapper/thin-provisioning.txt
	DataBlockSize        string `json:"data_block_size"`
	DataBlockSizeSectors uint32 `json:"-"`

	// Defines how much space to allocate when creating base image for container
	BaseImageSize      string `json:"base_image_size"`
	BaseImageSizeBytes uint64 `json:"-"`
}

// LoadConfig reads devmapper configuration file JSON format from disk
func LoadConfig(path string) (*Config, error) {
	data, err := ioutil.ReadFile(path)
	if err != nil {
		return nil, errors.Wrap(err, "failed to read file")
	}

	config := Config{}
	if err := json.Unmarshal(data, &config); err != nil {
		return nil, errors.Wrapf(err, "failed to unmarshal data at '%s'", path)
	}

	if err := config.parse(); err != nil {
		return nil, err
	}

	if err := config.validate(); err != nil {
		return nil, err
	}

	return &config, nil
}

func (c *Config) parse() error {
	var result *multierror.Error

	if blockSize, err := units.RAMInBytes(c.DataBlockSize); err != nil {
		result = multierror.Append(result, errors.Wrapf(err, "failed to parse data block size: %q", c.DataBlockSize))
	} else {
		c.DataBlockSizeSectors = uint32(blockSize / dmsetup.SectorSize)
	}

	if baseImageSize, err := units.RAMInBytes(c.BaseImageSize); err != nil {
		result = multierror.Append(result, errors.Wrapf(err, "failed to parse base image size: %q", c.BaseImageSize))
	} else {
		c.BaseImageSizeBytes = uint64(baseImageSize)
	}

	return result.ErrorOrNil()
}

func (c *Config) validate() error {
	var result *multierror.Error

	strChecks := []struct {
		field string
		name  string
	}{
		{c.PoolName, "pool_name"},
		{c.RootPath, "root_path"},
		{c.DataDevice, "data_device"},
		{c.MetadataDevice, "meta_device"},
	}

	for _, check := range strChecks {
		if check.field == "" {
			result = multierror.Append(result, errors.Errorf("%s is empty", check.name))
		}
	}

	if c.DataBlockSizeSectors < dataBlockMinSize || c.DataBlockSizeSectors > dataBlockMaxSize {
		result = multierror.Append(result, errInvalidBlockSize)
	}

	if c.DataBlockSizeSectors%dataBlockMinSize != 0 {
		result = multierror.Append(result, errInvalidBlockAlignment)
	}

	return result.ErrorOrNil()
}
