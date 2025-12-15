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

// Package cache implements a cache of remote snapshotters used by the
// demux snapshotter for routing requests and service discovery.
package cache

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/containerd/containerd/snapshots"
	"github.com/firecracker-microvm/firecracker-containerd/snapshotter/demux/proxy"
	"github.com/hashicorp/go-multierror"
	log "github.com/sirupsen/logrus"
)

// SnapshotterProvider defines a snapshotter fetch function.
type SnapshotterProvider = func(context.Context, string) (*proxy.RemoteSnapshotter, error)

// RemoteSnapshotterCache implements a cache for remote snapshotters.
type RemoteSnapshotterCache struct {
	mutex        *sync.RWMutex
	snapshotters map[string]*proxy.RemoteSnapshotter

	fetch SnapshotterProvider

	reaper *sync.Once
	evict  chan string
	lease  EvictionPolicy

	stop chan struct{}
}

// SnapshotterCacheOption is a functional option that operates on a remote snapshotter cache.
type SnapshotterCacheOption func(*RemoteSnapshotterCache)

// NewRemoteSnapshotterCache creates a new instance with an empty cache and applies any provided caching options.
func NewRemoteSnapshotterCache(fetch SnapshotterProvider, opts ...SnapshotterCacheOption) *RemoteSnapshotterCache {
	c := &RemoteSnapshotterCache{
		mutex:        &sync.RWMutex{},
		snapshotters: make(map[string]*proxy.RemoteSnapshotter),
		fetch:        fetch,
		reaper:       &sync.Once{},
		stop:         make(chan struct{}),
	}

	for _, opt := range opts {
		opt(c)
	}

	return c
}

// EvictOnConnectionFailure is a caching option for evicting entries from the cache after a failed connection attempt.
func EvictOnConnectionFailure(dialer proxy.Dialer, frequency time.Duration) func(*RemoteSnapshotterCache) {
	return func(c *RemoteSnapshotterCache) {
		c.evict = make(chan string)
		c.lease = NewEvictOnConnectionFailurePolicy(c.evict, dialer, frequency, c.stop)
		c.startBackgroundReaper()
	}
}

func (c *RemoteSnapshotterCache) startBackgroundReaper() {
	reap := func() {
		for {
			s, ok := <-c.evict
			if !ok {
				break
			}
			if err := c.Evict(s); err != nil {
				log.WithField("context", "cache reaper").Error(err)
			}
		}
	}
	c.reaper.Do(func() {
		go reap()
	})
}

// List returns the keys of a snapshotter cache.
func (c *RemoteSnapshotterCache) List() []string {
	c.mutex.RLock()
	defer c.mutex.RUnlock()

	keys := make([]string, 0, len(c.snapshotters))
	for k := range c.snapshotters {
		keys = append(keys, k)
	}

	return keys
}

// Get fetches and caches the snapshotter for a given key.
func (c *RemoteSnapshotterCache) Get(ctx context.Context, key string) (*proxy.RemoteSnapshotter, error) {
	c.mutex.RLock()
	snapshotter, ok := c.snapshotters[key]
	c.mutex.RUnlock()

	if !ok {
		c.mutex.Lock()
		snapshotter, ok = c.snapshotters[key]
		c.mutex.Unlock()

		if !ok {
			newSnapshotter, err := c.fetch(ctx, key)
			if err != nil {
				return nil, err
			}

			c.mutex.Lock()
			c.snapshotters[key] = newSnapshotter
			c.mutex.Unlock()

			if c.lease != nil {
				c.lease.Enforce(key)
			}
			snapshotter = newSnapshotter
		}
	}
	return snapshotter, nil
}

// WalkAll applies the provided function to all cached snapshotters.
func (c *RemoteSnapshotterCache) WalkAll(ctx context.Context, fn snapshots.WalkFunc, filters ...string) error {
	c.mutex.RLock()
	defer c.mutex.RUnlock()

	var allErr error

	for namespace, snapshotter := range c.snapshotters {
		if err := snapshotter.Walk(ctx, fn, filters...); err != nil {
			allErr = multierror.Append(allErr, fmt.Errorf("failed to walk function on snapshotter[%s]: %w", namespace, err))
		}
	}
	return allErr
}

// Evict removes a cached snapshotter for a given key.
func (c *RemoteSnapshotterCache) Evict(key string) error {
	c.mutex.RLock()
	s, ok := c.snapshotters[key]
	c.mutex.RUnlock()

	if !ok {
		return fmt.Errorf("snapshotter %s not found in cache", key)
	}

	c.mutex.Lock()
	defer c.mutex.Unlock()

	err := s.Close()
	delete(c.snapshotters, key)
	return err
}

// Close terminates any background reapers and closes any cached snapshotters.
func (c *RemoteSnapshotterCache) Close() error {
	close(c.stop)
	if c.lease != nil {
		close(c.evict)
	}

	c.mutex.Lock()
	defer c.mutex.Unlock()

	var allErr error

	for k, v := range c.snapshotters {
		if err := v.Close(); err != nil {
			allErr = multierror.Append(allErr, err)
		}
		delete(c.snapshotters, k)
	}
	return allErr
}
