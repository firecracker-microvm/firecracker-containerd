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
	"time"

	"github.com/firecracker-microvm/firecracker-containerd/snapshotter/demux/proxy"
)

// EvictionPolicy defines the interface for enforcing a cache eviction policy.
type EvictionPolicy interface {
	// Enforce monitors for policy failures.
	Enforce(key string)
}

type evictionPolicy struct {
	evict chan string
	stop  chan struct{}
}

// EvictOnConnectionFailurePolicy defines an eviction policy where entries are evicted
// from cache after a failed connection attempt occurs.
type EvictOnConnectionFailurePolicy struct {
	evictionPolicy

	dialer proxy.Dialer

	frequency time.Duration
}

// NewEvictOnConnectionFailurePolicy creates a new policy to evict on remote snapshotter
// connection failure on a specified frequency duration.
func NewEvictOnConnectionFailurePolicy(evictChan chan string, dialer proxy.Dialer, frequency time.Duration, stopCondition chan struct{}) EvictionPolicy {
	return &EvictOnConnectionFailurePolicy{evictionPolicy: evictionPolicy{evict: evictChan, stop: stopCondition}, dialer: dialer, frequency: frequency}
}

// Enforce launches a go routine which periodically attempts to dial the cached entry
// using the provided dial function.
//
// On connection failure, the entry will be evicted from cache.
func (p EvictOnConnectionFailurePolicy) Enforce(key string) {
	go func() {
		ticker := time.NewTicker(p.frequency)
		defer ticker.Stop()

		for {
			select {
			case <-ticker.C:
				ctx, cancel := context.WithTimeout(context.Background(), p.dialer.Timeout)
				conn, err := p.dialer.Dial(ctx, key)
				if err != nil {
					p.evict <- key
					cancel()
					return
				}
				cancel()
				conn.Close()
			case <-p.stop:
				return
			}
		}
	}()
}
