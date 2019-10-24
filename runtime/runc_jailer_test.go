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
	"context"
	"io/ioutil"
	"os"
	"path/filepath"
	"testing"

	"github.com/firecracker-microvm/firecracker-go-sdk"
	models "github.com/firecracker-microvm/firecracker-go-sdk/client/models"
	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/firecracker-microvm/firecracker-containerd/internal"
)

func TestBuildJailedRootHandler_Isolated(t *testing.T) {
	internal.RequiresIsolation(t)
	runcConfigPath = "./firecracker-runc-config.json.example"
	dir, err := ioutil.TempDir("./", "TestBuildJailedRootHandler")
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
	jailer, err := newRuncJailer(context.Background(), l, dir, "bin-path", 123, 456)
	require.NoError(t, err, "failed to create runc jailer")

	cfg := Config{
		FirecrackerBinaryPath: firecrackerPath,
		KernelImagePath:       kernelImagePath,
		RootDrive:             rootDrivePath,
	}
	socketPath := "/path/to/api.socket"
	vmID := "foo"
	handler := jailer.BuildJailedRootHandler(&cfg, &socketPath, vmID)

	machine := firecracker.Machine{
		Cfg: firecracker.Config{
			SocketPath:      socketPath,
			KernelImagePath: kernelImagePath,
			Drives: []models.Drive{
				{
					PathOnHost:   firecracker.String(rootDrivePath),
					IsRootDevice: firecracker.Bool(true),
				},
			},
		},
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
