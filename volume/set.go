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

package volume

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/containerd/containerd/containers"
	"github.com/containerd/containerd/mount"
	"github.com/containerd/containerd/oci"
	"github.com/containerd/continuity/fs"
	"github.com/firecracker-microvm/firecracker-containerd/proto"
	"github.com/hashicorp/go-multierror"
	"github.com/opencontainers/runtime-spec/specs-go"
)

const (
	vmVolumePath = "/vmv"
	fsType       = "ext4"
)

// Set is a set of volumes.
type Set struct {
	volumes map[string]*Volume
	tempDir string
}

// NewSet returns a new volume set.
func NewSet() *Set {
	return NewSetWithTempDir(os.TempDir())
}

// NewSetWithTempDir returns a new volume set and creates all temporary files under the tempDir.
func NewSetWithTempDir(tempDir string) *Set {
	return &Set{volumes: make(map[string]*Volume), tempDir: tempDir}
}

// Add a volume to the set.
func (vs *Set) Add(v *Volume) {
	vs.volumes[v.name] = v
}

func (vs *Set) createDiskImage(ctx context.Context, size int64) (path string, retErr error) {
	f, err := os.CreateTemp(vs.tempDir, "createDiskImage")
	if err != nil {
		retErr = err
		return
	}
	defer func() {
		// The file must be closed even in the success case.
		err := f.Close()
		if err != nil {
			retErr = multierror.Append(retErr, err)
		}
		// But the file must not be deleted in the success case.
		if retErr != nil {
			err = os.Remove(path)
			if err != nil {
				retErr = multierror.Append(retErr, err)
			}
		}
	}()

	err = f.Truncate(size)
	if err != nil {
		retErr = err
		return
	}

	out, err := exec.CommandContext(ctx, "mkfs."+fsType, "-F", f.Name()).CombinedOutput()
	if err != nil {
		retErr = fmt.Errorf("failed to execute mkfs.%s: %s: %w", fsType, out, err)
		return
	}

	path = f.Name()
	return
}

func mountDiskImage(source, target string) error {
	return mount.All([]mount.Mount{{Type: fsType, Source: source, Options: []string{"loop"}}}, target)
}

// PrepareDriveMount returns a FirecrackerDriveMount that could be used with CreateVM.
func (vs *Set) PrepareDriveMount(ctx context.Context, size int64) (dm *proto.FirecrackerDriveMount, retErr error) {
	path, err := vs.createDiskImage(ctx, size)
	if err != nil {
		retErr = err
		return
	}
	defer func() {
		// The file must not be deleted in the success case.
		if retErr != nil {
			err := os.Remove(path)
			if err != nil {
				retErr = multierror.Append(retErr, err)
			}
		}
	}()

	dir, err := os.MkdirTemp(vs.tempDir, "PrepareDriveMount")
	if err != nil {
		retErr = err
		return
	}
	defer func() {
		err := os.Remove(dir)
		if err != nil {
			retErr = multierror.Append(retErr, err)
		}
	}()

	err = mountDiskImage(path, dir)
	if err != nil {
		retErr = err
		return
	}
	defer func() {
		err := mount.Unmount(dir, 0)
		if err != nil {
			retErr = multierror.Append(retErr, err)
		}
	}()

	for _, v := range vs.volumes {
		path := filepath.Join(dir, v.name)
		if v.hostPath == "" {
			continue
		}
		err := fs.CopyDir(path, v.hostPath)
		if err != nil {
			retErr = fmt.Errorf("failed to copy volume %q: %w", v.name, err)
			return
		}
	}

	dm = &proto.FirecrackerDriveMount{
		HostPath:       path,
		VMPath:         vmVolumePath,
		FilesystemType: fsType,
		IsWritable:     true,
	}
	return
}

// Mount is used to expose volumes to containers.
type Mount struct {
	// Source is the name of a volume.
	Source string
	// Destination is the path inside the container where the volume is mounted.
	Destination string
	// ReadOnly is true if the volume is mounted as read-only.
	ReadOnly bool
}

// WithMounts expose given volumes to the container.
func (vs *Set) WithMounts(mountpoints []Mount) (oci.SpecOpts, error) {
	mounts := []specs.Mount{}

	for _, mp := range mountpoints {
		v, ok := vs.volumes[mp.Source]
		if !ok {
			return nil, fmt.Errorf("failed to find volume %q", mp.Source)
		}

		options := []string{"bind"}
		if mp.ReadOnly {
			options = append(options, "ro")
		}

		mounts = append(mounts, specs.Mount{
			// TODO: for volumes that are provided by the guest (e.g. in-VM snapshotters)
			// We may be able to have bind-mounts from in-VM snapshotters' mount points.
			Source:      filepath.Join(vmVolumePath, v.name),
			Destination: mp.Destination,
			Type:        "bind",
			Options:     options,
		})
	}

	return func(ctx context.Context, client oci.Client, container *containers.Container, s *oci.Spec) error {
		s.Mounts = append(s.Mounts, mounts...)
		return nil
	}, nil
}
