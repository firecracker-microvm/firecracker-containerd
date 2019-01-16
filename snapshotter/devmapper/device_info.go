// Copyright 2018 Amazon.com, Inc. or its affiliates. All Rights Reserved.
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

package devmapper

const (
	maxDeviceID = 0xffffff // Device IDs are 24-bit numbers
)

// DeviceInfo represents metadata for thin device within thin-pool
type DeviceInfo struct {
	// DeviceID is a 24-bit number assigned to a device within thin-pool device
	DeviceID uint32 `json:"device_id"`
	// Size is a thin device size
	Size uint64 `json:"size"`
	// Name is a device name to be used in /dev/mapper/
	Name string `json:"name"`
	// ParentName is a name of parent device (if snapshot)
	ParentName string `json:"parent_name"`
	// IsActivated indicates whether thin device was actived
	IsActivated bool `json:"is_active"`
}
