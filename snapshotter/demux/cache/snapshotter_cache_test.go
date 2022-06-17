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

	"github.com/containerd/containerd/errdefs"
	"github.com/containerd/containerd/snapshots"
	"github.com/hashicorp/go-multierror"

	"github.com/firecracker-microvm/firecracker-containerd/snapshotter/demux/internal/mock"
	"github.com/firecracker-microvm/firecracker-containerd/snapshotter/demux/proxy"
)

func getSnapshotterOkFunction(ctx context.Context, key string) (*proxy.RemoteSnapshotter, error) {
	return &proxy.RemoteSnapshotter{Snapshotter: &mock.Snapshotter{}}, nil
}

func getSnapshotterErrorFunction(ctx context.Context, key string) (*proxy.RemoteSnapshotter, error) {
	return &proxy.RemoteSnapshotter{Snapshotter: nil}, errors.New("mock error")
}

func getFailingSnapshotterOkFunction(ctx context.Context, key string) (*proxy.RemoteSnapshotter, error) {
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

func getSnapshotterFromEmptyCache(uut *SnapshotterCache) error {
	_, err := uut.Get(context.Background(), "SnapshotterKey", getSnapshotterOkFunction)
	if err != nil {
		return fmt.Errorf("Fetch from empty cache incorrectly resulted in error: %w", err)
	}
	return nil
}

func getCachedSnapshotter(uut *SnapshotterCache) error {
	if _, err := uut.Get(context.Background(), "SnapshotterKey", getSnapshotterOkFunction); err != nil {
		return fmt.Errorf("Adding snapshotter to empty cache incorrectly resulted in error: %w", err)
	}

	if _, err := uut.Get(context.Background(), "SnapshotterKey", getSnapshotterOkFunction); err != nil {
		return fmt.Errorf("Fetching cached snapshotter resulted in error: %w", err)
	}
	return nil
}

func getSnapshotterPropagatesErrors(uut *SnapshotterCache) error {
	if _, err := uut.Get(context.Background(), "SnapshotterKey", getSnapshotterErrorFunction); err == nil {
		return fmt.Errorf("Get function did not propagate errors from snapshotter generator function [%w]", err)
	}
	return nil
}

func successfulWalk(ctx context.Context, info snapshots.Info) error {
	return nil
}

func removeFunctionOnEmptyCache(uut *SnapshotterCache) error {
	if err := uut.RemoveAll(context.Background(), "SnapshotKey"); err != nil {
		return fmt.Errorf("RemoveAll on empty cache incorrectly resulted in error [%w]", err)
	}
	return nil
}

func removeFunctionToAllCachedSnapshotters(uut *SnapshotterCache) error {
	if _, err := uut.Get(context.Background(), "Snapshotter-A", getSnapshotterOkFunction); err != nil {
		return fmt.Errorf("Adding snapshotter A to empty cache incorrectly resulted in error [%w]", err)
	}
	if _, err := uut.Get(context.Background(), "Snapshotter-B", getSnapshotterOkFunction); err != nil {
		return fmt.Errorf("Adding snapshotter B to cache incorrectly resulted in error [%w]", err)
	}
	if err := uut.RemoveAll(context.Background(), "SnapshotKey"); err != nil {
		return fmt.Errorf("RemoveAll on populated cache incorrectly resulted in error [%w]", err)
	}
	return nil
}

func removeFunctionPropagatesErrorsExceptSnapshotNotFound(uut *SnapshotterCache) error {
	if _, err := uut.Get(context.Background(), "Snapshotter-A", getFailingSnapshotterOkFunction); err != nil {
		return fmt.Errorf("Adding snapshotter A to empty cache incorrectly resulted in error [%w]", err)
	}

	getFailingRemoveSnapshotterWithSnapshotNotFound := func(ctx context.Context, key string) (*proxy.RemoteSnapshotter, error) {
		return &proxy.RemoteSnapshotter{Snapshotter: &mock.Snapshotter{RemoveError: errdefs.ErrNotFound}}, nil
	}
	if _, err := uut.Get(context.Background(), "Snapshotter-B", getFailingRemoveSnapshotterWithSnapshotNotFound); err != nil {
		return fmt.Errorf("Adding snapshotter B to cache incorrectly resulted in error [%w]", err)
	}

	getFailingRemoveSnapshotter := func(ctx context.Context, key string) (*proxy.RemoteSnapshotter, error) {
		return &proxy.RemoteSnapshotter{Snapshotter: &mock.Snapshotter{RemoveError: errors.New("mock remove error")}}, nil
	}
	if _, err := uut.Get(context.Background(), "Snapshotter-C", getFailingRemoveSnapshotter); err != nil {
		return fmt.Errorf("Adding snapshotter C to cache incorrectly resulted in error [%w]", err)
	}
	err := uut.RemoveAll(context.Background(), "SnapshotKey")
	if err == nil {
		return fmt.Errorf("RemoveAll did not propagate errors from remove function")
	}
	if err == errdefs.ErrNotFound {
		return fmt.Errorf("RemoveAll did not mask snapshot not found errors [%w]", err)
	}
	return nil
}

func applyWalkFunctionOnEmptyCache(uut *SnapshotterCache) error {
	if err := uut.WalkAll(context.Background(), successfulWalk); err != nil {
		return fmt.Errorf("WalkAll on empty cache incorrectly resulted in error [%w]", err)
	}
	return nil
}

func applyWalkFunctionToAllCachedSnapshotters(uut *SnapshotterCache) error {
	if _, err := uut.Get(context.Background(), "Snapshotter-A", getSnapshotterOkFunction); err != nil {
		return fmt.Errorf("Adding snapshotter A to empty cache incorrectly resulted in error: %w", err)
	}
	if _, err := uut.Get(context.Background(), "Snapshotter-B", getSnapshotterOkFunction); err != nil {
		return fmt.Errorf("Adding snapshotter B to cache incorrectly resulted in error: %w", err)
	}
	if err := uut.WalkAll(context.Background(), successfulWalk); err != nil {
		return fmt.Errorf("WalkAll on populated cache incorrectly resulted in error: %w", err)
	}
	return nil
}

func applyWalkFunctionPropagatesErrors(uut *SnapshotterCache) error {
	if _, err := uut.Get(context.Background(), "Snapshotter-A", getFailingSnapshotterOkFunction); err != nil {
		return fmt.Errorf("Adding snapshotter A to empty cache incorrectly resulted in error: %w", err)
	}
	// The failing snapshotter mock will fail all Walk calls before applying
	// the snapshots.WalkFunc, but for the purposes of this test that is fine.
	// In which case, any function will do.
	walkFunc := func(ctx context.Context, info snapshots.Info) error {
		return nil
	}
	if err := uut.WalkAll(context.Background(), walkFunc); err == nil {
		return fmt.Errorf("WalkAll did not propagate errors from walk function [%w]", err)
	}
	return nil
}

func cleanupFunctionOnEmptyCache(uut *SnapshotterCache) error {
	if err := uut.CleanupAll(context.Background()); err != nil {
		return fmt.Errorf("CleanupAll on empty cache incorrectly resulted in error [%w]", err)
	}
	return nil
}

func cleanupFunctionToAllCacheSnapshotters(uut *SnapshotterCache) error {
	if _, err := uut.Get(context.Background(), "Snapshotter-A", getSnapshotterOkFunction); err != nil {
		return fmt.Errorf("Adding snapshotter A to empty cache incorrectly resulted in error [%w]", err)
	}
	if _, err := uut.Get(context.Background(), "Snapshotter-B", getSnapshotterOkFunction); err != nil {
		return fmt.Errorf("Adding snapshotter B to cache incorrectly resulted in error [%w]", err)
	}
	if err := uut.CleanupAll(context.Background()); err != nil {
		return fmt.Errorf("CleanupAll on populated cache incorrectly resulted in error")
	}
	return nil
}

func cleanupFunctionPropagatesErrors(uut *SnapshotterCache) error {
	if _, err := uut.Get(context.Background(), "Snapshotter-A", getSnapshotterOkFunction); err != nil {
		return fmt.Errorf("Adding snapshotter A to empty cache incorrectly resulted in error [%w]", err)
	}

	if _, err := uut.Get(context.Background(), "Snapshotter-B", getFailingSnapshotterOkFunction); err != nil {
		return fmt.Errorf("Adding snapshotter B to cache incorrectly resulted in error [%w]", err)
	}

	if err := uut.CleanupAll(context.Background()); err == nil {
		return fmt.Errorf("CleanupAll did not propagate errors from remove function")
	}
	return nil
}

func evictSnapshotterFromEmptyCache(uut *SnapshotterCache) error {
	if err := uut.Evict("SnapshotterKey"); err == nil {
		return fmt.Errorf("Evict function did not return error on call on empty cache")
	}
	return nil
}

func evictSnapshotterFromCache(uut *SnapshotterCache) error {
	if _, err := uut.Get(context.Background(), "SnapshotterKey", getSnapshotterOkFunction); err != nil {
		return fmt.Errorf("Adding snapshotter to empty cache incorrectly resulted in error: %w", err)
	}

	if err := uut.Evict("SnapshotterKey"); err != nil {
		return fmt.Errorf("Evicting snapshotter incorrectly resulted in error: %w", err)
	}
	return nil
}

func evictSnapshotterFromCachePropagatesCloseError(uut *SnapshotterCache) error {
	if _, err := uut.Get(context.Background(), "SnapshotterKey", getFailingSnapshotterOkFunction); err != nil {
		return fmt.Errorf("Adding snapshotter to empty cache incorrectly resulted in error: %w", err)
	}

	if err := uut.Evict("SnapshotterKey"); err == nil {
		return errors.New("Evicting snapshotter did not propagate closure error")
	}
	return nil
}

func closeCacheWithEmptyCache(uut *SnapshotterCache) error {
	if err := uut.Close(); err != nil {
		return fmt.Errorf("Close on empty cache resulted in error: %w", err)
	}
	return nil
}

func closeCacheWithNonEmptyCache(uut *SnapshotterCache) error {
	uut.Get(context.Background(), "SnapshotterKey", getSnapshotterOkFunction)

	if err := uut.Close(); err != nil {
		return fmt.Errorf("Close on non-empty cache resulted in error: %w", err)
	}
	return nil
}

func closeCacheReturnsAllOccurringErrors(uut *SnapshotterCache) error {
	uut.Get(context.Background(), "OkSnapshotterKey", getSnapshotterOkFunction)
	uut.Get(context.Background(), "FailingSnapshotterKey", getFailingSnapshotterOkFunction)
	uut.Get(context.Background(), "AnotherFailingSnapshotterKey", getFailingSnapshotterOkFunction)

	err := uut.Close()
	if err == nil {
		return fmt.Errorf("Close did not propagate the last close snapshotter error")
	}
	if merr, ok := err.(*multierror.Error); ok {
		if merr.Len() != 2 {
			return fmt.Errorf("Expected 2 errors: actual %d", merr.Len())
		}
	}
	return nil
}

func listEmptyCache(uut *SnapshotterCache) error {
	keys := uut.List()
	if len(keys) > 0 {
		return fmt.Errorf("List did not return an empty list for an empty snapshotter cache")
	}

	return nil
}

func listNonEmptyCache(uut *SnapshotterCache) error {
	uut.Get(context.Background(), "OkSnapshotterKey", getSnapshotterOkFunction)

	keys := uut.List()
	if len(keys) != 1 {
		return fmt.Errorf("List should return a non-empty list of keys")
	}
	if keys[0] != "OkSnapshotterKey" {
		return fmt.Errorf("List did not return correct list of keys")
	}

	return nil
}

func TestGetSnapshotterFromCache(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		run  func(*SnapshotterCache) error
	}{
		{"AddSnapshotterToCache", getSnapshotterFromEmptyCache},
		{"GetCachedSnapshotter", getCachedSnapshotter},
		{"PropogateFetchSnapshotterErrors", getSnapshotterPropagatesErrors},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			uut := NewSnapshotterCache()
			if err := test.run(uut); err != nil {
				t.Fatalf("%s: %s", test.name, err.Error())
			}
		})
	}
}

