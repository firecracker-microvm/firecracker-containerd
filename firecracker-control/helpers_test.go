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

package service

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
	mac          = "AA:FC:00:00:00:01"
	hostDevName  = "tap0"
)

func TestMachineConfigurationFromProto(t *testing.T) {
	config := machineConfigurationFromProto(&proto.FirecrackerMachineConfiguration{
		CPUTemplate: string(models.CPUTemplateC3),
		VcpuCount:   vcpuCount,
		MemSizeMib:  memSize,
		HtEnabled:   true,
	})

	assert.EqualValues(t, models.CPUTemplateC3, config.CPUTemplate)
	assert.EqualValues(t, vcpuCount, config.VcpuCount)
	assert.EqualValues(t, memSize, config.MemSizeMib)
	assert.True(t, config.HtEnabled)
}

func TestDefaultMachineConfigurationFromProto(t *testing.T) {
	configs := map[string]models.MachineConfiguration{
		"Nil":          machineConfigurationFromProto(nil),
		"Empty struct": machineConfigurationFromProto(&proto.FirecrackerMachineConfiguration{}),
	}

	for name, config := range configs {
		cfg := config
		t.Run(name, func(t *testing.T) {
			assert.EqualValues(t, defaultCPUTemplate, cfg.CPUTemplate)
			assert.EqualValues(t, defaultCPUCount, cfg.VcpuCount)
			assert.EqualValues(t, defaultMemSizeMb, cfg.MemSizeMib)
			assert.False(t, cfg.HtEnabled)
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
