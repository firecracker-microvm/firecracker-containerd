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
	"net"
	"time"
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

	dial      func(context.Context, string) (net.Conn, error)
	frequency time.Duration
}

// NewEvictOnConnectionFailurePolicy creates a new policy to evict on remote snapshotter
// connection failure on a specified frequency duration.
func NewEvictOnConnectionFailurePolicy(evictChan chan string, stopCondition chan struct{}, dial func(context.Context, string) (net.Conn, error), frequency time.Duration) EvictionPolicy {
	return &EvictOnConnectionFailurePolicy{evictionPolicy: evictionPolicy{evict: evictChan, stop: stopCondition}, dial: dial, frequency: frequency}
}

// Enforce launches a go routine which periodically attempts to dial the cached entry
// using the provided dial function.
//
// On connection failure, the entry will be evicted from cache.
func (p *EvictOnConnectionFailurePolicy) Enforce(key string) {
	go func(dial func(context.Context, string) (net.Conn, error)) {
		ticker := time.NewTicker(p.frequency)
		defer ticker.Stop()

		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		for {
			select {
			case <-ticker.C:
				conn, err := dial(ctx, key)
				if err != nil {
					p.evict <- key
					return
				}
				conn.Close()
			case <-p.stop:
				return
			}
		}
	}(p.dial)
}
