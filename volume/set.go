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

// Package volume provides functionality for volume management.
package volume

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/containerd/containerd/containers"
	"github.com/containerd/containerd/errdefs"
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
	volumes   map[string]*Volume
	mounts    map[string][]Mount
	providers map[string]Provider
	tempDir   string
	runtime   string
	volumeDir string
}

// Provider provides volumes from different sources.
type Provider interface {
	// Name of the provider.
	Name() string

	// CreateVolumesUnder creates volumes under the given directory.
	CreateVolumesUnder(ctx context.Context, tempDir string) ([]*Volume, error)

	// Delete all resources made by the provider.
	Delete(ctx context.Context) error
}

// NewSet returns a new volume set.
func NewSet(runtime string) *Set {
	return NewSetWithTempDir(runtime, os.TempDir())
}

// NewSetWithTempDir returns a new volume set and creates all temporary files under the tempDir.
func NewSetWithTempDir(runtime, tempDir string) *Set {
	return &Set{
		runtime:   runtime,
		mounts:    make(map[string][]Mount),
		providers: make(map[string]Provider),
		volumes:   make(map[string]*Volume),
		tempDir:   tempDir,
	}
}

// Add a volume to the set.
func (vs *Set) Add(v *Volume) {
	vs.volumes[fmt.Sprintf("named_%s", v.name)] = v
}

// AddFrom adds volumes from the given provider.
func (vs *Set) AddFrom(_ context.Context, vp Provider) error {
	if _, exists := vs.providers[vp.Name()]; exists {
		return fmt.Errorf("failed to add %q: %w", vp.Name(), errdefs.ErrAlreadyExists)
	}
	vs.providers[vp.Name()] = vp
	return nil
}

func (vs *Set) copyToHostFromProvider(ctx context.Context, vp Provider) error {
	dir, err := os.MkdirTemp(vs.tempDir, "copyToHostFromProvider")
	if err != nil {
		return err
	}
	volumes, err := vp.CreateVolumesUnder(ctx, dir)
	if err != nil {
		return err
	}

	mounts := make([]Mount, 0, len(volumes))
	for _, v := range volumes {
		if v.name == "" {
			index := len(vs.volumes)
			v.name = fmt.Sprintf("anon_%d", index)
		}
		vs.volumes[v.name] = v

		mounts = append(mounts, Mount{key: v.name, Destination: v.containerPath, ReadOnly: false})
	}
	vs.mounts[vp.Name()] = mounts
	return nil
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

// PrepareDirectory creates a directory that have volumes.
func (vs *Set) PrepareDirectory(ctx context.Context) (retErr error) {
	dir, err := os.MkdirTemp(vs.tempDir, "PrepareDirectory")
	if err != nil {
		retErr = err
		return
	}
	defer func() {
		if retErr != nil {
			err := os.Remove(dir)
			if err != nil {
				retErr = multierror.Append(retErr, err)
			}
		}
	}()

	err = vs.copyToHost(ctx, dir)
	if err != nil {
		retErr = err
		return
	}

	vs.volumeDir = dir
	return
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

	err = vs.copyToHost(ctx, dir)
	if err != nil {
		retErr = err
		return
	}

	dm = &proto.FirecrackerDriveMount{
		HostPath:       path,
		VMPath:         vmVolumePath,
		FilesystemType: fsType,
		IsWritable:     true,
	}
	vs.volumeDir = vmVolumePath
	return
}

// PrepareInGuest prepares volumes inside the VM.
func (vs *Set) PrepareInGuest(ctx context.Context, container string) error {
	for _, provider := range vs.providers {
		gp, guest := provider.(*GuestVolumeImageProvider)
		if !guest {
			continue
		}

		err := gp.pull(ctx)
		if err != nil {
			return err
		}

		err = gp.copy(ctx, container)
		if err != nil {
			return err
		}

		err = vs.copyToHostFromProvider(ctx, provider)
		if err != nil {
			return err
		}
	}
	return nil
}

func (vs *Set) copyToHost(ctx context.Context, dir string) error {
	for _, provider := range vs.providers {
		_, guest := provider.(*GuestVolumeImageProvider)
		if guest {
			continue
		}

		err := vs.copyToHostFromProvider(ctx, provider)
		if err != nil {
			return err
		}
	}

	for _, v := range vs.volumes {
		path, err := fs.RootPath(dir, v.name)
		if err != nil {
			return err
		}
		if v.hostPath == "" {
			continue
		}
		err = fs.CopyDir(path, v.hostPath)
		if err != nil {
			return fmt.Errorf("failed to copy volume %q: %w", v.name, err)
		}
	}
	return nil
}

// Mount is used to expose volumes to containers.
type Mount struct {
	// Source is the name of a volume.
	Source string
	// Destination is the path inside the container where the volume is mounted.
	Destination string
	// ReadOnly is true if the volume is mounted as read-only.
	ReadOnly bool

	key string
}

// WithMounts expose given volumes to the container.
func (vs *Set) WithMounts(mountpoints []Mount) (oci.SpecOpts, error) {
	mounts := []specs.Mount{}

	for _, mp := range mountpoints {
		key := mp.key
		if key == "" {
			key = fmt.Sprintf("named_%s", mp.Source)
		}
		v, ok := vs.volumes[key]
		if !ok {
			return nil, fmt.Errorf("failed to find volume %q", mp.Source)
		}

		options := []string{"bind"}
		if mp.ReadOnly {
			options = append(options, "ro")
		}

		var source string
		if v.vmPath != "" {
			source = v.vmPath
		} else {
			source = filepath.Join(vs.volumeDir, v.name)
		}

		mounts = append(mounts, specs.Mount{
			// TODO: for volumes that are provided by the guest (e.g. in-VM snapshotters)
			// We may be able to have bind-mounts from in-VM snapshotters' mount points.
			Source:      source,
			Destination: mp.Destination,
			Type:        "bind",
			Options:     options,
		})
	}

	return func(_ context.Context, _ oci.Client, _ *containers.Container, s *oci.Spec) error {
		s.Mounts = append(s.Mounts, mounts...)
		return nil
	}, nil
}

// WithMountsFromProvider exposes volumes from the provider.
func (vs *Set) WithMountsFromProvider(name string) (oci.SpecOpts, error) {
	_, exists := vs.providers[name]
	if !exists {
		return nil, fmt.Errorf("failed to find volume %q: %w", name, errdefs.ErrNotFound)
	}

	return vs.WithMounts(vs.mounts[name])
}
