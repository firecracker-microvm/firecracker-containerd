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
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/containerd/containerd"
	"github.com/containerd/containerd/cio"
	"github.com/pkg/errors"

	"github.com/firecracker-microvm/firecracker-containerd/config"
	"github.com/firecracker-microvm/firecracker-containerd/internal"
)

const runtimeConfigPath = "/etc/containerd/firecracker-runtime.json"
const shimBaseDir = "/srv/firecracker_containerd_tests"

var defaultRuntimeConfig = config.Config{
	FirecrackerBinaryPath: "/usr/local/bin/firecracker",
	KernelImagePath:       "/var/lib/firecracker-containerd/runtime/default-vmlinux.bin",
	KernelArgs:            "ro console=ttyS0 noapic reboot=k panic=1 pci=off nomodules systemd.journald.forward_to_console systemd.log_color=false systemd.unit=firecracker.target init=/sbin/overlay-init",
	RootDrive:             "/var/lib/firecracker-containerd/runtime/default-rootfs.img",
	CPUTemplate:           "T2",
	LogLevel:              "Debug",
	Debug:                 true,
	ShimBaseDir:           shimBaseDir,
	JailerConfig: config.JailerConfig{
		RuncBinaryPath: "/usr/local/bin/runc",
		RuncConfigPath: "/etc/containerd/firecracker-runc-config.json",
	},
}

func defaultSnapshotterName() string {
	if name := os.Getenv("FICD_SNAPSHOTTER"); name != "" {
		return name
	}

	return "devmapper"
}

func prepareIntegTest(t *testing.T, options ...func(*config.Config)) {
	t.Helper()

	internal.RequiresIsolation(t)

	err := writeRuntimeConfig(runtimeConfigPath, options...)
	if err != nil {
		t.Error(err)
	}
}

func writeRuntimeConfig(path string, options ...func(*config.Config)) error {
	config := defaultRuntimeConfig
	for _, option := range options {
		option(&config)
	}

	file, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY, 0600)
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

var testNameToVMIDReplacer = strings.NewReplacer("/", "_")

func testNameToVMID(s string) string {
	return testNameToVMIDReplacer.Replace(s)
}

type commandResult struct {
	stdout   string
	stderr   string
	exitCode uint32
}

func runTask(ctx context.Context, c containerd.Container) (*commandResult, error) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer

	task, err := c.NewTask(ctx, cio.NewCreator(cio.WithStreams(nil, &stdout, &stderr)))
	if err != nil {
		return nil, err
	}

	exitCh, err := task.Wait(ctx)
	if err != nil {
		return nil, err
	}

	err = task.Start(ctx)
	if err != nil {
		return nil, err
	}

	select {
	case exitStatus := <-exitCh:
		if err := exitStatus.Error(); err != nil {
			return nil, err
		}

		_, err := task.Delete(ctx)
		if err != nil {
			return nil, err
		}

		return &commandResult{
			stdout:   stdout.String(),
			stderr:   stderr.String(),
			exitCode: exitStatus.ExitCode(),
		}, nil
	case <-ctx.Done():
		return nil, errors.New("context cancelled")
	}
}

