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
	"fmt"
	"sync"

	"github.com/containerd/containerd/errdefs"
	"github.com/containerd/containerd/snapshots"
	"github.com/firecracker-microvm/firecracker-containerd/snapshotter/demux/proxy"
	"github.com/hashicorp/go-multierror"
)

// SnapshotterCache implements a read, write protected cache mechanism
// for keyed snapshotters.
type SnapshotterCache struct {
	mutex        *sync.RWMutex
	snapshotters map[string]*proxy.RemoteSnapshotter
}

// NewSnapshotterCache creates a new instance with an empty cache.
func NewSnapshotterCache() *SnapshotterCache {
	return &SnapshotterCache{&sync.RWMutex{}, make(map[string]*proxy.RemoteSnapshotter)}
}

// Get fetches and caches the snapshotter for a given key.
func (cache *SnapshotterCache) Get(ctx context.Context, key string, fetch SnapshotterProvider) (*proxy.RemoteSnapshotter, error) {
	cache.mutex.RLock()
	snapshotter, ok := cache.snapshotters[key]
	cache.mutex.RUnlock()

	if !ok {
		cache.mutex.Lock()
		defer cache.mutex.Unlock()

		snapshotter, ok = cache.snapshotters[key]
		if !ok {
			newSnapshotter, err := fetch(ctx, key)
			if err != nil {
				return nil, err
			}

			cache.snapshotters[key] = newSnapshotter
			snapshotter = newSnapshotter
		}
	}
	return snapshotter, nil
}

// RemoveAll removes the snapshot by the provided key from all cached snapshotters.
func (cache *SnapshotterCache) RemoveAll(ctx context.Context, key string) error {
	cache.mutex.RLock()
	defer cache.mutex.RUnlock()

	var allErr error

	for namespace, snapshotter := range cache.snapshotters {
		if err := snapshotter.Remove(ctx, key); err != nil && err != errdefs.ErrNotFound {
			allErr = multierror.Append(allErr, fmt.Errorf("failed to remove snapshot on snapshotter[%s]: %w", namespace, err))
		}
	}
	return allErr
}

// WalkAll applies the provided function to all cached snapshotters.
func (cache *SnapshotterCache) WalkAll(ctx context.Context, fn snapshots.WalkFunc, filters ...string) error {
	cache.mutex.RLock()
	defer cache.mutex.RUnlock()

	var allErr error

	for namespace, snapshotter := range cache.snapshotters {
		if err := snapshotter.Walk(ctx, fn, filters...); err != nil {
			allErr = multierror.Append(allErr, fmt.Errorf("failed to walk function on snapshotter[%s]: %w", namespace, err))
		}
	}
	return allErr
}

// CleanupAll issues a cleanup to all cached snapshotters.
func (cache *SnapshotterCache) CleanupAll(ctx context.Context) error {
	cache.mutex.RLock()
	defer cache.mutex.RUnlock()

	var allErr error

	for namespace, snapshotter := range cache.snapshotters {
		if err := snapshotter.Cleanup(ctx); err != nil {
			allErr = multierror.Append(allErr, fmt.Errorf("failed cleanup function on snapshotter[%s]: %w", namespace, err))
		}
	}
	return allErr
}

// Evict removes a cached snapshotter for a given key.
func (cache *SnapshotterCache) Evict(key string) error {
	cache.mutex.RLock()
	remoteSnapshotter, ok := cache.snapshotters[key]
	cache.mutex.RUnlock()

	if !ok {
		return fmt.Errorf("snapshotter %s not found in cache", key)
	}
	cache.mutex.Lock()
	defer cache.mutex.Unlock()

	err := remoteSnapshotter.Close()
	delete(cache.snapshotters, key)
	return err
}

// Close calls Close on all cached remote snapshotters.
func (cache *SnapshotterCache) Close() error {
	var compiledErr error
	cache.mutex.RLock()
	defer cache.mutex.RUnlock()
	for _, remoteSnapshotter := range cache.snapshotters {
		if err := remoteSnapshotter.Close(); err != nil {
			compiledErr = multierror.Append(compiledErr, err)
		}
	}
	return compiledErr
}

// List returns keys of a snapshotter cache.
func (cache *SnapshotterCache) List() []string {
	cache.mutex.RLock()
	defer cache.mutex.RUnlock()
	keys := make([]string, 0, len(cache.snapshotters))
	for k := range cache.snapshotters {
		keys = append(keys, k)
	}

	return keys
}
