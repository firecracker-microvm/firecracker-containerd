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
	"os"
	"strings"
	"testing"

	"github.com/firecracker-microvm/firecracker-containerd/internal"
)

const runtimeConfigPath = "/etc/containerd/firecracker-runtime.json"

var defaultRuntimeConfig = Config{
	FirecrackerBinaryPath: "/usr/local/bin/firecracker",
	KernelImagePath:       "/var/lib/firecracker-containerd/runtime/default-vmlinux.bin",
	KernelArgs:            "ro console=ttyS0 noapic reboot=k panic=1 pci=off nomodules systemd.journald.forward_to_console systemd.unit=firecracker.target init=/sbin/overlay-init",
	RootDrive:             "/var/lib/firecracker-containerd/runtime/default-rootfs.img",
	CPUCount:              1,
	CPUTemplate:           "T2",
	LogLevel:              "Debug",
}

func defaultSnapshotterName() string {
	name := os.Getenv("FICD_SNAPSHOTTER")
	if name == "" || name == "naive" {
		return "firecracker-naive"
	}

	return name
}

func prepareIntegTest(t *testing.T, options ...func(*Config)) {
	t.Helper()

	internal.RequiresIsolation(t)

	err := writeRuntimeConfig(options...)
	if err != nil {
		t.Error(err)
	}
}

func writeRuntimeConfig(options ...func(*Config)) error {
	config := defaultRuntimeConfig
	for _, option := range options {
		option(&config)
	}

	file, err := os.Create(runtimeConfigPath)
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

func withJailer() func(*Config) {
	return func(c *Config) {
		c.JailerConfig.RuncBinaryPath = "/usr/local/bin/runc"
	}
}

var testNameToVMIDReplacer = strings.NewReplacer("/", "_")

func testNameToVMID(s string) string {
	return testNameToVMIDReplacer.Replace(s)
}
