// Copyright 2018-2019 Amazon.com, Inc. or its affiliates. All Rights Reserved.
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

package main

import (
	"testing"

	"github.com/firecracker-microvm/firecracker-containerd/proto"
	firecracker "github.com/firecracker-microvm/firecracker-go-sdk"
	models "github.com/firecracker-microvm/firecracker-go-sdk/client/models"
	"gotest.tools/assert"
)

const (
	oneTimeBurst = 800
	refillTime   = 1000
	capacity     = 400
)

func TestOverrideVMConfigFromTaskOpts(t *testing.T) {
	var testCases = []struct {
		name        string
		in          firecracker.Config
		vmConfig    *proto.FirecrackerConfig
		expectedOut firecracker.Config
	}{
		{
			name: "network config",
			in:   firecracker.Config{},
			vmConfig: &proto.FirecrackerConfig{
				NetworkInterfaces: []*proto.FirecrackerNetworkInterface{
					{
						MacAddress:  mac,
						HostDevName: hostDevName,
						AllowMMDS:   true,
						InRateLimiter: &proto.FirecrackerRateLimiter{
							Bandwidth: &proto.FirecrackerTokenBucket{
								OneTimeBurst: oneTimeBurst,
								RefillTime:   refillTime,
								Capacity:     capacity,
							},
						},
						OutRateLimiter: &proto.FirecrackerRateLimiter{
							Bandwidth: &proto.FirecrackerTokenBucket{
								OneTimeBurst: oneTimeBurst,
								RefillTime:   refillTime,
								Capacity:     capacity,
							},
						},
					},
					{
						MacAddress:  mac,
						HostDevName: hostDevName,
					},
				},
			},
			expectedOut: firecracker.Config{
				NetworkInterfaces: []firecracker.NetworkInterface{
					{
						MacAddress:  mac,
						HostDevName: hostDevName,
						AllowMMDS:   true,
						InRateLimiter: &models.RateLimiter{
							Bandwidth: &models.TokenBucket{
								OneTimeBurst: firecracker.Int64(oneTimeBurst),
								RefillTime:   firecracker.Int64(refillTime),
								Size:         firecracker.Int64(capacity),
							},
						},
						OutRateLimiter: &models.RateLimiter{
							Bandwidth: &models.TokenBucket{
								OneTimeBurst: firecracker.Int64(oneTimeBurst),
								RefillTime:   firecracker.Int64(refillTime),
								Size:         firecracker.Int64(capacity),
							},
						},
					},
					{
						MacAddress:  mac,
						HostDevName: hostDevName,
					},
				},
			},
		},
	}
	for _, tc := range testCases {
		// scopelint gets sad if we use tc directly. The specific complaint is
		// "Using the variable on range scope in function literal".
		test := tc
		t.Run(test.name, func(t *testing.T) {
			out := overrideVMConfigFromTaskOpts(test.in, test.vmConfig)
			assert.DeepEqual(t, test.expectedOut, out)
		})
	}
}
