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
	"strings"

	"github.com/containerd/containerd/api/types"
	"github.com/containerd/containerd/mount"
)

const vmLocalMountTypePrefix = "vm:"

// IsLocalMount returns true if the mount source is inside the VM as opposed to the
// default assumption that the mount source is a block device on the host.
func IsLocalMount(mount *types.Mount) bool {
	return mount != nil && strings.HasPrefix(mount.Type, vmLocalMountTypePrefix)
}

// AddLocalMountIdentifier adds an identifier to a mount so that it can be detected by IsLocalMount.
// This is intended to be used by cooperating snapshotters to mark mounts as inside the VM so they
// can be plumbed at the proper points inside/outside the VM.
func AddLocalMountIdentifier(mnt mount.Mount) mount.Mount {
	return mount.Mount{
		Type:    vmLocalMountTypePrefix + mnt.Type,
		Source:  mnt.Source,
		Options: mnt.Options,
	}
}

// StripLocalMountIdentifier removes the identifier that signals that a mount
// is inside the VM. This is used before passing the mount information to runc
func StripLocalMountIdentifier(mnt *types.Mount) *types.Mount {
	options := make([]string, len(mnt.Options))
	copy(options, mnt.Options)
	return &types.Mount{
		Type:    strings.Replace(mnt.Type, vmLocalMountTypePrefix, "", 1),
		Source:  mnt.Source,
		Target:  mnt.Target,
		Options: options,
	}
}
