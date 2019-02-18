// Copyright 2018-2019 Amazon.com, Inc. or its affiliates. All Rights Reserved.
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
	"github.com/hashicorp/go-multierror"
	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/require"

	"github.com/firecracker-microvm/firecracker-containerd/internal"
	"github.com/firecracker-microvm/firecracker-containerd/snapshotter/pkg/dmsetup"
	"github.com/firecracker-microvm/firecracker-containerd/snapshotter/pkg/losetup"
)

func TestSnapshotterSuite(t *testing.T) {
	internal.RequiresRoot(t)
	logrus.SetLevel(logrus.DebugLevel)

	testsuite.SnapshotterSuite(t, "devmapper", func(ctx context.Context, root string) (snapshots.Snapshotter, func() error, error) {
		// Create loopback devices for each test case
		_, loopDataDevice := createLoopbackDevice(t, root)
		_, loopMetaDevice := createLoopbackDevice(t, root)

		poolName := fmt.Sprintf("containerd-snapshotter-suite-pool-%d", time.Now().Nanosecond())
		err := dmsetup.CreatePool(poolName, loopDataDevice, loopMetaDevice, 64*1024/dmsetup.SectorSize)
		require.NoErrorf(t, err, "failed to create pool %q", poolName)

		config := &Config{
			RootPath:      root,
			PoolName:      poolName,
			BaseImageSize: "16Mb",
		}

		snap, err := NewSnapshotter(context.Background(), config)
		if err != nil {
			return nil, nil, err
		}

		// Remove device mapper pool and detach loop devices after test completes
		removePool := func() error {
			var result *multierror.Error

			if err := snap.pool.RemovePool(ctx); err != nil {
				result = multierror.Append(result, err)
			}

			if err := losetup.DetachLoopDevice(loopDataDevice, loopMetaDevice); err != nil {
				result = multierror.Append(result, err)
			}

			return result.ErrorOrNil()
		}

		// Pool cleanup should be called before closing metadata store (as we need to retrieve device names)
		snap.cleanupFn = append([]closeFunc{removePool}, snap.cleanupFn...)

		return snap, snap.Close, nil
	})
}
