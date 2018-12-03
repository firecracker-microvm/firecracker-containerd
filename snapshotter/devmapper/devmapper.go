// Copyright 2018 Amazon.com, Inc. or its affiliates. All Rights Reserved.
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

package devmapper

import (
	"context"

	"github.com/containerd/containerd/mount"
	"github.com/containerd/containerd/snapshots"
)

type devmapper struct {
}

func NewSnapshotter(ctx context.Context) (snapshots.Snapshotter, error) {
	return &devmapper{}, nil
}

func (dm *devmapper) Stat(ctx context.Context, key string) (snapshots.Info, error) {
	panic("not implemented")
}

func (dm *devmapper) Update(ctx context.Context, info snapshots.Info, fieldpaths ...string) (snapshots.Info, error) {
	panic("not implemented")
}

func (dm *devmapper) Usage(ctx context.Context, key string) (snapshots.Usage, error) {
	panic("not implemented")
}

func (dm *devmapper) Mounts(ctx context.Context, key string) ([]mount.Mount, error) {
	panic("not implemented")
}

func (dm *devmapper) Prepare(ctx context.Context, key, parent string, opts ...snapshots.Opt) ([]mount.Mount, error) {
	panic("not implemented")
}

func (dm *devmapper) View(ctx context.Context, key, parent string, opts ...snapshots.Opt) ([]mount.Mount, error) {
	panic("not implemented")
}

func (dm *devmapper) Commit(ctx context.Context, name, key string, opts ...snapshots.Opt) error {
	panic("not implemented")
}

func (dm *devmapper) Remove(ctx context.Context, key string) error {
	panic("not implemented")
}

func (dm *devmapper) Walk(ctx context.Context, fn func(context.Context, snapshots.Info) error) error {
	panic("not implemented")
}

func (dm *devmapper) Close() error {
	panic("not implemented")
}
