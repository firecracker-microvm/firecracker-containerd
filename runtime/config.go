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

package main

import (
	"encoding/json"
	"io/ioutil"
	"os"

	"github.com/pkg/errors"
)

const (
	configPathEnvName = "FIRECRACKER_CONTAINERD_RUNTIME_CONFIG_PATH"
	defaultConfigPath = "/etc/containerd/firecracker-runtime.json"
	defaultSocketPath = "./firecracker.sock"
)

// Config represents runtime configuration parameters
type Config struct {
	FirecrackerBinaryPath string            `json:"firecracker_binary_path"`
	KernelImagePath       string            `json:"kernel_image_path"`
	KernelArgs            string            `json:"kernel_args"`
	RootDrive             string            `json:"root_drive"`
	CPUCount              int               `json:"cpu_count"`
	CPUTemplate           string            `json:"cpu_template"`
	AdditionalDrives      map[string]string `json:"additional_drives"`
	LogFifo               string            `json:"log_fifo"`
	LogLevel              string            `json:"log_level"`
	MetricsFifo           string            `json:"metrics_fifo"`
	HtEnabled             bool              `json:"ht_enabled"`
	Debug                 bool              `json:"debug"`
}

// LoadConfig loads configuration from JSON file at 'path'
func LoadConfig(path string) (*Config, error) {
	if path == "" {
		path = os.Getenv(configPathEnvName)
	}

	if path == "" {
		path = defaultConfigPath
	}

	data, err := ioutil.ReadFile(path)
	if err != nil {
		return nil, errors.Wrapf(err, "failed to read config from %q", path)
	}

	var cfg Config
	if err := json.Unmarshal(data, &cfg); err != nil {
		return nil, errors.Wrapf(err, "failed to unmarshal config from %q", path)
	}

	return &cfg, nil
}
