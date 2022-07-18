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
	"net"
	"testing"
	"time"

	"github.com/firecracker-microvm/firecracker-containerd/snapshotter/demux/internal"
	"github.com/firecracker-microvm/firecracker-containerd/snapshotter/demux/proxy"
	"github.com/stretchr/testify/require"
	"go.uber.org/goleak"
)

func getSnapshotter(ctx context.Context, config proxy.RemoteSnapshotterConfig) (*proxy.RemoteSnapshotter, error) {
	return &proxy.RemoteSnapshotter{Snapshotter: &internal.SuccessfulSnapshotter{}}, nil
}

func getErrorSnapshotter(ctx context.Context, config proxy.RemoteSnapshotterConfig) (*proxy.RemoteSnapshotter, error) {
	return &proxy.RemoteSnapshotter{Snapshotter: &internal.FailingSnapshotter{}}, nil
}

func (c *RemoteSnapshotterCache) length() int {
	c.mutex.RLock()
	defer c.mutex.RUnlock()
	return len(c.snapshotters)
}

func TestCacheEvictionOnConnectionFailure(t *testing.T) {
	defer goleak.VerifyNone(t)

	dial := func(ctx context.Context, namespace string) (net.Conn, error) {
		// Error on dial to simulate unhealthy connection
		return nil, errors.New("mock dial error")
	}
	frequency := 2 * time.Millisecond
	dialer := proxy.Dialer{Dial: dial, Timeout: 1 * time.Second}
	cache := NewRemoteSnapshotterCache(getSnapshotter, EvictOnConnectionFailure(dialer, frequency))
	defer cache.Close()

	_, err := cache.Put(context.Background(), "test", proxy.RemoteSnapshotterConfig{})
	require.NoError(t, err, "Snapshotter not added to cache correctly")

	ctx, cancel := context.WithTimeout(context.Background(), 25*time.Millisecond)
	defer cancel()

	ticker := time.NewTimer(5 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			cache.mutex.RLock()
			length := len(cache.snapshotters)
			cache.mutex.RUnlock()
			if length == 0 {
				return
			}
		case <-ctx.Done():
			t.Error("Cache entry was not evicted")
		}
	}
}

func TestCacheNotEvictedIfConnectionIsHealthy(t *testing.T) {
	defer goleak.VerifyNone(t)

	dial := func(ctx context.Context, namespace string) (net.Conn, error) {
		// Return no error to simulate healthy connection
		return &net.UnixConn{}, nil
	}
	frequency := 1 * time.Millisecond
	dialer := proxy.Dialer{Dial: dial, Timeout: 1 * time.Second}
	cache := NewRemoteSnapshotterCache(getSnapshotter, EvictOnConnectionFailure(dialer, frequency))
	defer cache.Close()

	_, err := cache.Put(context.Background(), "test", proxy.RemoteSnapshotterConfig{})
	require.NoError(t, err, "Snapshotter not added to cache correctly")

	ctx, cancel := context.WithTimeout(context.Background(), 25*time.Millisecond)
	defer cancel()

	ticker := time.NewTimer(5 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			if cache.length() == 0 {
				t.Error("Cache entry was incorrectly evicted")
			}
		case <-ctx.Done():
			require.Len(t, cache.snapshotters, 1, "Cache entry was incorrectly evicted")
			return
		}
	}
}

func TestBackgroundEnforcersCanBeStopped(t *testing.T) {
	defer goleak.VerifyNone(t)

	dial := func(ctx context.Context, namespace string) (net.Conn, error) {
		// Return no error so the entry is not evicted via policy
		return &net.UnixConn{}, nil
	}
	frequency := 1 * time.Millisecond
	dialer := proxy.Dialer{Dial: dial, Timeout: 1 * time.Second}
	cache := NewRemoteSnapshotterCache(getSnapshotter, EvictOnConnectionFailure(dialer, frequency))

	_, err := cache.Put(context.Background(), "test", proxy.RemoteSnapshotterConfig{})
	require.NoError(t, err, "Snapshotter not added to cache correctly")

	cache.Close()

	require.Len(t, cache.snapshotters, 0, "Cache was not closed properly")
}

func TestLogErrorOnEvictionFailure(t *testing.T) {
	defer goleak.VerifyNone(t)

	dial := func(ctx context.Context, namespace string) (net.Conn, error) {
		return nil, errors.New("mock dial error")
	}
	frequency := 1 * time.Millisecond
	dialer := proxy.Dialer{Dial: dial, Timeout: 1 * time.Second}
	cache := NewRemoteSnapshotterCache(getErrorSnapshotter, EvictOnConnectionFailure(dialer, frequency))
	defer cache.Close()

	_, err := cache.Put(context.Background(), "test", proxy.RemoteSnapshotterConfig{})
	require.NoError(t, err, "Snapshotter not added to cache correctly")

	ctx, cancel := context.WithTimeout(context.Background(), 25*time.Millisecond)
	defer cancel()

	ticker := time.NewTimer(5 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			if cache.length() == 1 {
				continue
			}

			return
		case <-ctx.Done():
			require.Len(t, cache.snapshotters, 1, "Cache entry was never evicted")
			return
		}
	}
}
