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

import (
	"github.com/firecracker-microvm/firecracker-containerd/config"
	"github.com/firecracker-microvm/firecracker-containerd/proto"
)

// WithDefaultNetwork is an option to use the default network configuration
// in the runtime configuration for integration testing
func WithDefaultNetwork() func(c *config.Config) {
	return func(c *config.Config) {
		c.DefaultNetworkInterfaces = []proto.FirecrackerNetworkInterface{
			{
				CNIConfig: &proto.CNIConfiguration{
					NetworkName:   "fcnet",
					InterfaceName: "veth0",
				},
			},
		}
	}
}
