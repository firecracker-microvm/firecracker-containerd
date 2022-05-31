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

package demux

import (
	"context"

	"github.com/containerd/containerd/log"
	"github.com/containerd/containerd/mount"
	"github.com/containerd/containerd/namespaces"
	"github.com/containerd/containerd/snapshots"
	"github.com/sirupsen/logrus"

	"github.com/firecracker-microvm/firecracker-containerd/internal/vm"
	"github.com/firecracker-microvm/firecracker-containerd/snapshotter/demux/cache"
	mountutil "github.com/firecracker-microvm/firecracker-containerd/snapshotter/internal/mount"
)

// Snapshotter routes snapshotter requests to their destined
// remote snapshotter via their snapshotter namespace.
//
// Proxy snapshotters are cached for subsequent snapshotter requests.
type Snapshotter struct {
	cache            cache.Cache
	fetchSnapshotter cache.SnapshotterProvider
}

// NewSnapshotter creates instance of Snapshotter with cache and
// proxy snapshotter creation function.
func NewSnapshotter(cache cache.Cache, fetchSnapshotter cache.SnapshotterProvider) snapshots.Snapshotter {
	return &Snapshotter{cache, fetchSnapshotter}
}

// Stat proxies remote snapshotter stat request.
//
// See https://github.com/containerd/containerd/blob/v1.6.4/snapshots/snapshotter.go
func (s *Snapshotter) Stat(ctx context.Context, key string) (snapshots.Info, error) {
	contextLogger := log.G(ctx).WithField("function", "Stat")

	snapshotter, err := s.getSnapshotterFromCache(ctx, contextLogger)
	if err != nil {
		return snapshots.Info{}, err
	}

	return snapshotter.Stat(ctx, key)
}

// Update proxies remote snapshotter update request.
//
// See https://github.com/containerd/containerd/blob/v1.6.4/snapshots/snapshotter.go
func (s *Snapshotter) Update(ctx context.Context, info snapshots.Info, fieldpaths ...string) (snapshots.Info, error) {
	contextLogger := log.G(ctx).WithField("function", "Update")

	snapshotter, err := s.getSnapshotterFromCache(ctx, contextLogger)
	if err != nil {
		return snapshots.Info{}, err
	}

	return snapshotter.Update(ctx, info, fieldpaths...)
}

// Usage proxies remote snapshotter usage request.
//
// See https://github.com/containerd/containerd/blob/v1.6.4/snapshots/snapshotter.go
func (s *Snapshotter) Usage(ctx context.Context, key string) (snapshots.Usage, error) {
	contextLogger := log.G(ctx).WithField("function", "Usage")

	snapshotter, err := s.getSnapshotterFromCache(ctx, contextLogger)
	if err != nil {
		return snapshots.Usage{}, err
	}

	return snapshotter.Usage(ctx, key)
}

// Mounts proxies remote snapshotter mounts request.
//
// See https://github.com/containerd/containerd/blob/v1.6.4/snapshots/snapshotter.go
func (s *Snapshotter) Mounts(ctx context.Context, key string) ([]mount.Mount, error) {
	contextLogger := log.G(ctx).WithField("function", "Mounts")

	snapshotter, err := s.getSnapshotterFromCache(ctx, contextLogger)
	if err != nil {
		return []mount.Mount{}, err
	}

	mounts, err := snapshotter.Mounts(ctx, key)
	if err != nil {
		return []mount.Mount{}, err
	}
	return mountutil.Map(mounts, vm.AddLocalMountIdentifier), nil
}

// Prepare proxies remote snapshotter prepare request.
//
// See https://github.com/containerd/containerd/blob/v1.6.4/snapshots/snapshotter.go
func (s *Snapshotter) Prepare(ctx context.Context, key string, parent string, opts ...snapshots.Opt) ([]mount.Mount, error) {
	contextLogger := log.G(ctx).WithField("function", "Prepare")

	snapshotter, err := s.getSnapshotterFromCache(ctx, contextLogger)
	if err != nil {
		return []mount.Mount{}, err
	}

	mounts, err := snapshotter.Prepare(ctx, key, parent, opts...)
	if err != nil {
		return []mount.Mount{}, err
	}
	return mountutil.Map(mounts, vm.AddLocalMountIdentifier), nil
}

