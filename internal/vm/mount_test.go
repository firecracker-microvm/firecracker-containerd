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

package vm

import (
	"testing"

	"github.com/containerd/containerd/api/types"
	"github.com/containerd/containerd/mount"
	"github.com/stretchr/testify/assert"
)

func TestIsLocalMount(t *testing.T) {
	mnt := types.Mount{
		Type:    "bind",
		Source:  "/tmp/snapshots/1/fs",
		Options: []string{"rbind"},
	}
	assert.False(t, IsLocalMount(&mnt), "Standard mount was considered vm local")
}

func TestAddLocalMountIdentifier(t *testing.T) {
	mnt := mount.Mount{
		Type:    "bind",
		Source:  "/tmp/snapshots/1/fs",
		Options: []string{"rbind"},
	}
	localMnt := AddLocalMountIdentifier(mnt)

	mntProto := types.Mount{
		Type:    mnt.Type,
		Source:  mnt.Source,
		Options: mnt.Options,
	}
	localMntProto := types.Mount{
		Type:    localMnt.Type,
		Source:  localMnt.Source,
		Options: localMnt.Options,
	}

	assert.False(t, IsLocalMount(&mntProto), "Standard mount was considered vm local")
	assert.True(t, IsLocalMount(&localMntProto), "Mount was not vm local after adding the local mount identifier")
}

func TestStripLocalMountIdentifier(t *testing.T) {
	mnt := mount.Mount{
		Type:    "bind",
		Source:  "/tmp/snapshots/1/fs",
		Options: []string{"rbind"},
	}
	localMnt := AddLocalMountIdentifier(mnt)
	localMntProto := &types.Mount{
		Type:    localMnt.Type,
		Source:  localMnt.Source,
		Options: localMnt.Options,
	}
	assert.True(t, IsLocalMount(localMntProto), "Mount was not vm local after adding the local mount identifier")
	localMntProto = StripLocalMountIdentifier(localMntProto)
	assert.False(t, IsLocalMount(localMntProto), "Mount was vm local after stripping the local mount identifier")
}
