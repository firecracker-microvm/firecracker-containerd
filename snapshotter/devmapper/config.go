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

package devmapper

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"os"

	"github.com/docker/go-units"
	"github.com/hashicorp/go-multierror"
	"github.com/pkg/errors"
)

// Config represents device mapper configuration loaded from file.
// Size units can be specified in human-readable string format (like "32KIB", "32GB", "32Tb")
type Config struct {
	// Device snapshotter root directory for metadata
	RootPath string `json:"root_path"`

	// Name for 'thin-pool' device to be used by snapshotter (without /dev/mapper/ prefix)
	PoolName string `json:"pool_name"`

	// Defines how much space to allocate when creating base image for container
	BaseImageSize      string `json:"base_image_size"`
	BaseImageSizeBytes uint64 `json:"-"`
}

// LoadConfig reads devmapper configuration file JSON format from disk
func LoadConfig(path string) (*Config, error) {
	if _, err := os.Stat(path); err != nil {
		if os.IsNotExist(err) {
			return nil, os.ErrNotExist
		}

		return nil, err
	}

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

	if err := config.Validate(); err != nil {
		return nil, err
	}

	return &config, nil
}

func (c *Config) parse() error {
	baseImageSize, err := units.RAMInBytes(c.BaseImageSize)
	if err != nil {
		return errors.Wrapf(err, "failed to parse base image size: '%s'", c.BaseImageSize)
	}

	c.BaseImageSizeBytes = uint64(baseImageSize)
	return nil
}

// Validate makes sure configuration fields are valid
func (c *Config) Validate() error {
	var result *multierror.Error

	if c.PoolName == "" {
		result = multierror.Append(result, fmt.Errorf("pool_name is required"))
	}

	if c.RootPath == "" {
		result = multierror.Append(result, fmt.Errorf("root_path is required"))
	}

	if c.BaseImageSize == "" {
		result = multierror.Append(result, fmt.Errorf("base_image_size is required"))
	}

	return result.ErrorOrNil()
}
