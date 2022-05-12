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

package integtest

import (
	"encoding/json"
	"os"

	"github.com/firecracker-microvm/firecracker-containerd/config"
)

const runtimeConfigPath = "/etc/containerd/firecracker-runtime.json"

// DefaultRuntimeConfig represents a simple firecracker-containerd configuration.
var DefaultRuntimeConfig = config.Config{
	FirecrackerBinaryPath: "/usr/local/bin/firecracker",
	KernelImagePath:       "/var/lib/firecracker-containerd/runtime/default-vmlinux.bin",
	KernelArgs:            "ro console=ttyS0 noapic reboot=k panic=1 pci=off nomodules systemd.unified_cgroup_hierarchy=0 systemd.journald.forward_to_console systemd.log_color=false systemd.unit=firecracker.target init=/sbin/overlay-init",
	RootDrive:             "/var/lib/firecracker-containerd/runtime/default-rootfs.img",
	LogLevels:             []string{"debug"},
	ShimBaseDir:           ShimBaseDir(),
	JailerConfig: config.JailerConfig{
		RuncBinaryPath: "/usr/local/bin/runc",
		RuncConfigPath: "/etc/containerd/firecracker-runc-config.json",
	},
}

func writeRuntimeConfig(options ...func(*config.Config)) error {
	config := DefaultRuntimeConfig
	for _, option := range options {
		option(&config)
	}

	file, err := os.OpenFile(runtimeConfigPath, os.O_CREATE|os.O_WRONLY, 0600)
	if err != nil {
		return err
	}
	defer file.Close()

	bytes, err := json.Marshal(config)
	if err != nil {
		return err
	}

	_, err = file.Write(bytes)
	if err != nil {
		return err
	}

	return nil
}
