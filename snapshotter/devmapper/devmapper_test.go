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
	"encoding/json"
	"fmt"
	"io/ioutil"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/containerd/containerd/snapshots"
	"github.com/containerd/containerd/snapshots/testsuite"
	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSnapshotterSuite(t *testing.T) {
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

		configPath := filepath.Join(root, "config.json")
		saveConfig(t, configPath, config)

		snap, err := NewSnapshotter(context.Background(), configPath)
		if err != nil {
			return nil, nil, err
		}

		return snap, func() error {
			err := snap.Close()
			assert.NoErrorf(t, err, "failed to close snapshotter")

			err = snap.pool.Close(context.Background(), true, true)
			assert.NoErrorf(t, err, "failed to cleanup thin-pool")

			err = exec.Command("losetup", "--detach", loopDataDevice, loopMetaDevice).Run()
			assert.NoErrorf(t, err, "failed to detach loop devices")

			return err
		}, nil
	})
}

func saveConfig(t *testing.T, path string, config *Config) {
	data, err := json.Marshal(config)
	require.NoError(t, err)

	err = ioutil.WriteFile(path, data, 0700)
	require.NoError(t, err)
}
