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

package bundle

import (
	"encoding/json"
	"io/ioutil"
	"os"
	"path/filepath"

	"github.com/firecracker-microvm/firecracker-containerd/internal"
	"github.com/firecracker-microvm/firecracker-containerd/runtime/firecrackeroci"
	"github.com/pkg/errors"
)

const (
	vmBundleRoot = "/container"
)

// VMBundleDir returns the directory inside a VM at which the bundle directory for
// the provided taskID should exist.
func VMBundleDir(taskID string) Dir {
	return Dir(filepath.Join(vmBundleRoot, taskID))
}

// Dir represents the root of a container bundle dir. It's just a type wrapper
// around a string, where the string is the path of the bundle dir.
type Dir string

// RootPath returns the top-level directory of the bundle dir
func (d Dir) RootPath() string {
	return string(d)
}

// AddrFilePath is the path to the address file as found in the bundleDir.
// Even though the shim address is set per-VM, not per-container, containerd expects
// to find the shim addr file in the bundle dir, so we still have to create it or
// symlink it to the shimAddrFilePath
func (d Dir) AddrFilePath() string {
	return filepath.Join(d.RootPath(), internal.ShimAddrFileName)
}

// LogFifoPath is a path to a FIFO for writing shim logs as found in the bundleDir.
// It is the path created by containerd for us, the shimLogFifoPath is just a symlink to one.
func (d Dir) LogFifoPath() string {
	return filepath.Join(d.RootPath(), internal.ShimLogFifoName)
}

// RootfsPath returns the path to the "rootfs" dir of the bundle
func (d Dir) RootfsPath() string {
	return filepath.Join(d.RootPath(), internal.BundleRootfsName)
}

// OCIConfigPath returns the path to the bundle's config.json
func (d Dir) OCIConfigPath() string {
	return filepath.Join(d.RootPath(), internal.OCIConfigName)
}

// OCIConfig returns a OCIConfig object wrapper around the bundle
// config.json
func (d Dir) OCIConfig() *OCIConfig {
	return &OCIConfig{path: d.OCIConfigPath()}
}

// OCIConfig is wrapper around a bundle's config.json that provided
// basic file operations on it with appropriate permissions.
type OCIConfig struct {
	path string
}

// File opens the config.json as read-only
func (c *OCIConfig) File() (*os.File, error) {
	f, err := os.Open(c.path)
	if err != nil {
		return nil, errors.Wrapf(err, "failed to open OCI config file %s", c.path)
	}

	return f, nil
}

// Bytes returns the bytes of config.json
func (c *OCIConfig) Bytes() ([]byte, error) {
	f, err := ioutil.ReadFile(c.path)
	if err != nil {
		return nil, errors.Wrapf(err, "failed to read OCI config file %s", c.path)
	}

	return f, nil
}

// Write will create or overwrite the config.json with the provided bytes
func (c *OCIConfig) Write(contents []byte) error {
	err := ioutil.WriteFile(c.path, contents, 0700)
	if err != nil {
		return errors.Wrapf(err, "failed to write OCI config file %s", c.path)
	}

	return nil
}

// VMID returns the firecracker VM ID set by the client in the OCI config Annotations section, if any.
func (c *OCIConfig) VMID() (string, error) {
	ociConfigFile, err := c.File()
	if err != nil {
		return "", err
	}

	defer ociConfigFile.Close()
	var ociConfig struct {
		Annotations map[string]string `json:"annotations,omitempty"`
	}

	if err := json.NewDecoder(ociConfigFile).Decode(&ociConfig); err != nil {
		return "", errors.Wrapf(err, "failed to parse Annotations section of OCI config file %s", c.path)
	}

	// This will return empty string if the key is not present in the OCI config, which the caller can decide
	// how to deal with
	return ociConfig.Annotations[firecrackeroci.VMIDAnnotationKey], nil
}
