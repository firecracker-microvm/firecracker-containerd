// +build al2

// Copyright 2018-2020 Amazon.com, Inc. or its affiliates. All Rights Reserved.
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
	"crypto/rand"
	"encoding/base32"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/firecracker-microvm/firecracker-containerd/config"
)

const firecrackerTestBinary = "firecracker-v0.19.0"

func TestMultipleVMs_AL2(t *testing.T) {
	b := make([]byte, 16)
	rand.Read(b)
	uuid := base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(b)
	baseDir := filepath.Join(os.TempDir(), "shim-base")
	dir := filepath.Join(baseDir, uuid)
	cleanupFns, err := setupBareMetalTest(dir, uuid, func(cfg *config.Config) {
		cfg.ShimBaseDir = dir
		cfg.KernelImagePath = filepath.Join(dir, "default-vmlinux.bin")
		cfg.JailerConfig.RuncConfigPath = filepath.Join(dir, "config.json")
		cfg.JailerConfig.RuncBinaryPath = filepath.Join(dir, "bin", "runc")
		cfg.FirecrackerBinaryPath = filepath.Join("/usr/local/bin", firecrackerTestBinary)
	})
	defer func() {
		cleanupFns()
		// clean up network binaries that were generated when running make
		os.RemoveAll("../bin")
	}()
	require.NoError(t, err, "failed to bootstrap test")
	t.Run("multiple", func(t *testing.T) {
		testMultipleVMs_Isolated(t, filepath.Join(dir, "containerd.sock"), filepath.Join(dir, "default-rootfs.img"))
	})
}
