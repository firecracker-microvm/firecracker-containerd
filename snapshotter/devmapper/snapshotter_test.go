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
	"io/ioutil"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/containerd/containerd/mount"
	"github.com/containerd/containerd/snapshots"
	"github.com/containerd/containerd/snapshots/testsuite"
	"github.com/containerd/continuity/fs/fstest"
	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/firecracker-microvm/firecracker-containerd/internal"
	"github.com/firecracker-microvm/firecracker-containerd/snapshotter/pkg/dmsetup"
	"github.com/firecracker-microvm/firecracker-containerd/snapshotter/pkg/losetup"
)

func TestSnapshotterSuite(t *testing.T) {
	internal.RequiresRoot(t)

	testsuite.SnapshotterSuite(t, "devmapper", func(ctx context.Context, root string) (snapshots.Snapshotter, func() error, error) {
		snap := createSnapshotter(t, root)
		return snap, snap.Close, nil
	})
}

func TestSnapshotterUsage(t *testing.T) {
	internal.RequiresRoot(t)

	tempDir, err := ioutil.TempDir("", "devmapper-snapshotter-")
	require.NoError(t, err)

	snap := createSnapshotter(t, tempDir)
	defer func() {
		err := snap.Close()
		assert.NoError(t, err)

		err = os.RemoveAll(tempDir)
		assert.NoError(t, err)
	}()

	preparing := filepath.Join(tempDir, "preparing")
	err = os.MkdirAll(preparing, 0700)
	require.NoError(t, err)

	mounts, err := snap.Prepare(context.Background(), preparing, "")
	require.NoError(t, err)

	initialApplier := fstest.Apply(
		fstest.CreateFile("/foo", []byte("foo\n"), 0777),
		fstest.CreateDir("/a", 0755),
	)

	err = mount.WithTempMount(context.Background(), mounts, initialApplier.Apply)
	require.NoError(t, err)

	t.Run("ActiveSnapshotUsage", func(t *testing.T) {
		usage, err := snap.Usage(context.Background(), preparing)
		assert.NoError(t, err)
		assert.NotZero(t, usage.Size)
	})

	committed := filepath.Join(tempDir, "committed")
	err = snap.Commit(context.Background(), committed, preparing)
	require.NoError(t, err)

	t.Run("CommittedSnapshotUsage", func(t *testing.T) {
		usage, err := snap.Usage(context.Background(), committed)
		assert.NoError(t, err)
		assert.NotZero(t, usage.Size)
	})
}

func createSnapshotter(t *testing.T, root string) *Snapshotter {
	logrus.SetLevel(logrus.DebugLevel)

	_, loopDataDevice := createLoopbackDevice(t, root)
	_, loopMetaDevice := createLoopbackDevice(t, root)

	poolName := fmt.Sprintf("containerd-snapshotter-suite-pool-%d", time.Now().Nanosecond())
	err := dmsetup.CreatePool(poolName, loopDataDevice, loopMetaDevice, 64*1024/dmsetup.SectorSize)
	require.NoErrorf(t, err, "failed to create pool %q", poolName)

	config := &Config{
		RootPath:           root,
		PoolName:           poolName,
		BaseImageSize:      "16Mb",
		BaseImageSizeBytes: 16 * 1024 * 1024,
	}

	snap, err := NewSnapshotter(context.Background(), config)
	require.NoErrorf(t, err, "unable to create snapshotter %q", config.PoolName)

	// Remove device mapper pool after test completes
	removePool := func() error {
		return snap.pool.RemovePool(context.Background())
	}

	// Detach data/metadata loop devices
	detachLoop := func() error {
		return losetup.DetachLoopDevice(loopDataDevice, loopMetaDevice)
	}

	// Pool cleanup should be called before closing metadata store (as we need to retrieve device names)
	snap.cleanupFn = append([]closeFunc{removePool}, snap.cleanupFn...)

	// Loop devices need to be detached at the very end
	snap.cleanupFn = append(snap.cleanupFn, detachLoop)

	return snap
}
