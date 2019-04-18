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
	"time"

	"github.com/firecracker-microvm/firecracker-go-sdk"
	models "github.com/firecracker-microvm/firecracker-go-sdk/client/models"

	"github.com/firecracker-microvm/firecracker-containerd/proto"
)

func machineConfigurationFromProto(req *proto.FirecrackerMachineConfiguration) models.MachineConfiguration {
	config := models.MachineConfiguration{
		CPUTemplate: defaultCPUTemplate,
		VcpuCount:   defaultCPUCount,
		MemSizeMib:  defaultMemSizeMb,
	}

	if name := req.GetCPUTemplate(); name != "" {
		config.CPUTemplate = models.CPUTemplate(name)
	}

	if count := req.GetVcpuCount(); count > 0 {
		config.VcpuCount = int64(count)
	}

	if size := req.GetMemSizeMib(); size > 0 {
		config.MemSizeMib = int64(size)
	}

	config.HtEnabled = req.GetHtEnabled()

	return config
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

func addDriveFromProto(builder firecracker.DrivesBuilder, drive *proto.FirecrackerDrive) firecracker.DrivesBuilder {
	opt := func(d *models.Drive) {
		d.IsRootDevice = firecracker.Bool(drive.GetIsRootDevice())
		d.Partuuid = drive.GetPartuuid()

		if limiter := drive.GetRateLimiter(); limiter != nil {
			d.RateLimiter = rateLimiterFromProto(limiter)
		}
	}

	return builder.AddDrive(drive.GetPathOnHost(), drive.GetIsReadOnly(), opt)
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