// View proxies remote snapshotter view request.
//
// See https://github.com/containerd/containerd/blob/v1.6.4/snapshots/snapshotter.go
func (s *Snapshotter) View(ctx context.Context, key string, parent string, opts ...snapshots.Opt) ([]mount.Mount, error) {
	contextLogger := log.G(ctx).WithField("function", "View")

	snapshotter, err := s.getSnapshotterFromCache(ctx, contextLogger)
	if err != nil {
		return []mount.Mount{}, err
	}

	mounts, err := snapshotter.View(ctx, key, parent, opts...)
	if err != nil {
		return []mount.Mount{}, err
	}
	return mountutil.Map(mounts, vm.AddLocalMountIdentifier), nil
}

// Commit proxies remote snapshotter commit request.
//
// See https://github.com/containerd/containerd/blob/v1.6.4/snapshots/snapshotter.go
func (s *Snapshotter) Commit(ctx context.Context, name string, key string, opts ...snapshots.Opt) error {
	contextLogger := log.G(ctx).WithField("function", "Commit")

	snapshotter, err := s.getSnapshotterFromCache(ctx, contextLogger)
	if err != nil {
		return err
	}

	return snapshotter.Commit(ctx, name, key, opts...)
}

// Remove proxies remote snapshotter remove request.
//
// See https://github.com/containerd/containerd/blob/v1.6.4/snapshots/snapshotter.go
func (s *Snapshotter) Remove(ctx context.Context, key string) error {
	contextLogger := log.G(ctx).WithField("function", "Remove")

	snapshotter, err := s.getSnapshotterFromCache(ctx, contextLogger)
	if err != nil {
		return err
	}

	return snapshotter.Remove(ctx, key)
}

// Walk proxies remote snapshotter walk request.
//
// See https://github.com/containerd/containerd/blob/v1.6.4/snapshots/snapshotter.go
func (s *Snapshotter) Walk(ctx context.Context, fn snapshots.WalkFunc, filters ...string) error {
	contextLogger := log.G(ctx).WithField("function", "Walk")

	_, err := getNamespaceFromContext(ctx, contextLogger)
	if err != nil {
		contextLogger.Debug("no namespace found, proxying walk function to all cached snapshotters")
		return s.cache.WalkAll(ctx, fn, filters...)
	}

	snapshotter, err := s.getSnapshotterFromCache(ctx, contextLogger)
	if err != nil {
		return err
	}

	return snapshotter.Walk(ctx, fn, filters...)
}

// Close calls close on all cached remote snapshotters.
//
// See https://github.com/containerd/containerd/blob/v1.6.4/snapshots/snapshotter.go
func (s *Snapshotter) Close() error {
	return s.cache.Close()
}

// Cleanup proxies remote snapshotter cleanup request.
//
// See https://github.com/containerd/containerd/blob/v1.6.4/snapshots/snapshotter.go
func (s *Snapshotter) Cleanup(ctx context.Context) error {
	contextLogger := log.G(ctx).WithField("function", "Cleanup")

	snapshotter, err := s.getSnapshotterFromCache(ctx, contextLogger)
	if err != nil {
		return err
	}

	return snapshotter.(snapshots.Cleaner).Cleanup(ctx)
}

const snapshotterNotFoundErrorString = "Snapshotter not found in cache"

func (s *Snapshotter) getSnapshotterFromCache(ctx context.Context, log *logrus.Entry) (snapshots.Snapshotter, error) {
	namespace, err := getNamespaceFromContext(ctx, log)
	if err != nil {
		return nil, err
	}
	snapshotter, err := s.cache.Get(ctx, namespace, s.fetchSnapshotter)
	if err != nil {
		log.WithField("namespace", namespace).WithError(err).Error(snapshotterNotFoundErrorString)
		return nil, err
	}
	return snapshotter, nil
}

const missingNamespaceErrorString = "Function called without namespaced context"

func getNamespaceFromContext(ctx context.Context, log *logrus.Entry) (string, error) {
	namespace, err := namespaces.NamespaceRequired(ctx)
	if err != nil {
		log.WithError(err).Error(missingNamespaceErrorString)
		return "", err
	}
	return namespace, nil
}
