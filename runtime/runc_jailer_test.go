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

package main

import (
	"context"
	"io/ioutil"
	"os"
	"path/filepath"
	"syscall"
	"testing"

	"github.com/firecracker-microvm/firecracker-go-sdk"
	models "github.com/firecracker-microvm/firecracker-go-sdk/client/models"
	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/firecracker-microvm/firecracker-containerd/config"
	"github.com/firecracker-microvm/firecracker-containerd/internal"
)

func TestBuildJailedRootHandler_Isolated(t *testing.T) {
	internal.RequiresIsolation(t)
	dir, err := ioutil.TempDir("", "TestBuildJailedRootHandler")
	require.NoError(t, err, "failed to create temporary directory")

	defer os.RemoveAll(dir)
	kernelImagePath := filepath.Join(dir, "kernel-image")
	kernelImageFd, err := os.OpenFile(kernelImagePath, os.O_CREATE, 0600)
	require.NoError(t, err, "failed to create kernel image")
	defer kernelImageFd.Close()

	rootDrivePath := filepath.Join(dir, "root-drive")
	rootDriveFd, err := os.OpenFile(rootDrivePath, os.O_CREATE, 0600)
	require.NoError(t, err, "failed to create kernel image")
	defer rootDriveFd.Close()

	firecrackerPath := filepath.Join(dir, "firecracker")
	firecrackerFd, err := os.OpenFile(firecrackerPath, os.O_CREATE, 0600)
	require.NoError(t, err, "failed to create firecracker")
	defer firecrackerFd.Close()

	l := logrus.NewEntry(logrus.New())
	runcConfig := runcJailerConfig{
		OCIBundlePath:  dir,
		RuncBinPath:    "bin-path",
		RuncConfigPath: "./firecracker-runc-config.json.example",
		UID:            123,
		GID:            456,
	}
	vmID := "foo"
	jailer, err := newRuncJailer(context.Background(), l, vmID, runcConfig)
	require.NoError(t, err, "failed to create runc jailer")

	cfg := config.Config{
		FirecrackerBinaryPath: firecrackerPath,
		KernelImagePath:       kernelImagePath,
		RootDrive:             rootDrivePath,
	}
	machineConfig := firecracker.Config{
		SocketPath:      "/path/to/api.socket",
		KernelImagePath: kernelImagePath,
		Drives: []models.Drive{
			{
				PathOnHost:   firecracker.String(rootDrivePath),
				IsRootDevice: firecracker.Bool(true),
				IsReadOnly:   firecracker.Bool(true),
			},
		},
	}
	handler := jailer.BuildJailedRootHandler(&cfg, &machineConfig, vmID)

	machine := firecracker.Machine{
		Cfg: machineConfig,
	}
	err = handler.Fn(context.Background(), &machine)
	assert.NoError(t, err, "jailed handler failed to run")

	_, err = os.Stat(filepath.Join(dir, "config.json"))
	assert.NoError(t, err, "failed to copy runc config")

	_, err = os.Stat(filepath.Join(dir, "rootfs"))
	assert.NoError(t, err, "failed to create rootfs")

	_, err = os.Stat(filepath.Join(dir, "rootfs", filepath.Base(kernelImagePath)))
	assert.NoError(t, err, "failed to create kernel image")

	_, err = os.Stat(filepath.Join(dir, "rootfs", filepath.Base(rootDrivePath)))
	assert.NoError(t, err, "failed to create root drive")
}

func TestMkdirAllWithPermissions_Isolated(t *testing.T) {
	// requires isolation so we can change uid/gid of files
	internal.RequiresIsolation(t)

	tmpdir, err := ioutil.TempDir("", "TestMkdirAllWithPermissions")
	require.NoError(t, err, "failed to create temporary directory")

	existingPath := filepath.Join(tmpdir, "exists")
	existingMode := os.FileMode(0700)
	err = os.Mkdir(existingPath, existingMode)
	require.NoError(t, err, "failed to create existing part of test directory")

	nonExistingPath := filepath.Join(existingPath, "nonexistent")
	newMode := os.FileMode(0755)
	newuid := uint32(123)
	newgid := uint32(456)
	err = mkdirAllWithPermissions(nonExistingPath, newMode, newuid, newgid)
	require.NoError(t, err, "failed to mkdirAllWithPermissions")

	existingPathStat, err := os.Stat(existingPath)
	require.NoError(t, err, "failed to stat pre-existing path")
	assert.Equal(t, existingMode, existingPathStat.Mode().Perm())
	assert.Equal(t, uint32(os.Getuid()), existingPathStat.Sys().(*syscall.Stat_t).Uid)
	assert.Equal(t, uint32(os.Getgid()), existingPathStat.Sys().(*syscall.Stat_t).Gid)

	newlyCreatedPathStat, err := os.Stat(nonExistingPath)
	require.NoError(t, err, "failed to stat newly created path")
	assert.Equal(t, newMode, newlyCreatedPathStat.Mode().Perm())
	assert.Equal(t, newuid, newlyCreatedPathStat.Sys().(*syscall.Stat_t).Uid)
	assert.Equal(t, newgid, newlyCreatedPathStat.Sys().(*syscall.Stat_t).Gid)
}
