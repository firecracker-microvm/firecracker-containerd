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
	"errors"
	"testing"

	"github.com/containerd/containerd/log/logtest"
	"github.com/containerd/containerd/namespaces"
	"github.com/containerd/containerd/snapshots"

	"github.com/firecracker-microvm/firecracker-containerd/snapshotter/demux/cache"
	"github.com/firecracker-microvm/firecracker-containerd/snapshotter/demux/internal/mock"
	"github.com/firecracker-microvm/firecracker-containerd/snapshotter/demux/proxy"
)

func fetchOkSnapshotter(ctx context.Context, key string) (*proxy.RemoteSnapshotter, error) {
	return &proxy.RemoteSnapshotter{Snapshotter: &mock.Snapshotter{}}, nil
}

func fetchSnapshotterNotFound(ctx context.Context, key string) (*proxy.RemoteSnapshotter, error) {
	return nil, errors.New("mock snapshotter not found")
}

func createSnapshotterCacheWithSuccessfulSnapshotter(namespace string) cache.Cache {
	cache := cache.NewSnapshotterCache()
	cache.Get(context.Background(), namespace, fetchOkSnapshotter)
	return cache
}

func fetchFailingSnapshotter(ctx context.Context, key string) (*proxy.RemoteSnapshotter, error) {
	return &proxy.RemoteSnapshotter{Snapshotter: &mock.Snapshotter{
		StatError:    errors.New("mock stat error"),
		UpdateError:  errors.New("mock update error"),
		UsageError:   errors.New("mock usage error"),
		MountsError:  errors.New("mock mounts error"),
		PrepareError: errors.New("mock prepare error"),
		ViewError:    errors.New("mock view error"),
		CommitError:  errors.New("mock commit error"),
		RemoveError:  errors.New("mock remove error"),
		WalkError:    errors.New("mock walk error"),
		CloseError:   errors.New("mock close error"),
		CleanupError: errors.New("mock cleanup error"),
	}}, nil
}

func createSnapshotterCacheWithFailingSnapshotter(namespace string) cache.Cache {
	cache := cache.NewSnapshotterCache()
	cache.Get(context.Background(), namespace, fetchFailingSnapshotter)
	return cache
}

func TestReturnErrorWhenCalledWithoutNamespacedContext(t *testing.T) {
	t.Parallel()

	cache := cache.NewSnapshotterCache()
	ctx := logtest.WithT(context.Background(), t)

	uut := NewSnapshotter(cache, fetchOkSnapshotter)

	tests := []struct {
		name string
		run  func() error
	}{
		{"Stat", func() error { _, err := uut.Stat(ctx, "layerKey"); return err }},
		{"Update", func() error { _, err := uut.Update(ctx, snapshots.Info{}); return err }},
		{"Usage", func() error { _, err := uut.Usage(ctx, "layerKey"); return err }},
		{"Mounts", func() error { _, err := uut.Mounts(ctx, "layerKey"); return err }},
		{"Prepare", func() error { _, err := uut.Prepare(ctx, "layerKey", ""); return err }},
		{"View", func() error { _, err := uut.View(ctx, "layerKey", ""); return err }},
		{"Commit", func() error { return uut.Commit(ctx, "layer1", "layerKey") }},
	}

	for _, test := range tests {
		if err := test.run(); err == nil {
			t.Fatalf("%s call did not return error", test.name)
		}
	}
}

func TestNoErrorWhenCalledWithoutNamespacedContext(t *testing.T) {
	t.Parallel()

	cache := cache.NewSnapshotterCache()
	ctx := logtest.WithT(context.Background(), t)

	uut := NewSnapshotter(cache, fetchOkSnapshotter)

	tests := []struct {
		name string
		run  func() error
	}{
		{"Remove", func() error { return uut.Remove(ctx, "layerKey") }},
		{"Walk", func() error {
			var callback = func(c context.Context, i snapshots.Info) error { return nil }
			return uut.Walk(ctx, callback)
		}},
		{"Cleanup", func() error { return uut.(snapshots.Cleaner).Cleanup(ctx) }},
	}

	for _, test := range tests {
		if err := test.run(); err != nil {
			t.Fatalf("%s call returned error on no namespace execution", test.name)
		}
	}
}

