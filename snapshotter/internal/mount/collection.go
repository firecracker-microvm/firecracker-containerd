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

package mount

import "github.com/containerd/containerd/mount"

// Map returns the slice of results after applying the given function
// to each item of a given slice.
func Map(mounts []mount.Mount, f func(mount.Mount) mount.Mount) []mount.Mount {
	fms := make([]mount.Mount, len(mounts))
	for i, mount := range mounts {
		fms[i] = f(mount)
	}
	return fms
}
