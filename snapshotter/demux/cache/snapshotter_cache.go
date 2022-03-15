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

	"github.com/containerd/containerd/snapshots"
	"github.com/hashicorp/go-multierror"
)

// SnapshotterCache implements a read, write protected cache mechanism
// for keyed snapshotters.
type SnapshotterCache struct {
	mutex        *sync.Mutex
	snapshotters map[string]snapshots.Snapshotter
}

// NewSnapshotterCache creates a new instance with an empty cache.
func NewSnapshotterCache() *SnapshotterCache {
	return &SnapshotterCache{&sync.Mutex{}, make(map[string]snapshots.Snapshotter)}
}

// Get fetches and caches the snapshotter for a given key.
func (cache *SnapshotterCache) Get(ctx context.Context, key string, fetch SnapshotterProvider) (snapshots.Snapshotter, error) {
	snapshotter, ok := cache.snapshotters[key]

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

// Evict removes a cached snapshotter for a given key.
func (cache *SnapshotterCache) Evict(key string) error {
	snapshotter, ok := cache.snapshotters[key]

	if !ok {
		return fmt.Errorf("snapshotter %s not found in cache", key)
	}
	cache.mutex.Lock()
	defer cache.mutex.Unlock()

	err := snapshotter.Close()
	delete(cache.snapshotters, key)
	return err
}

// Close calls Close on all cached remote snapshotters.
func (cache *SnapshotterCache) Close() error {
	var compiledErr error
	for _, snapshotter := range cache.snapshotters {
		if err := snapshotter.Close(); err != nil {
			compiledErr = multierror.Append(compiledErr, err)
		}
	}
	return compiledErr
}
