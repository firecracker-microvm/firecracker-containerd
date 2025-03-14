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
	"errors"

	"github.com/containerd/containerd/mount"
	"github.com/containerd/containerd/snapshots"
)

// FailingSnapshotter mocks containerd snapshots.Snapshotter interface
// return non-nil errors on calls.
type FailingSnapshotter struct{}

// Stat mocks a failing remote call with a non-nil error.
func (s *FailingSnapshotter) Stat(_ context.Context, _ string) (snapshots.Info, error) {
	return snapshots.Info{}, errors.New("mock Stat error from remote snapshotter")
}

// Update mocks a failing remote call with a non-nil error.
func (s *FailingSnapshotter) Update(_ context.Context, _ snapshots.Info, _ ...string) (snapshots.Info, error) {
	return snapshots.Info{}, errors.New("mock Update error from remote snapshotter")
}

// Usage mocks a failing remote call with a non-nil error.
func (s *FailingSnapshotter) Usage(_ context.Context, _ string) (snapshots.Usage, error) {
	return snapshots.Usage{}, errors.New("mock Usage error from remote snapshotter")
}

// Mounts mocks a failing remote call with a non-nil error.
func (s *FailingSnapshotter) Mounts(_ context.Context, _ string) ([]mount.Mount, error) {
	return []mount.Mount{}, errors.New("mock Mounts error from remote snapshotter")
}

// Prepare mocks a failing remote call with a non-nil error.
func (s *FailingSnapshotter) Prepare(_ context.Context, _ string, _ string, _ ...snapshots.Opt) ([]mount.Mount, error) {
	return []mount.Mount{}, errors.New("mock Prepare error from remote snapshotter")
}

// View mocks a failing remote call with a non-nil error.
func (s *FailingSnapshotter) View(_ context.Context, _ string, _ string, _ ...snapshots.Opt) ([]mount.Mount, error) {
	return []mount.Mount{}, errors.New("mock View error from remote snapshotter")
}

// Commit mocks a failing remote call with a non-nil error.
func (s *FailingSnapshotter) Commit(_ context.Context, _ string, _ string, _ ...snapshots.Opt) error {
	return errors.New("mock Commit error from remote snapshotter")
}

// Remove mocks a failing remote call with a non-nil error.
func (s *FailingSnapshotter) Remove(_ context.Context, _ string) error {
	return errors.New("mock Remove error from remote snapshotter")
}

// Walk mocks a failing remote call with a non-nil error.
func (s *FailingSnapshotter) Walk(_ context.Context, _ snapshots.WalkFunc, _ ...string) error {
	return errors.New("mock Walk error from remote snapshotter")
}

// Close mocks a failing remote call with a non-nil error.
func (s *FailingSnapshotter) Close() error {
	return errors.New("mock Close error from remote snapshotter")
}

// Cleanup mocks a failing remote call with a non-nil error.
func (s *FailingSnapshotter) Cleanup(_ context.Context) error {
	return errors.New("mock Cleanup error from remote snapshotter")
}