func TestRemoveAllFunctionOnCache(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		run  func(*SnapshotterCache) error
	}{
		{"RemoveFunctionOnEmptyCache", removeFunctionOnEmptyCache},
		{"RemoveFunctionToAllCachedSnapshotters", removeFunctionToAllCachedSnapshotters},
		{"RemoveFunctionPropagatesErrorsExceptSnapshotNotFound", removeFunctionPropagatesErrorsExceptSnapshotNotFound},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			uut := NewSnapshotterCache()
			if err := test.run(uut); err != nil {
				t.Fatalf("%s: %v", test.name, err)
			}
		})
	}
}

func TestWalkAllFunctionOnCache(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		run  func(*SnapshotterCache) error
	}{
		{"ApplyWalkFunctionOnEmptyCache", applyWalkFunctionOnEmptyCache},
		{"ApplyWalkFunctionToAllCachedSnapshotters", applyWalkFunctionToAllCachedSnapshotters},
		{"ApplyWalkFunctionPropagatesErrors", applyWalkFunctionPropagatesErrors},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			uut := NewSnapshotterCache()
			if err := test.run(uut); err != nil {
				t.Fatalf("%s: %s", test.name, err.Error())
			}
		})
	}
}

func TestCleanupAllFunctionOnCache(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		run  func(*SnapshotterCache) error
	}{
		{"CleanupFunctionOnEmptyCache", cleanupFunctionOnEmptyCache},
		{"CleanupFunctionToAllCachedSnapshotters", cleanupFunctionToAllCacheSnapshotters},
		{"CleanupFunctionPropagatesErrors", cleanupFunctionPropagatesErrors},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			uut := NewSnapshotterCache()
			if err := test.run(uut); err != nil {
				t.Fatalf("%s: %v", test.name, err)
			}
		})
	}
}

