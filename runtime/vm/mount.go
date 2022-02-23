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
	"github.com/containerd/containerd/mount"
	"github.com/firecracker-microvm/firecracker-containerd/internal/vm"
)

// AddLocalMountIdentifier adds an identifier to a mount so that it can be detected by IsLocalMount.
// This is intended to be used by cooperating snapshotters to mark mounts as inside the VM so they
// can be plumbed at the proper points inside/outside the VM.
func AddLocalMountIdentifier(mnt mount.Mount) mount.Mount {
	return vm.AddLocalMountIdentifier(mnt)
}