// setupBareMetalTest will run firecracker-containerd, the runtime shim, and
// snapshotter. This will return the cleanup function needed to kill each
// process.
func setupBareMetalTest(dir, uuid string, options ...func(*config.Config)) (func(), error) {
	if err := os.MkdirAll(filepath.Join(dir, "firecracker-containerd", "containerd"), 0644); err != nil {
		return nil, errors.Wrap(err, "failed to create shim base directory")
	}
	cleanupFns := func() {
		os.RemoveAll(dir)
	}

	if err := os.MkdirAll(filepath.Join(dir, "state"), 0644); err != nil {
		return cleanupFns, errors.Wrap(err, "failed to create state folder")
	}

	devmapperPath := filepath.Join(dir, "devmapper")
	if err := os.MkdirAll(devmapperPath, 0644); err != nil {
		return cleanupFns, errors.Wrap(err, "failed to create devmapper folder")
	}

	binPath := filepath.Join(dir, "bin")
	if err := os.MkdirAll(binPath, 0644); err != nil {
		return cleanupFns, errors.Wrap(err, "failed to create bin folder")
	}

	paths := os.Getenv("PATH")
	os.Setenv("PATH", binPath+":"+paths)

	runtimeConfigPath := filepath.Join(dir, "firecracker-runtime.json")
	if err := writeRuntimeConfig(runtimeConfigPath, options...); err != nil {
		return cleanupFns, errors.Wrap(err, "failed to create runtime config")
	}

	thinPoolName := uuid
	thinPoolCmd := exec.Command("../tools/thinpool.sh", "reset", thinPoolName)
	thinPoolCmd.Stdout = os.Stdout
	thinPoolCmd.Stderr = os.Stderr

	if err := thinPoolCmd.Run(); err != nil {
		return cleanupFns, errors.Wrap(err, "failed to create thin pool")
	}

	cleanupFns = func() {
		os.RemoveAll(dir)
		exec.Command("../tools/thinpool.sh", "remove", thinPoolName).Run()
	}

	os.Setenv(config.ConfigPathEnvName, runtimeConfigPath)
	os.Setenv("INSTALLROOT", dir)
	os.Setenv("FIRECRACKER_CONTAINERD_RUNTIME_DIR", dir)

	cmd := exec.Command("make")
	cmd.Dir = filepath.Join(cmd.Dir, "..")
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return cleanupFns, errors.Wrap(err, "failed running make")
	}

	cmd = exec.Command("make", "install")
	cmd.Dir = filepath.Join(cmd.Dir, "..")
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return cleanupFns, errors.Wrap(err, "failed running make install")
	}

	cmd = exec.Command("make", "install-default-vmlinux")
	cmd.Dir = filepath.Join(cmd.Dir, "..")
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return cleanupFns, errors.Wrap(err, "failed running make install-default-vmlinux")
	}

	cmd = exec.Command("make", "image")
	cmd.Dir = filepath.Join(cmd.Dir, "..")
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return cleanupFns, errors.Wrap(err, "failed running make image")
	}

	cmd = exec.Command("make", "install-default-rootfs")
	cmd.Dir = filepath.Join(cmd.Dir, "..")
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return cleanupFns, errors.Wrap(err, "failed running make install-default-rootfs")
	}

	const configTomlFormat = `disabled_plugins = ["cri"]
root = "%s"
state = "%s/state"
[grpc]
  address = "%s/containerd.sock"
[plugins]
  [plugins.devmapper]
    pool_name = "fcci--vg-%s"
    base_image_size = "10GB"
    root_path = "%s/devmapper"
[debug]
  level = "debug"`

	configToml := fmt.Sprintf(configTomlFormat, dir, dir, dir, thinPoolName, dir)
	if err := ioutil.WriteFile(filepath.Join(dir, "config.toml"), []byte(configToml), 0644); err != nil {
		return cleanupFns, errors.Wrap(err, "faiiled to write config.toml")
	}

	cpCmd := exec.Command("cp", "firecracker-runc-config.json.example", filepath.Join(dir, "config.json"))
	cpCmd.Stdout = os.Stdout
	cpCmd.Stderr = os.Stderr
	if err := cpCmd.Run(); err != nil {
		return cleanupFns, errors.Wrap(err, "failed to copy runc config")
	}

	cpCmd = exec.Command("cp", "../_submodules/runc/runc", binPath)
	cpCmd.Stdout = os.Stdout
	cpCmd.Stderr = os.Stderr
	if err := cpCmd.Run(); err != nil {
		return cleanupFns, errors.Wrap(err, "failed to copy runc")
	}

	containerdBin := filepath.Join(binPath, "firecracker-containerd")
	containerdProcess := exec.Command(containerdBin, "--config", filepath.Join(dir, "config.toml"))
	containerdProcess.Stdout = os.Stdout
	containerdProcess.Stderr = os.Stderr
	if err := containerdProcess.Start(); err != nil {
		return cleanupFns, errors.Wrap(err, "failed to run containerd")
	}

	cleanupFns = func() {
		if containerdProcess != nil && containerdProcess.Process != nil {
			containerdProcess.Process.Kill()
		}
		exec.Command("../tools/thinpool.sh", "remove", thinPoolName).Run()
		os.RemoveAll(dir)
	}

	containerdAddress := filepath.Join(dir, "containerd.sock")
	ctrFetch := exec.Command(
		filepath.Join(binPath, "firecracker-ctr"),
		"--address",
		containerdAddress,
		"content",
		"fetch",
		"docker.io/library/alpine:3.10.1")
	ctrFetch.Stdout = os.Stdout
	ctrFetch.Stderr = os.Stderr
	if err := ctrFetch.Run(); err != nil {
		return cleanupFns, errors.Wrap(err, "failed to fetch alpine image")
	}

	return cleanupFns, nil
}
