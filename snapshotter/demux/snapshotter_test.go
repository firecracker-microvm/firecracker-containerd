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
	"github.com/firecracker-microvm/firecracker-containerd/snapshotter/demux/internal"
	"github.com/firecracker-microvm/firecracker-containerd/snapshotter/demux/proxy"
)

func fetchOkSnapshotter(ctx context.Context, key string) (*proxy.RemoteSnapshotter, error) {
	var snapshotter internal.SuccessfulSnapshotter = internal.SuccessfulSnapshotter{}
	return &proxy.RemoteSnapshotter{Snapshotter: &snapshotter}, nil
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
	var snapshotter internal.FailingSnapshotter = internal.FailingSnapshotter{}
	return &proxy.RemoteSnapshotter{Snapshotter: &snapshotter}, nil
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
		{"Remove", func() error { return uut.Remove(ctx, "layerKey") }},
		{"Walk", func() error {
			var callback = func(c context.Context, i snapshots.Info) error { return nil }
			return uut.Walk(ctx, callback)
		}},
	}

	for _, test := range tests {
		if err := test.run(); err == nil {
			t.Fatal(test.name + " call did not return error")
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
	}

	for _, test := range tests {
		if err := test.run(); err == nil {
			t.Fatal(test.name + " call did not return error")
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
	}

	for _, test := range tests {
		t.Run(test.name+"ProxyFailure", func(t *testing.T) {
			if err := test.run(); err == nil {
				t.Fatal(test.name + " call did not return error")
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
	}

	for _, test := range tests {
		t.Run(test.name+"SuccessfulProxyCall", func(t *testing.T) {
			if err := test.run(); err != nil {
				t.Fatal(test.name + " call incorrectly returned an error")
			}
		})
	}
}
