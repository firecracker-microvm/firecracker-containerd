// Copyright Amazon.com, Inc. or its affiliates. All Rights Reserved.
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

package config

import (
	"encoding/json"
	"io/ioutil"
	"os"

	"github.com/pkg/errors"

	"github.com/firecracker-microvm/firecracker-containerd/internal/debug"
	"github.com/firecracker-microvm/firecracker-containerd/proto"
	models "github.com/firecracker-microvm/firecracker-go-sdk/client/models"
)

const (
	// ConfigPathEnvName is the name of the environment variable used to
	// overwrite the default runtime config path
	ConfigPathEnvName  = "FIRECRACKER_CONTAINERD_RUNTIME_CONFIG_PATH"
	defaultConfigPath  = "/etc/containerd/firecracker-runtime.json"
	defaultKernelArgs  = "console=ttyS0 noapic reboot=k panic=1 pci=off nomodules rw"
	defaultFilesPath   = "/var/lib/firecracker-containerd/runtime/"
	defaultKernelPath  = defaultFilesPath + "default-vmlinux.bin"
	defaultRootfsPath  = defaultFilesPath + "default-rootfs.img"
	defaultCPUTemplate = models.CPUTemplateT2
	defaultShimBaseDir = "/var/lib/firecracker-containerd/shim-base"
	runcConfigPath     = "/etc/containerd/firecracker-runc-config.json"
)

// Config represents runtime configuration parameters
type Config struct {
	FirecrackerBinaryPath string   `json:"firecracker_binary_path"`
	KernelImagePath       string   `json:"kernel_image_path"`
	KernelArgs            string   `json:"kernel_args"`
	RootDrive             string   `json:"root_drive"`
	CPUTemplate           string   `json:"cpu_template"`
	LogLevels             []string `json:"log_levels"`
	HtEnabled             bool     `json:"ht_enabled"`
	// If a CreateVM call specifies no network interfaces and DefaultNetworkInterfaces is non-empty,
	// the VM will default to using the network interfaces as specified here. This is especially
	// useful when a CNI-based network interface is provided in DefaultNetworkInterfaces.
	DefaultNetworkInterfaces []proto.FirecrackerNetworkInterface `json:"default_network_interfaces"`
	// ShimBaseDir is the base directory which shim dirs will be created for each
	// VM. In addition to this if jailing is enabled the jail will also use this
	// directory.
	ShimBaseDir  string       `json:"shim_base_dir"`
	JailerConfig JailerConfig `json:"jailer"`

	DebugHelper *debug.Helper `json:"-"`
}

// JailerConfig houses a set of configurable values for jailing
// TODO: Add netns field
type JailerConfig struct {
	RuncBinaryPath string `json:"runc_binary_path"`
	RuncConfigPath string `json:"runc_config_path"`
}

// LoadConfig loads configuration from JSON file at 'path'
func LoadConfig(path string) (*Config, error) {
	if path == "" {
		path = os.Getenv(ConfigPathEnvName)
	}

	if path == "" {
		path = defaultConfigPath
	}

	data, err := ioutil.ReadFile(path)
	if err != nil {
		return nil, errors.Wrapf(err, "failed to read config from %q", path)
	}

	cfg := &Config{
		KernelArgs:      defaultKernelArgs,
		KernelImagePath: defaultKernelPath,
		RootDrive:       defaultRootfsPath,
		CPUTemplate:     string(defaultCPUTemplate),
		ShimBaseDir:     defaultShimBaseDir,
		JailerConfig: JailerConfig{
			RuncConfigPath: runcConfigPath,
		},
	}

	cfg.DebugHelper, err = debug.New(cfg.LogLevels...)
	if err != nil {
		return nil, err
	}

	if err := json.Unmarshal(data, cfg); err != nil {
		return nil, errors.Wrapf(err, "failed to unmarshal config from %q", path)
	}

	return cfg, nil
}
