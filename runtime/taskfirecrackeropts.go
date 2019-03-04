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
	"time"

	"github.com/firecracker-microvm/firecracker-containerd/proto"
	firecracker "github.com/firecracker-microvm/firecracker-go-sdk"
	models "github.com/firecracker-microvm/firecracker-go-sdk/client/models"
	"github.com/pkg/errors"
)

// overrideVMConfigFromTaskOpts overrides the firecracker VM config for the
// task with values set in the protobuf message.
func overrideVMConfigFromTaskOpts(
	cfg firecracker.Config,
	vmConfig *proto.FirecrackerConfig,
	drivesBuilder firecracker.DrivesBuilder,
) (firecracker.Config, firecracker.DrivesBuilder, error) {
	if vmConfig == nil {
		return cfg, drivesBuilder, nil
	}
	var err error
	if vmConfig.KernelImagePath != "" {
		// If kernel image path is set, override it.
		cfg.KernelImagePath = vmConfig.KernelImagePath
	}
	if vmConfig.KernelArgs != "" {
		// If kernel args are set, override it.
		cfg.KernelArgs = vmConfig.KernelArgs
	}
	// Attach network interface specified in the config.
	if len(vmConfig.NetworkInterfaces) > 0 {
		nwIfaces := make([]firecracker.NetworkInterface, len(vmConfig.NetworkInterfaces))
		for i, nw := range vmConfig.NetworkInterfaces {
			nwIfaces[i] = networkConfigFromProto(nw)
		}
		cfg.NetworkInterfaces = nwIfaces
	}
	if vmConfig.MachineCfg != nil {
		cfg.MachineCfg, err = machineConfigFromProto(vmConfig.MachineCfg)
		if err != nil {
			return firecracker.Config{}, drivesBuilder, err
		}
	}
	// Overwrite the root drive config if specified.
	if vmConfig.RootDrive != nil {
		drivesBuilder = drivesBuilderFromProto(vmConfig.RootDrive, drivesBuilder, true)
	}
	// Add additional drives if specified.
	if len(vmConfig.AdditionalDrives) > 0 {
		for _, drive := range vmConfig.AdditionalDrives {
			drivesBuilder = drivesBuilderFromProto(drive, drivesBuilder, false)
		}
	}

	return cfg, drivesBuilder, nil
}

// netNSFromProto returns the network namespace set, if any in the protobuf
// message.
func netNSFromProto(vmConfig *proto.FirecrackerConfig) string {
	if vmConfig != nil {
		return vmConfig.FirecrackerNetworkNamespace
	}

	return ""
}

// networkConfigFromProto creates a firecracker NetworkInterface object from
// the protobuf FirecrackerNetworkInterface message.
func networkConfigFromProto(nwIface *proto.FirecrackerNetworkInterface) firecracker.NetworkInterface {
	result := firecracker.NetworkInterface{
		MacAddress:  nwIface.MacAddress,
		HostDevName: nwIface.HostDevName,
		AllowMMDS:   nwIface.AllowMMDS,
	}
	if nwIface.InRateLimiter != nil {
		result.InRateLimiter = rateLimiterFromProto(nwIface.InRateLimiter)
	}
	if nwIface.OutRateLimiter != nil {
		result.OutRateLimiter = rateLimiterFromProto(nwIface.OutRateLimiter)
	}

	return result
}

// machineConfigFromProto creates a firecracker MachineConfiguration object
// from the protobuf FirecrackerMachineConfiguration message.
func machineConfigFromProto(mCfg *proto.FirecrackerMachineConfiguration) (models.MachineConfiguration, error) {
	result := models.MachineConfiguration{}
	if mCfg.MemSizeMib > 0 && mCfg.VcpuCount == 0 {
		// Only memory is being overridden.
		return result, errors.New("both vm memory and vcpu count need to be overridden")
	}
	if mCfg.VcpuCount > 0 && mCfg.MemSizeMib == 0 {
		// Only cpu is being overridden.
		return result, errors.New("both vm memory and vcpu count need to be overridden")
	}
	if mCfg.CPUTemplate != "" {
		result.CPUTemplate = models.CPUTemplate(mCfg.CPUTemplate)
	}
	result.HtEnabled = mCfg.HtEnabled
	result.MemSizeMib = int64(mCfg.MemSizeMib)
	result.VcpuCount = int64(mCfg.VcpuCount)

	return result, nil
}

// drivesBuilderFromProto returns a new DrivesBuilder that can be used to
// build the firecracker Drives config. The new DrivesBuilder object is
// constructed using the information present in the protobuf message.
func drivesBuilderFromProto(drive *proto.FirecrackerDrive,
	drivesBuilder firecracker.DrivesBuilder,
	isRoot bool,
) firecracker.DrivesBuilder {
	opt := func(d *models.Drive) {
		d.Partuuid = drive.Partuuid
		if drive.RateLimiter != nil {
			d.RateLimiter = rateLimiterFromProto(drive.RateLimiter)
		}
	}
	if isRoot {
		return drivesBuilder.WithRootDrive(drive.PathOnHost, opt)
	}
	return drivesBuilder.AddDrive(drive.PathOnHost, drive.IsReadOnly, opt)
}

// rateLimiterFromProto creates a firecracker RateLimiter object from the
// protobuf message.
func rateLimiterFromProto(rl *proto.FirecrackerRateLimiter) *models.RateLimiter {
	result := models.RateLimiter{}
	if rl.Bandwidth != nil {
		result.Bandwidth = tokenBucketFromProto(rl.Bandwidth)
	}
	if rl.Ops != nil {
		result.Ops = tokenBucketFromProto(rl.Ops)
	}

	return &result
}

// tokenBucketFromProto creates a firecracker TokenBucket object from the
// protobuf message.
func tokenBucketFromProto(bucket *proto.FirecrackerTokenBucket) *models.TokenBucket {
	builder := firecracker.TokenBucketBuilder{}
	if bucket.OneTimeBurst > 0 {
		builder = builder.WithInitialSize(bucket.OneTimeBurst)
	}
	if bucket.RefillTime > 0 {
		builder = builder.WithRefillDuration(time.Duration(bucket.RefillTime) * time.Millisecond)
	}
	if bucket.Capacity > 0 {
		builder = builder.WithBucketSize(bucket.Capacity)
	}

	res := builder.Build()
	return &res
}
