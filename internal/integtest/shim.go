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

// Package integtest provides integration testing utilities.
package integtest

import "os"

const shimBaseDirEnvVar = "SHIM_BASE_DIR"
const defaultShimBaseDir = "/srv/firecracker_containerd_tests"

// ShimBaseDir checks the "SHIM_BASE_DIR" environment variable and returns its value
// if it exists, else returns the default value
func ShimBaseDir() string {
	if v := os.Getenv(shimBaseDirEnvVar); v != "" {
		return v
	}

	return defaultShimBaseDir
}
