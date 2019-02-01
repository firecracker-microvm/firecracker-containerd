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

package snapshotter

import (
	"context"
	"flag"
	"fmt"
	"os"
	"sync/atomic"
	"testing"
	"time"

	"github.com/containerd/containerd/mount"
	"github.com/containerd/containerd/snapshots"
	"github.com/containerd/containerd/snapshots/overlay"
	"github.com/containerd/continuity/fs/fstest"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/firecracker-microvm/firecracker-containerd/snapshotter/devmapper"
	"github.com/firecracker-microvm/firecracker-containerd/snapshotter/naive"
)

var (
	dmPoolDev       string
	dmRootPath      string
	overlayRootPath string
	naiveRootPath   string
)

func init() {
	flag.StringVar(&dmPoolDev, "dm.thinPoolDev", "", "Pool device to run benchmark on")
	flag.StringVar(&dmRootPath, "dm.rootPath", "", "Root dir for devmapper snapshotter")
	flag.StringVar(&overlayRootPath, "overlay.rootPath", "", "Root dir for overlay snapshotter")
	flag.StringVar(&naiveRootPath, "naive.rootPath", "", "Root dir for naive snapshotter")
}

func BenchmarkNaive(b *testing.B) {
	if naiveRootPath == "" {
		b.Skip("naive snapshotter root dir must be provided")
	}

	snapshotter, err := naive.NewSnapshotter(context.Background(), naiveRootPath)
	require.NoErrorf(b, err, "failed to create naive snapshotter")

	defer func() {
		err = snapshotter.Close()
		assert.NoError(b, err)

		err = os.RemoveAll(naiveRootPath)
		assert.NoError(b, err)
	}()

	benchmarkSnapshotter(b, snapshotter)
}

func BenchmarkOverlay(b *testing.B) {
	if overlayRootPath == "" {
		b.Skip("overlay root dir must be provided")
	}

	snapshotter, err := overlay.NewSnapshotter(overlayRootPath)
	require.NoErrorf(b, err, "failed to create overlay snapshotter")

	defer func() {
		err = snapshotter.Close()
		assert.NoError(b, err)

		err = os.RemoveAll(overlayRootPath)
		assert.NoError(b, err)
	}()

	benchmarkSnapshotter(b, snapshotter)
}

func BenchmarkDeviceMapper(b *testing.B) {
	if dmPoolDev == "" {
		b.Skip("devmapper benchmark requires thin-pool device to be prepared in advance and provided")
	}

	if dmRootPath == "" {
		b.Skip("devmapper snapshotter root dir must be provided")
	}

	config := &devmapper.Config{
		PoolName:      dmPoolDev,
		RootPath:      dmRootPath,
		BaseImageSize: "16Mb",
	}

	ctx := context.Background()

	snapshotter, err := devmapper.NewSnapshotter(ctx, config)
	require.NoError(b, err)

	defer func() {
		err := snapshotter.ResetPool(ctx)
		assert.NoError(b, err)

		err = snapshotter.Close()
		assert.NoError(b, err)

		err = os.RemoveAll(dmRootPath)
		assert.NoError(b, err)
	}()

	benchmarkSnapshotter(b, snapshotter)
}

// benchmarkSnapshotter tests snapshotter performance.
// It writes 16 layers with randomly created, modified, or removed files.
// Depending on layer index different sets of files are modified.
func benchmarkSnapshotter(b *testing.B, snapshotter snapshots.Snapshotter) {
	const (
		layerCount    = 16
		fileSizeBytes = int64(1 * 1024 * 1024) // 1 MB
	)

	var (
		total      = 0
		layers     = make([]fstest.Applier, layerCount)
		layerIndex = int64(0)
	)

	for i := 0; i < layerCount; i++ {
		appliers := makeApplier(i, fileSizeBytes)
		layers[i] = fstest.Apply(appliers...)
		total += len(appliers)
	}

	b.SetBytes(int64(total) * fileSizeBytes)
	b.ResetTimer()

	b.RunParallel(func(pb *testing.PB) {
		ctx := context.Background()

		for pb.Next() {
			parent := ""

			for l := 0; l < layerCount; l++ {
				current := fmt.Sprintf("prepare-layer-%d", atomic.AddInt64(&layerIndex, 1))

				mounts, err := snapshotter.Prepare(ctx, current, parent)
				require.NoError(b, err)

				err = mount.WithTempMount(ctx, mounts, layers[l].Apply)
				require.NoError(b, err)

				parent = fmt.Sprintf("comitted-%d", atomic.AddInt64(&layerIndex, 1))
				err = snapshotter.Commit(ctx, parent, current)
				require.NoError(b, err)
			}
		}
	})
}

func makeApplier(layerIndex int, fileSizeBytes int64) []fstest.Applier {
	seed := time.Now().UnixNano()

	switch {
	case layerIndex%3 == 0:
		return []fstest.Applier{
			fstest.CreateRandomFile("/e", seed, fileSizeBytes, 0777),
			fstest.CreateRandomFile("/f", seed, fileSizeBytes, 0777),
			fstest.RemoveAll("/g"),
			fstest.CreateRandomFile("/h", seed, fileSizeBytes, 0777),
			fstest.CreateRandomFile("/i", seed, fileSizeBytes, 0777),
			fstest.CreateRandomFile("/j", seed, fileSizeBytes, 0777),
		}
	case layerIndex%2 == 0:
		return []fstest.Applier{
			fstest.CreateRandomFile("/a", seed, fileSizeBytes, 0777),
			fstest.CreateRandomFile("/b", seed, fileSizeBytes, 0777),
			fstest.RemoveAll("/c"),
			fstest.CreateRandomFile("/d", seed, fileSizeBytes, 0777),
			fstest.CreateRandomFile("/e", seed, fileSizeBytes, 0777),
			fstest.RemoveAll("/f"),
			fstest.CreateRandomFile("/g", seed, fileSizeBytes, 0777),
		}
	default:
		return []fstest.Applier{
			fstest.CreateRandomFile("/a", seed, fileSizeBytes, 0777),
			fstest.CreateRandomFile("/b", seed, fileSizeBytes, 0777),
			fstest.CreateRandomFile("/c", seed, fileSizeBytes, 0777),
			fstest.CreateRandomFile("/d", seed, fileSizeBytes, 0777),
			fstest.CreateRandomFile("/e", seed, fileSizeBytes, 0777),
			fstest.CreateRandomFile("/f", seed, fileSizeBytes, 0777),
			fstest.CreateRandomFile("/g", seed, fileSizeBytes, 0777),
			fstest.CreateRandomFile("/h", seed, fileSizeBytes, 0777),
			fstest.CreateRandomFile("/i", seed, fileSizeBytes, 0777),
			fstest.CreateRandomFile("/j", seed, fileSizeBytes, 0777),
		}
	}
}
