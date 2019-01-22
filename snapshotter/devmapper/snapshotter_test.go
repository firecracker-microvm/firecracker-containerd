// Copyright 2018 Amazon.com, Inc. or its affiliates. All Rights Reserved.
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

package devmapper

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/containerd/containerd/snapshots"
	"github.com/containerd/containerd/snapshots/testsuite"
	"github.com/firecracker-microvm/firecracker-containerd/internal"
	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"

	"github.com/firecracker-microvm/firecracker-containerd/snapshotter/pkg/losetup"
)

func TestSnapshotterSuite(t *testing.T) {
	internal.RequiresRoot(t)
	logrus.SetLevel(logrus.DebugLevel)

	testsuite.SnapshotterSuite(t, "devmapper", func(ctx context.Context, root string) (snapshots.Snapshotter, func() error, error) {
		// Create loopback devices for each test case
		_, loopDataDevice := createLoopbackDevice(t, root)
		_, loopMetaDevice := createLoopbackDevice(t, root)

		config := &Config{
			RootPath:       root,
			PoolName:       fmt.Sprintf("containerd-snapshotter-suite-pool-%d", time.Now().Nanosecond()),
			DataDevice:     loopDataDevice,
			MetadataDevice: loopMetaDevice,
			DataBlockSize:  "64Kb",
			BaseImageSize:  "16Mb",
		}

		snap, err := NewSnapshotter(context.Background(), config)
		if err != nil {
			return nil, nil, err
		}

		// Remove device mapper pool after test completes
		removePool := func() error {
			return snap.pool.RemovePool(ctx)
		}

		// Pool cleanup should be called before closing metadata store (as we need to retrieve device names)
		snap.cleanupFn = append([]closeFunc{removePool}, snap.cleanupFn...)

		return snap, func() error {
			err := snap.Close()
			assert.NoErrorf(t, err, "failed to close snapshotter")

			err = losetup.DetachLoopDevice(loopDataDevice, loopMetaDevice)
			assert.NoErrorf(t, err, "failed to detach loop devices")

			return err
		}, nil
	})
}
