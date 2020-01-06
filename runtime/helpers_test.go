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
	"github.com/stretchr/testify/require"

	"github.com/firecracker-microvm/firecracker-containerd/config"
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
		config                *config.Config
		proto                 *proto.FirecrackerMachineConfiguration
		expectedMachineConfig models.MachineConfiguration
	}{
		{
			name:   "ProtoOnly",
			config: &config.Config{},
			proto: &proto.FirecrackerMachineConfiguration{
				CPUTemplate: string(models.CPUTemplateC3),
				VcpuCount:   vcpuCount,
				MemSizeMib:  memSize,
				HtEnabled:   true,
			},
			expectedMachineConfig: models.MachineConfiguration{
				CPUTemplate: models.CPUTemplateC3,
				VcpuCount:   firecracker.Int64(vcpuCount),
				MemSizeMib:  firecracker.Int64(memSize),
				HtEnabled:   firecracker.Bool(true),
			},
		},
		{
			name: "ConfigOnly",
			config: &config.Config{
				CPUTemplate: "C3",
			},
			proto: &proto.FirecrackerMachineConfiguration{},
			expectedMachineConfig: models.MachineConfiguration{
				CPUTemplate: models.CPUTemplateC3,
				VcpuCount:   firecracker.Int64(defaultCPUCount),
				MemSizeMib:  firecracker.Int64(defaultMemSizeMb),
				HtEnabled:   firecracker.Bool(false),
			},
		},
		{
			name: "NilProto",
			config: &config.Config{
				CPUTemplate: "C3",
			},
			expectedMachineConfig: models.MachineConfiguration{
				CPUTemplate: models.CPUTemplateC3,
				VcpuCount:   firecracker.Int64(defaultCPUCount),
				MemSizeMib:  firecracker.Int64(defaultMemSizeMb),
				HtEnabled:   firecracker.Bool(false),
			},
		},
		{
			name: "Overrides",
			config: &config.Config{
				CPUTemplate: "T2",
			},
			proto: &proto.FirecrackerMachineConfiguration{
				CPUTemplate: string(models.CPUTemplateC3),
				VcpuCount:   vcpuCount,
				MemSizeMib:  memSize,
				HtEnabled:   true,
			},
			expectedMachineConfig: models.MachineConfiguration{
				CPUTemplate: models.CPUTemplateC3,
				VcpuCount:   firecracker.Int64(vcpuCount),
				MemSizeMib:  firecracker.Int64(memSize),
				HtEnabled:   firecracker.Bool(true),
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

func TestNetworkConfigFromProto_Static(t *testing.T) {
	primaryAddr := "198.51.100.2/24"
	gatewayAddr := "198.51.100.1"
	nameservers := []string{"192.0.2.1", "192.0.2.2", "192.0.2.3"}

	network, err := networkConfigFromProto(&proto.FirecrackerNetworkInterface{
		AllowMMDS: true,
		StaticConfig: &proto.StaticNetworkConfiguration{
			MacAddress:  mac,
			HostDevName: hostDevName,
			IPConfig: &proto.IPConfiguration{
				PrimaryAddr: primaryAddr,
				GatewayAddr: gatewayAddr,
				Nameservers: nameservers,
			},
		},
	}, "vmID")
	require.NoError(t, err, "failed to parse static network config from proto")

	assert.Equal(t, mac, network.StaticConfiguration.MacAddress)
	assert.Equal(t, hostDevName, network.StaticConfiguration.HostDevName)
	assert.Equal(t, primaryAddr, network.StaticConfiguration.IPConfiguration.IPAddr.String())
	assert.Equal(t, gatewayAddr, network.StaticConfiguration.IPConfiguration.Gateway.String())
	assert.Equal(t, nameservers, network.StaticConfiguration.IPConfiguration.Nameservers)

	assert.True(t, network.AllowMMDS)
	assert.Nil(t, network.InRateLimiter)
	assert.Nil(t, network.OutRateLimiter)
}

func TestNetworkConfigFromProto_CNI(t *testing.T) {
	networkName := "da-network"
	ifName := "da-iface"
	vmID := "da-vm"
	cniConfDir := "/da/cni/config"
	cniCacheDir := "/da/cni/cache"
	cniBinPath := []string{"/foo", "/boo/far"}
	cniKey1 := "foo"
	cniVal1 := "bar"
	cniKey2 := "boo"
	cniVal2 := "far"

	inputArgs := []*proto.CNIConfiguration_CNIArg{
		{
			Key:   cniKey1,
			Value: cniVal1,
		},
		{
			Key:   cniKey2,
			Value: cniVal2,
		},
	}

	network, err := networkConfigFromProto(&proto.FirecrackerNetworkInterface{
		AllowMMDS: true,
		CNIConfig: &proto.CNIConfiguration{
			NetworkName:   networkName,
			InterfaceName: ifName,
			ConfDir:       cniConfDir,
			CacheDir:      cniCacheDir,
			BinPath:       cniBinPath,
			Args:          inputArgs,
		},
	}, vmID)
	require.NoError(t, err, "failed to parse CNI network config from proto")

	assert.True(t, network.AllowMMDS)
	assert.Nil(t, network.InRateLimiter)
	assert.Nil(t, network.OutRateLimiter)

	assert.Equal(t, network.CNIConfiguration.NetworkName, networkName)
	assert.Equal(t, network.CNIConfiguration.IfName, ifName)
	assert.Equal(t, network.CNIConfiguration.ConfDir, cniConfDir)
	assert.Equal(t, network.CNIConfiguration.CacheDir, cniCacheDir)
	assert.Equal(t, network.CNIConfiguration.BinPath, cniBinPath)

	require.Len(t, network.CNIConfiguration.Args, 2, "unexpected number of CNI args")
	for i, inputArg := range inputArgs {
		outputArg := network.CNIConfiguration.Args[i]
		assert.Equal(t, inputArg.Key, outputArg[0])
		assert.Equal(t, inputArg.Value, outputArg[1])
	}
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
