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
func (s *SuccessfulSnapshotter) Stat(_ context.Context, _ string) (snapshots.Info, error) {
	return snapshots.Info{}, nil
}

// Update mocks a successful remote call with a nil error.
func (s *SuccessfulSnapshotter) Update(_ context.Context, _ snapshots.Info, _ ...string) (snapshots.Info, error) {
	return snapshots.Info{}, nil
}

// Usage mocks a successful remote call with a nil error.
func (s *SuccessfulSnapshotter) Usage(_ context.Context, _ string) (snapshots.Usage, error) {
	return snapshots.Usage{}, nil
}

// Mounts mocks a successful remote call with a nil error.
func (s *SuccessfulSnapshotter) Mounts(_ context.Context, _ string) ([]mount.Mount, error) {
	return []mount.Mount{}, nil
}

// Prepare mocks a successful remote call with a nil error.
func (s *SuccessfulSnapshotter) Prepare(_ context.Context, _ string, _ string, _ ...snapshots.Opt) ([]mount.Mount, error) {
	return []mount.Mount{}, nil
}

// View mocks a successful remote call with a nil error.
func (s *SuccessfulSnapshotter) View(_ context.Context, _ string, _ string, _ ...snapshots.Opt) ([]mount.Mount, error) {
	return []mount.Mount{}, nil
}

// Commit mocks a successful remote call with a nil error.
func (s *SuccessfulSnapshotter) Commit(_ context.Context, _ string, _ string, _ ...snapshots.Opt) error {
	return nil
}

// Remove mocks a successful remote call with a nil error.
func (s *SuccessfulSnapshotter) Remove(_ context.Context, _ string) error {
	return nil
}

// Walk mocks a successful remote call with a nil error.
func (s *SuccessfulSnapshotter) Walk(_ context.Context, _ snapshots.WalkFunc, _ ...string) error {
	return nil
}

// Close mocks a successful remote call with a nil error.
func (s *SuccessfulSnapshotter) Close() error {
	return nil
}

// Cleanup mocks a successful remote call with a nil error.
func (s *SuccessfulSnapshotter) Cleanup(_ context.Context) error {
	return nil
}
