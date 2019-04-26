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

package vm

import (
	"os"
	"path/filepath"

	"github.com/containerd/containerd/runtime/v2/shim"
	"github.com/firecracker-microvm/firecracker-containerd/internal"
	"github.com/firecracker-microvm/firecracker-containerd/internal/bundle"
	"github.com/pkg/errors"
)

// Dir represents the root of a firecracker-containerd VM directory, which
// holds various files, sockets and FIFOs used during VM runtime.
type Dir string

// RootPath returns the top-level directory of the VM dir
func (d Dir) RootPath() string {
	return string(d)
}

// Create will mkdir the RootPath with correct permissions, or no-op if it
// already exists
func (d Dir) Create() error {
	return os.MkdirAll(d.RootPath(), 0700)
}

// AddrFilePath returns the path to the shim address file as found in the vmDir. The
// address file holds the abstract unix socket path at which the shim serves its API.
func (d Dir) AddrFilePath() string {
	return filepath.Join(d.RootPath(), internal.ShimAddrFileName)
}

// LogFifoPath returns the path to the FIFO for writing shim logs
func (d Dir) LogFifoPath() string {
	return filepath.Join(d.RootPath(), internal.ShimLogFifoName)
}

// FirecrackerSockPath returns the path to the unix socket at which the firecracker VMM
// services its API
func (d Dir) FirecrackerSockPath() string {
	return filepath.Join(d.RootPath(), internal.FirecrackerSockName)
}

// FirecrackerLogFifoPath returns the path to the FIFO at which the firecracker VMM writes
// its logs
func (d Dir) FirecrackerLogFifoPath() string {
	return filepath.Join(d.RootPath(), internal.FirecrackerLogFifoName)
}

// FirecrackerMetricsFifoPath returns the path to the FIFO at which the firecracker VMM writes
// metrics
func (d Dir) FirecrackerMetricsFifoPath() string {
	return filepath.Join(d.RootPath(), internal.FirecrackerMetricsFifoName)
}

// BundleLink returns the path to the symlink to the bundle dir for a given container running
// inside the VM of this vm dir.
func (d Dir) BundleLink(containerID string) bundle.Dir {
	return bundle.Dir(filepath.Join(d.RootPath(), containerID))
}

// CreateBundleLink creates the BundleLink by symlinking to the provided bundle dir
func (d Dir) CreateBundleLink(containerID string, bundleDir bundle.Dir) error {
	return createSymlink(bundleDir.RootPath(), d.BundleLink(containerID).RootPath(), "bundle")
}

// CreateAddressLink creates a symlink from the VM dir to the bundle dir for the shim address file.
// This symlink is read by containerd.
// CreateAddressLink assumes that CreateBundleLink has been called.
func (d Dir) CreateAddressLink(containerID string) error {
	return createSymlink(d.AddrFilePath(), d.BundleLink(containerID).AddrFilePath(), "shim address file")
}

// CreateShimLogFifoLink creates a symlink from the bundleDir of the provided container.
// CreateAddressLink assumes that CreateBundleLink has been called.
func (d Dir) CreateShimLogFifoLink(containerID string) error {
	return createSymlink(d.BundleLink(containerID).LogFifoPath(), d.LogFifoPath(), "shim log fifo")
}

// WriteAddress will write the actual address file in the VM dir.
func (d Dir) WriteAddress(shimSocketAddress string) error {
	return shim.WriteAddress(d.AddrFilePath(), shimSocketAddress)
}

func createSymlink(oldPath, newPath string, errMsgName string) error {
	err := os.Symlink(oldPath, newPath)
	if err != nil {
		return errors.Wrapf(err, `failed to create %s symlink from "%s"->"%s"`, errMsgName, newPath, oldPath)
	}

	return nil
}
