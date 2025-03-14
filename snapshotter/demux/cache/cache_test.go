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
	"errors"
	"fmt"
	"testing"

	"github.com/containerd/containerd/snapshots"
	"github.com/hashicorp/go-multierror"

	"github.com/firecracker-microvm/firecracker-containerd/snapshotter/demux/internal"
	"github.com/firecracker-microvm/firecracker-containerd/snapshotter/demux/proxy"
)

func getSnapshotterOkFunction(_ context.Context, _ string) (*proxy.RemoteSnapshotter, error) {
	return &proxy.RemoteSnapshotter{Snapshotter: &internal.SuccessfulSnapshotter{}}, nil
}

func getSnapshotterErrorFunction(_ context.Context, _ string) (*proxy.RemoteSnapshotter, error) {
	return nil, errors.New("Mock retrieve snapshotter error")
}

func getFailingSnapshotterOkFunction(_ context.Context, _ string) (*proxy.RemoteSnapshotter, error) {
	return &proxy.RemoteSnapshotter{Snapshotter: &internal.FailingSnapshotter{}}, nil
}

func getSnapshotterFromEmptyCache() error {
	uut := NewRemoteSnapshotterCache(getSnapshotterOkFunction)
	_, err := uut.Get(context.Background(), "SnapshotterKey")
	if err != nil {
		return fmt.Errorf("Fetch from empty cache incorrectly resulted in error: %w", err)
	}
	return nil
}

func getCachedSnapshotter() error {
	uut := NewRemoteSnapshotterCache(getSnapshotterOkFunction)
	if _, err := uut.Get(context.Background(), "SnapshotterKey"); err != nil {
		return fmt.Errorf("Adding snapshotter to empty cache incorrectly resulted in error: %w", err)
	}

	if _, err := uut.Get(context.Background(), "SnapshotterKey"); err != nil {
		return fmt.Errorf("Fetching cached snapshotter resulted in error: %w", err)
	}
	return nil
}

func getSnapshotterPropagatesErrors() error {
	uut := NewRemoteSnapshotterCache(getSnapshotterErrorFunction)
	if _, err := uut.Get(context.Background(), "SnapshotterKey"); err == nil {
		return errors.New("Get function did not propagate errors from snapshotter generator function")
	}
	return nil
}

func successfulWalk(_ context.Context, _ snapshots.Info) error {
	return nil
}

func applyWalkFunctionOnEmptyCache() error {
	uut := NewRemoteSnapshotterCache(getSnapshotterOkFunction)
	if err := uut.WalkAll(context.Background(), successfulWalk); err != nil {
		return errors.New("WalkAll on empty cache incorrectly resulted in error")
	}
	return nil
}

func applyWalkFunctionToAllCachedSnapshotters() error {
	uut := NewRemoteSnapshotterCache(getSnapshotterOkFunction)
	if _, err := uut.Get(context.Background(), "Snapshotter-A"); err != nil {
		return fmt.Errorf("Adding snapshotter A to empty cache incorrectly resulted in error: %w", err)
	}
	if _, err := uut.Get(context.Background(), "Snapshotter-B"); err != nil {
		return fmt.Errorf("Adding snapshotter B to cache incorrectly resulted in error: %w", err)
	}
	if err := uut.WalkAll(context.Background(), successfulWalk); err != nil {
		return fmt.Errorf("WalkAll on populated cache incorrectly resulted in error: %w", err)
	}
	return nil
}

func applyWalkFunctionPropagatesErrors() error {
	uut := NewRemoteSnapshotterCache(getFailingSnapshotterOkFunction)
	if _, err := uut.Get(context.Background(), "Snapshotter-A"); err != nil {
		return fmt.Errorf("Adding snapshotter A to empty cache incorrectly resulted in error: %w", err)
	}
	// The failing snapshotter mock will fail all Walk calls before applying
	// the snapshots.WalkFunc, but for the purposes of this test that is fine.
	// In which case, any function will do.
	walkFunc := func(_ context.Context, _ snapshots.Info) error {
		return nil
	}
	if err := uut.WalkAll(context.Background(), walkFunc); err == nil {
		return errors.New("WalkAll did not propagate errors from walk function")
	}
	return nil
}

func evictSnapshotterFromEmptyCache() error {
	uut := NewRemoteSnapshotterCache(getSnapshotterOkFunction)
	if err := uut.Evict("SnapshotterKey"); err == nil {
		return errors.New("Evict function did not return error on call on empty cache")
	}
	return nil
}

func evictSnapshotterFromCache() error {
	uut := NewRemoteSnapshotterCache(getSnapshotterOkFunction)
	if _, err := uut.Get(context.Background(), "SnapshotterKey"); err != nil {
		return fmt.Errorf("Adding snapshotter to empty cache incorrectly resulted in error: %w", err)
	}

	if err := uut.Evict("SnapshotterKey"); err != nil {
		return fmt.Errorf("Evicting snapshotter incorrectly resulted in error: %w", err)
	}
	return nil
}