func TestReturnErrorWhenSnapshotterNotFound(t *testing.T) {
	t.Parallel()

	const namespace = "testing"
	cache := cache.NewSnapshotterCache()
	ctx := namespaces.WithNamespace(context.TODO(), namespace)
	ctx = logtest.WithT(ctx, t)

	uut := NewSnapshotter(cache, fetchSnapshotterNotFound)

	tests := []struct {
		name string
		run  func() error
	}{
		{"Stat", func() error { _, err := uut.Stat(ctx, "layerKey"); return err }},
		{"Update", func() error { _, err := uut.Update(ctx, snapshots.Info{}); return err }},
		{"Usage", func() error { _, err := uut.Usage(ctx, "layerKey"); return err }},
		{"Mounts", func() error { _, err := uut.Mounts(ctx, "layerKey"); return err }},
		{"Prepare", func() error { _, err := uut.Prepare(ctx, "layerKey", ""); return err }},
		{"View", func() error { _, err := uut.View(ctx, "layerKey", ""); return err }},
		{"Commit", func() error { return uut.Commit(ctx, "layer1", "layerKey") }},
		{"Remove", func() error { return uut.Remove(ctx, "layerKey") }},
		{"Walk", func() error {
			var callback = func(c context.Context, i snapshots.Info) error { return nil }
			return uut.Walk(ctx, callback)
		}},
		{"Cleanup", func() error { return uut.(snapshots.Cleaner).Cleanup(ctx) }},
	}

	for _, test := range tests {
		if err := test.run(); err == nil {
			t.Fatalf("%s call did not return error", test.name)
		}
	}
}

func TestReturnErrorAfterProxyFunctionFailure(t *testing.T) {
	t.Parallel()

	const namespace = "testing"
	cache := createSnapshotterCacheWithFailingSnapshotter(namespace)
	ctx := namespaces.WithNamespace(context.Background(), namespace)
	ctx = logtest.WithT(ctx, t)

	uut := NewSnapshotter(cache, fetchOkSnapshotter)

	tests := []struct {
		name string
		run  func() error
	}{
		{"Stat", func() error { _, err := uut.Stat(ctx, "layerKey"); return err }},
		{"Update", func() error { _, err := uut.Update(ctx, snapshots.Info{}); return err }},
		{"Usage", func() error { _, err := uut.Usage(ctx, "layerKey"); return err }},
		{"Mounts", func() error { _, err := uut.Mounts(ctx, "layerKey"); return err }},
		{"Prepare", func() error { _, err := uut.Prepare(ctx, "layerKey", ""); return err }},
		{"View", func() error { _, err := uut.View(ctx, "layerKey", ""); return err }},
		{"Commit", func() error { return uut.Commit(ctx, "layer1", "layerKey") }},
		{"Remove", func() error { return uut.Remove(ctx, "layerKey") }},
		{"Walk", func() error {
			var callback = func(c context.Context, i snapshots.Info) error { return nil }
			return uut.Walk(ctx, callback)
		}},
		{"Close", func() error { return uut.Close() }},
		{"Cleanup", func() error { return uut.(snapshots.Cleaner).Cleanup(ctx) }},
	}

	for _, test := range tests {
		t.Run(test.name+"ProxyFailure", func(t *testing.T) {
			if err := test.run(); err == nil {
				t.Fatalf("%s call did not return error", test.name)
			}
		})
	}
}

func TestNoErrorIsReturnedOnSuccessfulProxyExecution(t *testing.T) {
	t.Parallel()

	const namespace = "testing"
	cache := createSnapshotterCacheWithSuccessfulSnapshotter(namespace)
	ctx := namespaces.WithNamespace(context.Background(), namespace)
	ctx = logtest.WithT(ctx, t)

	uut := NewSnapshotter(cache, fetchOkSnapshotter)

	tests := []struct {
		name string
		run  func() error
	}{
		{"Stat", func() error { _, err := uut.Stat(ctx, "layerKey"); return err }},
		{"Update", func() error { _, err := uut.Update(ctx, snapshots.Info{}); return err }},
		{"Usage", func() error { _, err := uut.Usage(ctx, "layerKey"); return err }},
		{"Mounts", func() error { _, err := uut.Mounts(ctx, "layerKey"); return err }},
		{"Prepare", func() error { _, err := uut.Prepare(ctx, "layerKey", ""); return err }},
		{"View", func() error { _, err := uut.View(ctx, "layerKey", ""); return err }},
		{"Commit", func() error { return uut.Commit(ctx, "layer1", "layerKey") }},
		{"Remove", func() error { return uut.Remove(ctx, "layerKey") }},
		{"Walk", func() error {
			var callback = func(c context.Context, i snapshots.Info) error { return nil }
			return uut.Walk(ctx, callback)
		}},
		{"Close", func() error { return uut.Close() }},
		{"Cleanup", func() error { return uut.(snapshots.Cleaner).Cleanup(ctx) }},
	}

	for _, test := range tests {
		t.Run(test.name+"SuccessfulProxyCall", func(t *testing.T) {
			if err := test.run(); err != nil {
				t.Fatalf("%s call incorrectly returned an error", test.name)
			}
		})
	}
}
