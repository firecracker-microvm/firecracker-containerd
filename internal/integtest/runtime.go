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

package integtest

import (
	"os"
	"strconv"
)

const (
	// FirecrackerRuntime is the Firecracker-containerd runtime
	FirecrackerRuntime = "aws.firecracker"

	containerdSockPathEnvVar = "CONTAINERD_SOCKET"
	numberOfVmsEnvVar        = "NUMBER_OF_VMS"
)

var (
	// ContainerdSockPath is the default Firecracker-containerd socket path
	ContainerdSockPath = "/run/firecracker-containerd/containerd.sock"

	// NumberOfVms is the number of VMs used in integration testing set
	// by either environment variable read or defaults to 5 VMs.
	NumberOfVms = 5
)

func init() {
	if v := os.Getenv(containerdSockPathEnvVar); v != "" {
		ContainerdSockPath = v
	}
	if v := os.Getenv(numberOfVmsEnvVar); v != "" {
		NumberOfVms, _ = strconv.Atoi(v)
	}
}
