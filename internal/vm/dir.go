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

package vm

import (
	"context"
	"io"
	"os"
	"path/filepath"

	"github.com/containerd/containerd/identifiers"
	"github.com/containerd/containerd/runtime/v2/shim"
	"github.com/containerd/fifo"
	"github.com/pkg/errors"
	"golang.org/x/sys/unix"

	"github.com/firecracker-microvm/firecracker-containerd/internal"
	"github.com/firecracker-microvm/firecracker-containerd/internal/bundle"
)

// ShimDir holds files, sockets and FIFOs scoped to a single shim managing the
// VM with the given VMID. It is unique per-VM and containerd namespace.
func ShimDir(shimBaseDir, namespace, vmID string) (Dir, error) {
	return shimDir(shimBaseDir, namespace, vmID)
}

func shimDir(varRunDir, namespace, vmID string) (Dir, error) {
	if err := identifiers.Validate(namespace); err != nil {
		return "", errors.Wrap(err, "invalid namespace")
	}

	if err := identifiers.Validate(vmID); err != nil {
		return "", errors.Wrap(err, "invalid vm id")
	}

	resolvedVarRunDir, err := filepath.EvalSymlinks(varRunDir)
	if err != nil {
		return "", errors.Wrapf(err, "failed evaluating any symlinks in path %q", varRunDir)
	}

	return Dir(filepath.Join(resolvedVarRunDir, namespace, vmID)), nil
}

// Dir represents the root of a firecracker-containerd VM directory, which
// holds various files, sockets and FIFOs used during VM runtime.
type Dir string

// RootPath returns the top-level directory of the VM dir
func (d Dir) RootPath() string {
	return string(d)
}

// Mkdir will mkdir the RootPath with correct permissions, or no-op if it
// already exists
func (d Dir) Mkdir() error {
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

// OpenLogFifo opens the shim's log fifo as WriteOnly
func (d Dir) OpenLogFifo(requestCtx context.Context) (io.ReadWriteCloser, error) {
	return fifo.OpenFifo(requestCtx, d.LogFifoPath(), unix.O_WRONLY|unix.O_NONBLOCK, 0200)
}

// LogStartPath returns the path to the file for storing
// firecracker logs after the microVM is started and until
// it is Offloaded
func (d Dir) LogStartPath() string {
	return filepath.Join(d.RootPath(), internal.LogPathNameStart)
}

// LogLoadPath returns the path to the file for storing
// firecracker logs after the microVM is loaded from a snapshot
func (d Dir) LogLoadPath() string {
	return filepath.Join(d.RootPath(), internal.LogPathNameLoad)
}

// FirecrackerSockPath returns the path to the unix socket at which the firecracker VMM
// services its API
func (d Dir) FirecrackerSockPath() string {
	return filepath.Join(d.RootPath(), internal.FirecrackerSockName)
}

// FirecrackerSockRelPath returns the path to FirecrackerSockPath relative to the
// current working directory
func (d Dir) FirecrackerSockRelPath() (string, error) {
	return relPathTo(d.FirecrackerSockPath())
}

// FirecrackerVSockPath returns the path to the vsock unix socket that the runtime uses
// to communicate with the VM agent.
func (d Dir) FirecrackerVSockPath() string {
	return filepath.Join(d.RootPath(), internal.FirecrackerVSockName)
}

// FirecrackerVSockRelPath returns the path to FirecrackerVSockPath relative to the
// current working directory
func (d Dir) FirecrackerVSockRelPath() (string, error) {
	return relPathTo(d.FirecrackerVSockPath())
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
func (d Dir) BundleLink(containerID string) (bundle.Dir, error) {
	if err := identifiers.Validate(containerID); err != nil {
		return "", errors.Wrapf(err, "invalid container id %q", containerID)
	}

	return bundle.Dir(filepath.Join(d.RootPath(), containerID)), nil
}

// CreateBundleLink creates the BundleLink by symlinking to the provided bundle dir
func (d Dir) CreateBundleLink(containerID string, bundleDir bundle.Dir) error {
	path, err := d.BundleLink(containerID)
	if err != nil {
		return err
	}

	return createSymlink(bundleDir.RootPath(), path.RootPath(), "bundle")
}

// CreateAddressLink creates a symlink from the VM dir to the bundle dir for the shim address file.
// This symlink is read by containerd.
// CreateAddressLink assumes that CreateBundleLink has been called.
func (d Dir) CreateAddressLink(containerID string) error {
	path, err := d.BundleLink(containerID)
	if err != nil {
		return err
	}

	return createSymlink(d.AddrFilePath(), path.AddrFilePath(), "shim address file")
}

// CreateShimLogFifoLink creates a symlink from the bundleDir of the provided container.
// CreateAddressLink assumes that CreateBundleLink has been called.
func (d Dir) CreateShimLogFifoLink(containerID string) error {
	path, err := d.BundleLink(containerID)
	if err != nil {
		return err
	}

	return createSymlink(path.LogFifoPath(), d.LogFifoPath(), "shim log fifo")
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

func relPathTo(absPath string) (string, error) {
	cwd, err := os.Getwd()
	if err != nil {
		return "", errors.Wrapf(err, "failed to get current working directory")
	}

	relPath, err := filepath.Rel(cwd, absPath)
	if err != nil {
		return "", errors.Wrapf(err, "failed to get relative path from %q to %q", cwd, absPath)
	}

	return relPath, nil
}