func evictSnapshotterFromCachePropagatesCloseError() error {
	uut := NewRemoteSnapshotterCache(getFailingSnapshotterOkFunction)
	if _, err := uut.Get(context.Background(), "SnapshotterKey"); err != nil {
		return fmt.Errorf("Adding snapshotter to empty cache incorrectly resulted in error: %w", err)
	}

	if err := uut.Evict("SnapshotterKey"); err == nil {
		return errors.New("Evicting snapshotter did not propagate closure error")
	}
	return nil
}

func closeCacheWithEmptyCache() error {
	uut := NewRemoteSnapshotterCache(getSnapshotterOkFunction)
	if err := uut.Close(); err != nil {
		return fmt.Errorf("Close on empty cache resulted in error: %w", err)
	}
	return nil
}

func closeCacheWithNonEmptyCache() error {
	uut := NewRemoteSnapshotterCache(getSnapshotterOkFunction)
	uut.Get(context.Background(), "SnapshotterKey")

	if err := uut.Close(); err != nil {
		return fmt.Errorf("Close on non-empty cache resulted in error: %w", err)
	}
	return nil
}

func closeCacheReturnsAllOccurringErrors() error {
	uut := NewRemoteSnapshotterCache(getFailingSnapshotterOkFunction)
	uut.Get(context.Background(), "FailingSnapshotterKey")
	uut.Get(context.Background(), "AnotherFailingSnapshotterKey")

	err := uut.Close()
	if err == nil {
		return errors.New("Close did not propagate the last close snapshotter error")
	}
	if merr, ok := err.(*multierror.Error); ok {
		if merr.Len() != 2 {
			return fmt.Errorf("Expected 2 errors: actual %d", merr.Len())
		}
	}
	return nil
}

func listEmptyCache() error {
	uut := NewRemoteSnapshotterCache(getSnapshotterOkFunction)
	keys := uut.List()
	if len(keys) > 0 {
		return errors.New("List did not return an empty list for an empty snapshotter cache")
	}

	return nil
}

func listNonEmptyCache() error {
	uut := NewRemoteSnapshotterCache(getSnapshotterOkFunction)
	uut.Get(context.Background(), "OkSnapshotterKey")

	keys := uut.List()
	if len(keys) != 1 {
		return errors.New("List should return a non-empty list of keys")
	}
	if keys[0] != "OkSnapshotterKey" {
		return errors.New("List did not return correct list of keys")
	}

	return nil
}

func TestGetSnapshotterFromCache(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		run  func() error
	}{
		{"AddSnapshotterToCache", getSnapshotterFromEmptyCache},
		{"GetCachedSnapshotter", getCachedSnapshotter},
		{"PropogateFetchSnapshotterErrors", getSnapshotterPropagatesErrors},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if err := test.run(); err != nil {
				t.Fatalf("%s: %s", test.name, err.Error())
			}
		})
	}
}

func TestWalkAllFunctionOnCache(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		run  func() error
	}{
		{"ApplyWalkFunctionOnEmptyCache", applyWalkFunctionOnEmptyCache},
		{"ApplyWalkFunctionToAllCachedSnapshotters", applyWalkFunctionToAllCachedSnapshotters},
		{"ApplyWalkFunctionPropogatesErrors", applyWalkFunctionPropagatesErrors},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if err := test.run(); err != nil {
				t.Fatalf("%s: %s", test.name, err.Error())
			}
		})
	}
}

func TestEvictSnapshotterFromCache(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		run  func() error
	}{
		{"EvictSnapshotterFromEmptyCache", evictSnapshotterFromEmptyCache},
		{"EvictSnapshotterFromCache", evictSnapshotterFromCache},
		{"PropogateEvictSnapshotterCloseErrors", evictSnapshotterFromCachePropagatesCloseError},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if err := test.run(); err != nil {
				t.Fatalf("%s: %s", test.name, err.Error())
			}
		})
	}
}

func TestCloseCache(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		run  func() error
	}{
		{"CloseCacheWithEmptyCache", closeCacheWithEmptyCache},
		{"CloseCacheWithNonEmptyCache", closeCacheWithNonEmptyCache},
		{"CloseCacheReturnsAllOccurringErrors", closeCacheReturnsAllOccurringErrors},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if err := test.run(); err != nil {
				t.Fatalf("%s: %s", test.name, err.Error())
			}
		})
	}
}

func TestListSnapshotters(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		run  func() error
	}{
		{"ListEmptyCache", listEmptyCache},
		{"ListNonEmptyCache", listNonEmptyCache},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if err := test.run(); err != nil {
				t.Fatalf("%s: %s", test.name, err.Error())
			}
		})
	}
}
