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

	"github.com/firecracker-microvm/firecracker-go-sdk"
	models "github.com/firecracker-microvm/firecracker-go-sdk/client/models"
	"github.com/stretchr/testify/assert"

	"github.com/firecracker-microvm/firecracker-containerd/proto"
)

const (
	oneTimeBurst = 800
	refillTime   = 1000
	capacity     = 400
	memSize      = 4096
	vcpuCount    = 2
)

func TestMachineConfigurationFromProto(t *testing.T) {
	testcases := []struct {
		name                  string
		config                *Config
		proto                 *proto.FirecrackerMachineConfiguration
		expectedMachineConfig models.MachineConfiguration
	}{
		{
			name:   "ProtoOnly",
			config: &Config{},
			proto: &proto.FirecrackerMachineConfiguration{
				CPUTemplate: string(models.CPUTemplateC3),
				VcpuCount:   vcpuCount,
				MemSizeMib:  memSize,
				HtEnabled:   true,
			},
			expectedMachineConfig: models.MachineConfiguration{
				CPUTemplate: models.CPUTemplateC3,
				VcpuCount:   vcpuCount,
				MemSizeMib:  memSize,
				HtEnabled:   true,
			},
		},
		{
			name: "ConfigOnly",
			config: &Config{
				CPUTemplate: "C3",
				CPUCount:    vcpuCount,
			},
			proto: &proto.FirecrackerMachineConfiguration{},
			expectedMachineConfig: models.MachineConfiguration{
				CPUTemplate: models.CPUTemplateC3,
				VcpuCount:   vcpuCount,
				MemSizeMib:  defaultMemSizeMb,
				HtEnabled:   false,
			},
		},
		{
			name: "NilProto",
			config: &Config{
				CPUTemplate: "C3",
				CPUCount:    vcpuCount,
			},
			expectedMachineConfig: models.MachineConfiguration{
				CPUTemplate: models.CPUTemplateC3,
				VcpuCount:   vcpuCount,
				MemSizeMib:  defaultMemSizeMb,
				HtEnabled:   false,
			},
		},
		{
			name: "Overrides",
			config: &Config{
				CPUTemplate: "T2",
				CPUCount:    vcpuCount + 1,
			},
			proto: &proto.FirecrackerMachineConfiguration{
				CPUTemplate: string(models.CPUTemplateC3),
				VcpuCount:   vcpuCount,
				MemSizeMib:  memSize,
				HtEnabled:   true,
			},
			expectedMachineConfig: models.MachineConfiguration{
				CPUTemplate: models.CPUTemplateC3,
				VcpuCount:   vcpuCount,
				MemSizeMib:  memSize,
				HtEnabled:   true,
			},
		},
	}

	for _, tc := range testcases {
		tc := tc // see https://github.com/kyoh86/scopelint/issues/4
		t.Run(tc.name, func(t *testing.T) {
			machineConfig := machineConfigurationFromProto(tc.config, tc.proto)
			assert.Equal(t, tc.expectedMachineConfig, machineConfig)
		})
	}
}

func TestNetworkConfigFromProto(t *testing.T) {
	network := networkConfigFromProto(&proto.FirecrackerNetworkInterface{
		MacAddress:  mac,
		HostDevName: hostDevName,
		AllowMMDS:   true,
	})

	assert.Equal(t, mac, network.MacAddress)
	assert.Equal(t, hostDevName, network.HostDevName)
	assert.True(t, network.AllowMMDS)
	assert.Nil(t, network.InRateLimiter)
	assert.Nil(t, network.OutRateLimiter)
}

func TestTokenBucketFromProto(t *testing.T) {
	bucket := tokenBucketFromProto(&proto.FirecrackerTokenBucket{
		OneTimeBurst: oneTimeBurst,
		RefillTime:   refillTime,
		Capacity:     capacity,
	})

	assert.EqualValues(t, oneTimeBurst, *bucket.OneTimeBurst)
	assert.EqualValues(t, refillTime, *bucket.RefillTime)
	assert.EqualValues(t, capacity, *bucket.Size)
}

func TestAddDriveFromProto(t *testing.T) {
	list := addDriveFromProto(firecracker.DrivesBuilder{}, &proto.FirecrackerDrive{
		IsReadOnly: true,
		PathOnHost: "/a",
		Partuuid:   "xy",
	}).Build()

	assert.Equal(t, "/a", *list[0].PathOnHost)
	assert.Equal(t, "xy", list[0].Partuuid)
	assert.True(t, *list[0].IsReadOnly)
}
