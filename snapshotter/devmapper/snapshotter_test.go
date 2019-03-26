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
	"io/ioutil"
	"os"
	"testing"
	"time"

	"github.com/containerd/containerd/mount"
	"github.com/containerd/containerd/namespaces"
	"github.com/containerd/containerd/snapshots"
	"github.com/containerd/containerd/snapshots/testsuite"
	"github.com/containerd/continuity/fs/fstest"
	"github.com/hashicorp/go-multierror"
	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/firecracker-microvm/firecracker-containerd/internal"
	"github.com/firecracker-microvm/firecracker-containerd/snapshotter/pkg/dmsetup"
	"github.com/firecracker-microvm/firecracker-containerd/snapshotter/pkg/losetup"
)

func TestSnapshotterSuite(t *testing.T) {
	internal.RequiresRoot(t)
	logrus.SetLevel(logrus.DebugLevel)

	snapshotterFn := func(ctx context.Context, root string) (snapshots.Snapshotter, func() error, error) {
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
			result := multierror.Append(
				snap.pool.RemovePool(ctx),
				losetup.DetachLoopDevice(loopDataDevice, loopMetaDevice))

			return result.ErrorOrNil()
		}

		// Pool cleanup should be called before closing metadata store (as we need to retrieve device names)
		snap.cleanupFn = append([]closeFunc{removePool}, snap.cleanupFn...)

		return snap, snap.Close, nil
	}

	testsuite.SnapshotterSuite(t, "devmapper", snapshotterFn)

	ctx := context.Background()
	ctx = namespaces.WithNamespace(ctx, "testsuite")

	t.Run("DevMapperUsage", func(t *testing.T) {
		tempDir, err := ioutil.TempDir("", "snapshot-suite-usage")
		require.NoError(t, err)
		defer os.RemoveAll(tempDir)

		snapshotter, closer, err := snapshotterFn(ctx, tempDir)
		require.NoError(t, err)
		defer closer()

		testUsage(t, snapshotter)
	})
}

// testUsage tests devmapper's Usage implementation. This is an approximate test as it's hard to
// predict how many blocks will be consumed under different conditions and parameters.
func testUsage(t *testing.T, snapshotter snapshots.Snapshotter) {
	ctx := context.Background()

	// Create empty base layer
	_, err := snapshotter.Prepare(ctx, "prepare-1", "")
	require.NoError(t, err)

	emptyLayerUsage, err := snapshotter.Usage(ctx, "prepare-1")
	assert.NoError(t, err)

	// Should be > 0 as just written file system also consumes blocks
	assert.NotZero(t, emptyLayerUsage.Size)

	err = snapshotter.Commit(ctx, "layer-1", "prepare-1")
	require.NoError(t, err)

	// Create child layer with 1MB file

	var (
		sizeBytes   int64 = 1048576 // 1MB
		baseApplier       = fstest.Apply(fstest.CreateRandomFile("/a", 12345679, sizeBytes, 0777))
	)

	mounts, err := snapshotter.Prepare(ctx, "prepare-2", "layer-1")
	require.NoError(t, err)

	err = mount.WithTempMount(ctx, mounts, baseApplier.Apply)
	require.NoError(t, err)

	err = snapshotter.Commit(ctx, "layer-2", "prepare-2")
	require.NoError(t, err)

	layer2Usage, err := snapshotter.Usage(ctx, "layer-2")
	require.NoError(t, err)

	// Should be at least 1 MB + fs metadata
	assert.InDelta(t, sizeBytes, layer2Usage.Size, 256*dmsetup.SectorSize)
}
