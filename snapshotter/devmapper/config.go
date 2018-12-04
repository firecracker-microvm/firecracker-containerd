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

	"github.com/docker/go-units"
	"github.com/pkg/errors"
)

const (
	configPathEnvName = "FIRECRACKER_CONTAINERD_DEVMAPPER_SNAPSHOTTER_CONFIG_PATH"
	defaultConfigPath = "/etc/containerd/firecracker-devmapper-snapshotter.json"
)

// Config represents device mapper configuration loaded from file.
// Size units might be specified in human-readable string format (like "32KIB", "32GB", "32Tb")
type Config struct {
	// Use loopback devices when creating thin-pool. Loopback is slow and should not be
	// used in production, but easy to setup and might be useful for debugging and testing.
	UseLoopback bool `json:"use_loopback"`

	// Size of a filesystem image used by thin-pool as data device
	LoopbackDataFileSize      string `json:"loopback_data_file_size"`
	LoopbackDataFileSizeBytes int64  `json:"-"`

	// Size of a filesystem image used by thin-pool as metadata device
	LoopbackMetadataFileSize      string `json:"loopback_metadata_file_size"`
	LoopbackMetadataFileSizeBytes int64  `json:"-"`

	// The size of allocation chunks in data file.
	// Must be between 128 sectors (64KB) and 2097152 sectors (1GB) and a mutlipole of 128 sectors (64KB)
	// Block size can't be changed after pool created.
	// See https://www.kernel.org/doc/Documentation/device-mapper/thin-provisioning.txt
	DataBlockSize        string `json:"data_block_size_sectors"`
	DataBlockSizeSectors int64  `json:"-"`

	// Defines how much space to allocate when creating base image for container
	BaseImageSize      string `json:"base_image_size"`
	BaseImageSizeBytes int64  `json:"-"`
}

// LoadConfig reads devmapper configuration file JSON format from disk
func LoadConfig(path string) (*Config, error) {
	if path == "" {
		path = os.Getenv(configPathEnvName)
	}

	if path == "" {
		path = defaultConfigPath
	}

	data, err := ioutil.ReadFile(path)
	if err != nil {
		return nil, err
	}

	config := Config{}
	if err := json.Unmarshal(data, &config); err != nil {
		return nil, err
	}

	if err := config.validate(); err != nil {
		return nil, err
	}

	return &config, nil
}

func (c *Config) validate() error {
	const (
		sectorSize       = 512
		dataBlockMinSize = 128
		dataBlockMaxSize = 2097152
	)

	if blockSize, err := units.RAMInBytes(c.DataBlockSize); err != nil {
		return errors.Wrap(err, "failed to parse data block size")
	} else {
		c.DataBlockSizeSectors = blockSize / sectorSize
	}

	if c.DataBlockSizeSectors < dataBlockMinSize || c.DataBlockSizeSectors > dataBlockMaxSize {
		return errors.Errorf("block size should be between %d and %d", dataBlockMinSize, dataBlockMaxSize)
	}

	if c.DataBlockSizeSectors%dataBlockMinSize != 0 {
		return errors.Errorf("block size should be mutlipole of %d sectors", dataBlockMinSize)
	}

	var err error

	if c.UseLoopback {
		if c.LoopbackDataFileSizeBytes, err = units.RAMInBytes(c.LoopbackDataFileSize); err != nil {
			return errors.Wrap(err, "failed to parse loopback data file size")
		}

		if c.LoopbackMetadataFileSizeBytes, err = units.RAMInBytes(c.LoopbackMetadataFileSize); err != nil {
			return errors.Wrap(err, "failed to parse loopback metadata file size")
		}
	}

	if c.BaseImageSizeBytes, err = units.RAMInBytes(c.BaseImageSize); err != nil {
		return errors.Wrap(err, "failed to parse base image size")
	}

	return nil
}
