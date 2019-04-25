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

	proto "github.com/firecracker-microvm/firecracker-containerd/proto/grpc"
	firecracker "github.com/firecracker-microvm/firecracker-go-sdk"
	models "github.com/firecracker-microvm/firecracker-go-sdk/client/models"
	"github.com/stretchr/testify/assert"
)

const (
	oneTimeBurst  = 800
	refillTime    = 1000
	capacity      = 400
	rootDrivePath = "/path/to/root/drive"
	loopDrivePath = "/path/to/loop/drive"
	loopDriveRO   = false
	memSize       = 4096
	vcpuCount     = 2
)

var (
	defaultDrivesBuilder = firecracker.NewDrivesBuilder(rootDrivePath).AddDrive(
		loopDrivePath, loopDriveRO)
	defaultDrives             = defaultDrivesBuilder.Build()
	overrideRootDrivesBuilder = firecracker.NewDrivesBuilder(loopDrivePath)
	additionalDrivesBuilder   = firecracker.NewDrivesBuilder(rootDrivePath).AddDrive(
		loopDrivePath, loopDriveRO).AddDrive(
		loopDrivePath, loopDriveRO, func(d *models.Drive) {
			d.RateLimiter = &models.RateLimiter{
				Ops: &models.TokenBucket{
					OneTimeBurst: firecracker.Int64(oneTimeBurst),
					RefillTime:   firecracker.Int64(refillTime),
					Size:         firecracker.Int64(capacity),
				},
			}
		})
)

func TestOverrideVMConfigFromTaskOpts(t *testing.T) {
	var testCases = []struct {
		name            string
		in              firecracker.Config
		inDrivesBuilder firecracker.DrivesBuilder
		vmConfig        *proto.FirecrackerConfig
		expectedOut     firecracker.Config
		expectedDrives  []models.Drive
		expectedErr     bool
	}{
		{
			name:            "network config",
			in:              firecracker.Config{},
			inDrivesBuilder: defaultDrivesBuilder,
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
			expectedErr: false,
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
			expectedDrives: defaultDrives,
		},
		{
			name:            "machine config",
			in:              firecracker.Config{},
			inDrivesBuilder: defaultDrivesBuilder,
			vmConfig: &proto.FirecrackerConfig{
				MachineCfg: &proto.FirecrackerMachineConfiguration{
					MemSizeMib: memSize,
					VcpuCount:  vcpuCount,
				},
			},
			expectedErr: false,
			expectedOut: firecracker.Config{
				MachineCfg: models.MachineConfiguration{
					MemSizeMib: memSize,
					VcpuCount:  vcpuCount,
				},
			},
			expectedDrives: defaultDrivesBuilder.Build(),
		},
		{
			name:            "invalid memory in machine config",
			in:              firecracker.Config{},
			inDrivesBuilder: defaultDrivesBuilder,
			vmConfig: &proto.FirecrackerConfig{
				MachineCfg: &proto.FirecrackerMachineConfiguration{
					VcpuCount: vcpuCount,
				},
			},
			expectedErr:    true,
			expectedOut:    firecracker.Config{},
			expectedDrives: defaultDrivesBuilder.Build(),
		},
		{
			name:            "invalid vcpu count in machine config",
			in:              firecracker.Config{},
			inDrivesBuilder: defaultDrivesBuilder,
			vmConfig: &proto.FirecrackerConfig{
				MachineCfg: &proto.FirecrackerMachineConfiguration{
					MemSizeMib: memSize,
				},
			},
			expectedErr:    true,
			expectedOut:    firecracker.Config{},
			expectedDrives: defaultDrivesBuilder.Build(),
		},
		{
			name:            "storage config overrides root drive",
			in:              firecracker.Config{},
			inDrivesBuilder: overrideRootDrivesBuilder,
			vmConfig: &proto.FirecrackerConfig{
				RootDrive: &proto.FirecrackerDrive{
					PathOnHost: loopDrivePath,
				},
			},
			expectedErr:    false,
			expectedOut:    firecracker.Config{},
			expectedDrives: overrideRootDrivesBuilder.Build(),
		},
		{
			name:            "storage config adds additional drives",
			in:              firecracker.Config{},
			inDrivesBuilder: defaultDrivesBuilder,
			vmConfig: &proto.FirecrackerConfig{
				AdditionalDrives: []*proto.FirecrackerDrive{
					{
						PathOnHost: loopDrivePath,
						RateLimiter: &proto.FirecrackerRateLimiter{
							Ops: &proto.FirecrackerTokenBucket{
								OneTimeBurst: oneTimeBurst,
								RefillTime:   refillTime,
								Capacity:     capacity,
							},
						},
					},
				},
			},
			expectedErr:    false,
			expectedOut:    firecracker.Config{},
			expectedDrives: additionalDrivesBuilder.Build(),
		},
	}
	for _, tc := range testCases {
		// scopelint gets sad if we use tc directly. The specific complaint is
		// "Using the variable on range scope in function literal".
		test := tc
		t.Run(test.name, func(t *testing.T) {
			out, drivesBuilder, err := overrideVMConfigFromTaskOpts(test.in, test.vmConfig, test.inDrivesBuilder)
			if test.expectedErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
				assert.Equal(t, test.expectedOut, out)
				assert.Equal(t, test.expectedDrives, drivesBuilder.Build())
			}
		})
	}
}
