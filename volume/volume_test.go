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

// Package volume provides volumes like Docker and Amazon ECS.
// Volumes are specicial directories that could be shared by multiple containers.
//
// https://docs.aws.amazon.com/AmazonECS/latest/developerguide/task_definition_parameters.html#volumes
package volume

import (
	"context"
	"os"
	"testing"

	"github.com/containerd/containerd/mount"
	"github.com/stretchr/testify/require"
)

const mib = 1024 * 1024

func TestCreateDiskImage(t *testing.T) {
	ctx := context.Background()

	vs := &Set{}

	_, err := vs.createDiskImage(ctx, 10)
	require.Errorf(t, err, "10 bytes is too small to have a valid %s image", fsType)

	path, err := vs.createDiskImage(ctx, 100*mib)
	require.NoError(t, err)

	defer os.Remove(path)

	uid := os.Getuid()
	if uid != 0 {
		t.Logf("skip mounting %s since uid=%d", path, uid)
		return
	}

	// Make sure that the disk image is valid.
	target := t.TempDir()
	err = mountDiskImage(path, target)
	require.NoError(t, err)

	err = mount.Unmount(target, 0)
	require.NoError(t, err)
}
