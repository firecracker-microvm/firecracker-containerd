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

package internal

import (
	"context"

	"github.com/containerd/containerd/mount"
	"github.com/containerd/containerd/snapshots"
)

// SuccessfulSnapshotter mocks containerd snapshots.Snapshotter interface
// returning nil errors on calls.
type SuccessfulSnapshotter struct{}

// Stat mocks a successful remote call with a nil error.
func (s *SuccessfulSnapshotter) Stat(ctx context.Context, key string) (snapshots.Info, error) {
	return snapshots.Info{}, nil
}

// Update mocks a successful remote call with a nil error.
func (s *SuccessfulSnapshotter) Update(ctx context.Context, info snapshots.Info, fieldpaths ...string) (snapshots.Info, error) {
	return snapshots.Info{}, nil
}

// Usage mocks a successful remote call with a nil error.
func (s *SuccessfulSnapshotter) Usage(ctx context.Context, key string) (snapshots.Usage, error) {
	return snapshots.Usage{}, nil
}

// Mounts mocks a successful remote call with a nil error.
func (s *SuccessfulSnapshotter) Mounts(ctx context.Context, key string) ([]mount.Mount, error) {
	return []mount.Mount{}, nil
}

// Prepare mocks a successful remote call with a nil error.
func (s *SuccessfulSnapshotter) Prepare(ctx context.Context, key string, parent string, opts ...snapshots.Opt) ([]mount.Mount, error) {
	return []mount.Mount{}, nil
}

// View mocks a successful remote call with a nil error.
func (s *SuccessfulSnapshotter) View(ctx context.Context, key string, parent string, opts ...snapshots.Opt) ([]mount.Mount, error) {
	return []mount.Mount{}, nil
}

// Commit mocks a successful remote call with a nil error.
func (s *SuccessfulSnapshotter) Commit(ctx context.Context, name string, key string, opts ...snapshots.Opt) error {
	return nil
}

// Remove mocks a successful remote call with a nil error.
func (s *SuccessfulSnapshotter) Remove(ctx context.Context, key string) error {
	return nil
}

// Walk mocks a successful remote call with a nil error.
func (s *SuccessfulSnapshotter) Walk(ctx context.Context, fn snapshots.WalkFunc, filters ...string) error {
	return nil
}

// Close mocks a successful remote call with a nil error.
func (s *SuccessfulSnapshotter) Close() error {
	return nil
}
