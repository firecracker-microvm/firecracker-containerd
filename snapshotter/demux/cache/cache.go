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

package cache

import (
	"context"

	"github.com/containerd/containerd/snapshots"
	"github.com/firecracker-microvm/firecracker-containerd/snapshotter/demux/proxy"
)

// SnapshotterProvider defines a snapshotter fetch function.
type SnapshotterProvider = func(context.Context, string) (*proxy.RemoteSnapshotter, error)

// Cache defines the interface for a snapshotter caching mechanism.
type Cache interface {
	// Retrieves the snapshotter from the underlying cache using the provided
	// fetch function if the snapshotter is not currently cached.
	Get(ctx context.Context, key string, fetch SnapshotterProvider) (*proxy.RemoteSnapshotter, error)

	// RemoveAll removes the snapshot by the provided key from all cached snapshotters.
	RemoveAll(ctx context.Context, key string) error

	// WalkAll applies the provided function across all cached snapshotters.
	WalkAll(ctx context.Context, fn snapshots.WalkFunc, filters ...string) error

	// CleanupAll issues a cleanup call to all cached snapshotters.
	CleanupAll(ctx context.Context) error

	// Closes the snapshotter and removes it from the cache.
	Evict(key string) error

	// Releases the cache's internal resources and closes any cached snapshotters.
	Close() error

	// Lists keys present in the cache.
	List() []string
}