func TestEvictSnapshotterFromCache(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		run  func(*SnapshotterCache) error
	}{
		{"EvictSnapshotterFromEmptyCache", evictSnapshotterFromEmptyCache},
		{"EvictSnapshotterFromCache", evictSnapshotterFromCache},
		{"PropogateEvictSnapshotterCloseErrors", evictSnapshotterFromCachePropagatesCloseError},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			uut := NewSnapshotterCache()
			if err := test.run(uut); err != nil {
				t.Fatalf("%s: %s", test.name, err.Error())
			}
		})
	}
}

func TestCloseCache(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		run  func(*SnapshotterCache) error
	}{
		{"CloseCacheWithEmptyCache", closeCacheWithEmptyCache},
		{"CloseCacheWithNonEmptyCache", closeCacheWithNonEmptyCache},
		{"CloseCacheReturnsAllOccurringErrors", closeCacheReturnsAllOccurringErrors},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			uut := NewSnapshotterCache()
			if err := test.run(uut); err != nil {
				t.Fatalf("%s: %s", test.name, err.Error())
			}
		})
	}
}

func TestListSnapshotters(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		run  func(*SnapshotterCache) error
	}{
		{"ListEmptyCache", listEmptyCache},
		{"ListNonEmptyCache", listNonEmptyCache},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			uut := NewSnapshotterCache()
			if err := test.run(uut); err != nil {
				t.Fatalf("%s: %s", test.name, err.Error())
			}
		})
	}
}
