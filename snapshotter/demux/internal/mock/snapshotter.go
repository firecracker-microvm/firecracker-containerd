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

package mock

import (
	"context"

	"github.com/containerd/containerd/mount"
	"github.com/containerd/containerd/snapshots"
)

// Snapshotter is a mock whose behavior can be configured via options.
type Snapshotter struct {
	StatValue snapshots.Info
	StatError error

	UpdateValue snapshots.Info
	UpdateError error

	UsageValue snapshots.Usage
	UsageError error

	MountsValue []mount.Mount
	MountsError error

	PrepareValue []mount.Mount
	PrepareError error

	ViewValue []mount.Mount
	ViewError error

	CommitError error

	RemoveError error

	WalkError error

	CloseError error

	CleanupError error
}

// Stat mocks a snapshotter stat call.
func (s *Snapshotter) Stat(ctx context.Context, key string) (snapshots.Info, error) {
	return s.StatValue, s.StatError
}

// Update mocks a snapshotter update call.
func (s *Snapshotter) Update(ctx context.Context, info snapshots.Info, fieldpaths ...string) (snapshots.Info, error) {
	return s.UpdateValue, s.UpdateError
}

// Usage mocks a snapshotter usage call.
func (s *Snapshotter) Usage(ctx context.Context, key string) (snapshots.Usage, error) {
	return s.UsageValue, s.UsageError
}

// Mounts mocks a snapshotter mounts call.
func (s *Snapshotter) Mounts(ctx context.Context, key string) ([]mount.Mount, error) {
	return s.MountsValue, s.MountsError
}

// Prepare mocks a snapshotter prepare call.
func (s *Snapshotter) Prepare(ctx context.Context, key string, parent string, opts ...snapshots.Opt) ([]mount.Mount, error) {
	return s.PrepareValue, s.PrepareError
}

// View mocks a snapshotter view call.
func (s *Snapshotter) View(ctx context.Context, key string, parent string, opts ...snapshots.Opt) ([]mount.Mount, error) {
	return s.ViewValue, s.ViewError
}

// Commit mocks a snapshotter commit call.
func (s *Snapshotter) Commit(ctx context.Context, name string, key string, opts ...snapshots.Opt) error {
	return s.CommitError
}

// Remove mocks a snapshotter remove call.
func (s *Snapshotter) Remove(ctx context.Context, key string) error {
	return s.RemoveError
}

// Walk mocks a snapshotter walk call.
func (s *Snapshotter) Walk(ctx context.Context, fn snapshots.WalkFunc, filters ...string) error {
	return s.WalkError
}

// Close mocks a snapshotter close call.
func (s *Snapshotter) Close() error {
	return s.CloseError
}

// Cleanup mocks a snapshotter cleanup call.
func (s *Snapshotter) Cleanup(ctx context.Context) error {
	return s.CleanupError
}
