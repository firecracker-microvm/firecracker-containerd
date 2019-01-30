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

package naive

import (
	"context"
	_ "crypto/sha256"
	"io/ioutil"
	"os"
	"path/filepath"
	"testing"

	"github.com/containerd/containerd/snapshots"
	"github.com/containerd/containerd/snapshots/testsuite"
	"github.com/firecracker-microvm/firecracker-containerd/internal"
)

func TestCreateImage(t *testing.T) {
	internal.RequiresRoot(t)
	snap := Snapshotter{}

	tempDir, err := ioutil.TempDir("", "fc-snapshotter")
	if err != nil {
		t.Fatal(err)
	}

	defer os.RemoveAll(tempDir)

	imgPath := filepath.Join(tempDir, "x.img")

	const (
		sizeMiB   = 100
		sizeBytes = sizeMiB * mib
	)

	err = snap.createImage(context.Background(), imgPath, sizeMiB)
	if err != nil {
		t.Fatal(err)
	}

	if stat, err := os.Stat(imgPath); os.IsNotExist(err) {
		t.Fatal("error creating image file")
	} else if stat.Size() != sizeBytes {
		t.Errorf("wrong image size %d != %d", stat.Size(), sizeBytes)
	}
}

func createSnapshotter(ctx context.Context, root string) (snapshots.Snapshotter, func() error, error) {
	snap, err := NewSnapshotter(ctx, root)
	if err != nil {
		return nil, nil, err
	}

	return snap, snap.Close, nil
}

func TestSnapshotterSuite(t *testing.T) {
	internal.RequiresRoot(t)
	testsuite.SnapshotterSuite(t, "Snapshotter", createSnapshotter)
}
