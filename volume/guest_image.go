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
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/containerd/containerd"
	"github.com/containerd/containerd/cio"
	"github.com/containerd/containerd/containers"
	"github.com/containerd/containerd/identifiers"
	"github.com/containerd/containerd/oci"
	"github.com/firecracker-microvm/firecracker-containerd/runtime/firecrackeroci"
	"github.com/opencontainers/runtime-spec/specs-go"
)

const (
	vmBinDir        = "/usr/local/bin"
	containerBinDir = "/hostbin"

	vmVolumesDir        = "/volumes"
	containerVolumesDir = "/hostvolumes"
)

// GuestVolumeImageProvider expose volumes from in-VM snapshotters.
type GuestVolumeImageProvider struct {
	config   imageProviderConfig
	client   *containerd.Client
	cImage   containerd.Image
	image    string
	volumes  []string
	snapshot string
	vmID     string
	vmDir    string
}

// GuestVolumeImageInput is volume-init command's input.
type GuestVolumeImageInput struct {
	From    string
	To      string
	Volumes []string
}

// GuestVolumeImageOutput is volume-init command's output.
type GuestVolumeImageOutput struct {
	Error string
}

// FromGuestImage returns a new provider to that expose the volumes on the given image.
func FromGuestImage(client *containerd.Client, vmID, image, snapshot string, volumes []string, opts ...ImageOpt) *GuestVolumeImageProvider {
	p := GuestVolumeImageProvider{
		client:   client,
		image:    image,
		snapshot: snapshot,
		vmID:     vmID,
		volumes:  volumes,
	}
	p.config.snapshotter = containerd.DefaultSnapshotter

	for _, opt := range opts {
		opt(&p.config)
	}

	return &p
}

// Name of the provider.
func (p *GuestVolumeImageProvider) Name() string {
	return p.image
}

// pull the image.
func (p *GuestVolumeImageProvider) pull(ctx context.Context) error {
	remoteOpts := []containerd.RemoteOpt{
		containerd.WithPullUnpack,
		containerd.WithPullSnapshotter(p.config.snapshotter),
	}
	remoteOpts = append(remoteOpts, p.config.pullOpts...)
	image, err := p.client.Pull(ctx, p.image, remoteOpts...)
	if err != nil {
		return err
	}

	p.cImage = image
	return nil
}

func mountHostDirs(_ string) oci.SpecOpts {
	return func(_ context.Context, _ oci.Client, _ *containers.Container, s *oci.Spec) error {
		s.Mounts = append(s.Mounts,
			specs.Mount{
				Source:      vmBinDir,
				Destination: containerBinDir,
				Type:        "bind",
				Options:     []string{"bind", "ro"},
			},
			specs.Mount{
				Source:      vmVolumesDir,
				Destination: containerVolumesDir,
				Type:        "bind",
				Options:     []string{"bind"},
			},
		)
		return nil
	}
}

// copy files from the image by launching the container.
// The name must be unique within the VM and must be /[A-Z0-9a-z][A-Z0-9a-z._-]*/.
func (p *GuestVolumeImageProvider) copy(ctx context.Context, containerName string) error {
	err := identifiers.Validate(containerName)
	if err != nil {
		return err
	}
	container, err := p.client.NewContainer(ctx,
		containerName,
		containerd.WithSnapshotter(p.config.snapshotter),
		containerd.WithNewSnapshot(p.snapshot, p.cImage),
		containerd.WithNewSpec(
			firecrackeroci.WithVMID(p.vmID),
			oci.WithProcessArgs(filepath.Join(containerBinDir, "volume-init")),
			oci.WithProcessCwd("/"),
			mountHostDirs(containerName),
		),
	)
	if err != nil {
		return fmt.Errorf("failed to create container %q with %q: %w", containerName, p.config.snapshotter, err)
	}
	defer container.Delete(ctx, containerd.WithSnapshotCleanup)

	input := GuestVolumeImageInput{From: "/", To: filepath.Join(containerVolumesDir, containerName), Volumes: p.volumes}
	b, err := json.Marshal(&input)
	if err != nil {
		return err
	}

	stdin := bytes.NewBuffer(b)
	var stdout, stderr bytes.Buffer
	task, err := container.NewTask(ctx, cio.NewCreator(cio.WithStreams(stdin, &stdout, os.Stderr)))
	if err != nil {
		return err
	}
	defer task.Delete(ctx)

	exitCh, err := task.Wait(ctx)
	if err != nil {
		return err
	}

	err = task.Start(ctx)
	if err != nil {
		return err
	}

	err = task.CloseIO(ctx, containerd.WithStdinCloser)
	if err != nil {
		return err
	}

	select {
	case status := <-exitCh:
		if err := status.Error(); err != nil {
			return err
		}

		_, err := task.Delete(ctx)
		if err != nil {
			return err
		}

		code := status.ExitCode()
		if code != 0 {
			return fmt.Errorf(
				"failed to copy files from %q: stdout=%q, stderr=%q, code=%d",
				p.image,
				stdout.String(), stderr.String(), status.ExitCode(),
			)
		}
	case <-ctx.Done():
		return ctx.Err()
	}

	p.vmDir = filepath.Join(vmVolumesDir, containerName)
	return nil
}

// CreateVolumesUnder creates volumes under the given directory.
func (p *GuestVolumeImageProvider) CreateVolumesUnder(_ context.Context, _ string) ([]*Volume, error) {
	if p.vmDir == "" {
		return nil, errors.New("call Copy() beforehand")
	}

	result := make([]*Volume, 0, len(p.volumes))
	for _, path := range p.volumes {
		result = append(result, &Volume{
			vmPath:        filepath.Join(p.vmDir, path),
			containerPath: path,
		})
	}
	return result, nil
}

// Delete all resources made by the provider.
func (*GuestVolumeImageProvider) Delete(_ context.Context) error {
	return nil
}
