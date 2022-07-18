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
	"encoding/json"
	"fmt"
	"os"
	"time"

	"github.com/containerd/containerd"
	"github.com/containerd/containerd/content"
	"github.com/containerd/containerd/images"
	"github.com/containerd/containerd/mount"
	"github.com/containerd/containerd/snapshots"
	"github.com/containerd/continuity/fs"
	"github.com/hashicorp/go-multierror"
	"github.com/opencontainers/image-spec/identity"
	v1 "github.com/opencontainers/image-spec/specs-go/v1"
)

type imageProviderConfig struct {
	snapshotter  string
	pullOpts     []containerd.RemoteOpt
	snapshotOpts []snapshots.Opt
}

type imageVolumeProvider struct {
	config   imageProviderConfig
	client   *containerd.Client
	image    string
	target   string
	snapshot string
}

// ImageOpt allows setting optional properties of the provider.
type ImageOpt func(*imageProviderConfig)

// WithSnapshotter sets the snapshotter to pull images.
func WithSnapshotter(ss string) ImageOpt {
	return func(c *imageProviderConfig) {
		c.snapshotter = ss
	}
}

// WithPullOptions sets the snapshotter's pull options.
func WithPullOptions(opts ...containerd.RemoteOpt) ImageOpt {
	return func(c *imageProviderConfig) {
		c.pullOpts = opts
	}
}

// WithSnapshotOptions sets the snapshotter's snapshot options.
func WithSnapshotOptions(opts ...snapshots.Opt) ImageOpt {
	return func(c *imageProviderConfig) {
		c.snapshotOpts = opts
	}
}

func getV1ImageConfig(ctx context.Context, image containerd.Image) (*v1.Image, error) {
	var result v1.Image

	ic, err := image.Config(ctx)
	if err != nil {
		return nil, err
	}

	switch ic.MediaType {
	case v1.MediaTypeImageConfig, images.MediaTypeDockerSchema2Config:
		p, err := content.ReadBlob(ctx, image.ContentStore(), ic)
		if err != nil {
			return nil, err
		}
		if err := json.Unmarshal(p, &result); err != nil {
			return nil, err
		}
	default:
		return nil, fmt.Errorf("unknown type: %s", ic.MediaType)
	}
	return &result, nil
}

// FromImage returns a new provider to that expose the volumes on the given image.
func FromImage(client *containerd.Client, image, snapshot string, opts ...ImageOpt) Provider {
	p := imageVolumeProvider{
		config: imageProviderConfig{
			snapshotter: containerd.DefaultSnapshotter,
		},
		client:   client,
		image:    image,
		snapshot: snapshot,
	}

	for _, opt := range opts {
		opt(&p.config)
	}

	return &p
}

func (p *imageVolumeProvider) Name() string {
	return p.image
}

func (p *imageVolumeProvider) Delete(ctx context.Context) error {
	var retErr error

	if p.target != "" {
		retErr = mount.UnmountAll(p.target, 0)
	}

	ss := p.client.SnapshotService(p.config.snapshotter)
	err := ss.Remove(ctx, p.snapshot)
	if err != nil {
		retErr = multierror.Append(retErr, err)
	}
	defer ss.Close()

	return retErr
}

func (p *imageVolumeProvider) CreateVolumesUnder(ctx context.Context, tempDir string) ([]*Volume, error) {
	image, err := p.client.GetImage(ctx, p.image)
	if err != nil {
		return nil, err
	}

	unpacked, err := image.IsUnpacked(ctx, p.config.snapshotter)
	if err != nil {
		return nil, err
	}

	if !unpacked {
		err := image.Unpack(ctx, p.config.snapshotter)
		if err != nil {
			return nil, err
		}
	}

	config, err := getV1ImageConfig(ctx, image)
	if err != nil {
		return nil, err
	}

	if len(config.Config.Volumes) == 0 {
		return nil, nil
	}

	root, err := p.mountImage(ctx, tempDir)
	if err != nil {
		return nil, err
	}
	p.target = root

	result := make([]*Volume, 0, len(config.Config.Volumes))
	for path := range config.Config.Volumes {
		hostPath, err := fs.RootPath(root, path)
		if err != nil {
			return nil, err
		}
		result = append(result, &Volume{hostPath: hostPath, containerPath: path})
	}
	return result, nil
}

func (p *imageVolumeProvider) mountImage(ctx context.Context, tempDir string) (string, error) {
	image, err := p.client.GetImage(ctx, p.image)
	if err != nil {
		return "", err
	}

	ids, err := image.RootFS(ctx)
	if err != nil {
		return "", err
	}

	parent := identity.ChainID(ids).String()

	ss := p.client.SnapshotService(p.config.snapshotter)
	mounts, err := ss.View(ctx, p.snapshot, parent, snapshots.WithLabels(map[string]string{
		"containerd.io/gc.root": time.Now().UTC().Format(time.RFC3339),
	}))
	if err != nil {
		return "", err
	}
	defer ss.Close()

	target, err := os.MkdirTemp(tempDir, "mount")
	if err != nil {
		return "", err
	}
	err = mount.All(mounts, target)
	if err != nil {
		return "", err
	}

	return target, nil
}
