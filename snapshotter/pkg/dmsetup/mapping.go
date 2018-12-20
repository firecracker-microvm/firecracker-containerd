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

package dmsetup

import (
	"fmt"
	"os/exec"
	"strconv"
	"strings"

	"github.com/pkg/errors"
)

const SectorSize = 512

// BlockDeviceSize returns size of block device in bytes
func BlockDeviceSize(devicePath string) (uint64, error) {
	data, err := exec.Command("blockdev", "--getsize64", "-q", devicePath).CombinedOutput()
	output := string(data)
	if err != nil {
		return 0, errors.Wrapf(err, output)
	}

	output = strings.TrimSuffix(output, "\n")
	return strconv.ParseUint(output, 10, 64)
}

// makeThinPoolMapping makes thin-pool table entry
func makeThinPoolMapping(dataFile, metaFile string, blockSizeSectors uint32) (string, error) {
	dataDeviceSizeBytes, err := BlockDeviceSize(dataFile)
	if err != nil {
		return "", errors.Wrapf(err, "failed to get block device size: %s", dataFile)
	}

	// Thin-pool mapping target has the following format:
	// start - starting block in virtual device
	// length - length of this segment
	// metadata_dev - the metadata device
	// data_dev - the data device
	// data_block_size - the data block size in sectors
	// low_water_mark - the low water mark, expressed in blocks of size data_block_size
	// feature_args - the number of feature arguments
	// args
	lengthSectors := dataDeviceSizeBytes / SectorSize
	target := fmt.Sprintf("0 %d thin-pool %s %s %d 32768 1 skip_block_zeroing", lengthSectors, metaFile, dataFile, blockSizeSectors)

	return target, nil
}

// makeThinMapping makes thin target table entry
func makeThinMapping(poolName string, deviceID int, sizeBytes uint64, externalOriginDevice string) string {
	lengthSectors := sizeBytes / SectorSize

	// Thin target has the following format:
	// start - starting block in virtual device
	// length - length of this segment
	// pool_dev - the thin-pool device, can be /dev/mapper/pool_name or 253:0
	// dev_id - the internal device id of the device to be activated
	// external_origin_dev - an optional block device outside the pool to be treated as a read-only snapshot origin.
	target := fmt.Sprintf("0 %d thin %s %d %s", lengthSectors, GetFullDevicePath(poolName), deviceID, externalOriginDevice)
	return strings.TrimSpace(target)
}
