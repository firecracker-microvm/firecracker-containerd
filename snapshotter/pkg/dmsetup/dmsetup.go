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
	"regexp"
	"strings"
	"sync"

	"github.com/pkg/errors"
	"golang.org/x/sys/unix"
)

const DevMapperDir = "/dev/mapper/"

// DeviceInfo represents device info returned by "dmsetup info"
type DeviceInfo struct {
	Name            string
	BlockDeviceName string
	TableLive       bool
	TableInactive   bool
	Suspended       bool
	ReadOnly        bool
	Major           uint32
	Minor           uint32
	OpenCount       uint32 // Open reference count
	TargetCount     uint32 // Number of targets in the live table
	EventNumber     uint32 // Last event sequence number (used by wait)
}

var (
	errTable     map[string]unix.Errno
	errTableInit sync.Once
)

// CreatePool creates a device with the given name, data and metadata file and block size (see "dmsetup create")
func CreatePool(poolName, dataFile, metaFile string, blockSizeBytes uint32) error {
	thinPool, err := makeThinPoolMapping(dataFile, metaFile, blockSizeBytes)
	if err != nil {
		return err
	}

	_, err = dmsetup("create", poolName, "--table", thinPool)
	return err
}

// ReloadPool reloads existing thin-pool (see "dmsetup reload")
func ReloadPool(deviceName, dataFile, metaFile string, blockSizeBytes uint32) error {
	thinPool, err := makeThinPoolMapping(dataFile, metaFile, blockSizeBytes)
	if err != nil {
		return err
	}

	_, err = dmsetup("reload", deviceName, "--table", thinPool)
	return err
}

// CreateDevice sends "create_thin <deviceID>" message to the given thin-pool
func CreateDevice(poolName string, deviceID int) error {
	_, err := dmsetup("message", poolName, "0", fmt.Sprintf("create_thin %d", deviceID))
	return err
}

// ActivateDevice activates the given thin-device using the 'thin' target
func ActivateDevice(poolName string, deviceName string, deviceID int, size uint64, external string) error {
	mapping := makeThinMapping(poolName, deviceID, size, external)
	_, err := dmsetup("create", deviceName, "--table", mapping)
	return err
}

// SuspendDevice suspends the given device (see "dmsetup suspend")
func SuspendDevice(deviceName string) error {
	_, err := dmsetup("suspend", deviceName)
	return err
}

// ResumeDevice resumes the given device (see "dmsetup resume")
func ResumeDevice(deviceName string) error {
	_, err := dmsetup("resume", deviceName)
	return err
}

// Table returns the current table for the device
func Table(deviceName string) (string, error) {
	return dmsetup("table", deviceName)
}

// CreateSnapshot sends "create_snap" message to the given thin-pool.
// Caller needs to suspend and resume device if it is active.
func CreateSnapshot(poolName string, deviceID int, baseDeviceID int) error {
	_, err := dmsetup("message", poolName, "0", fmt.Sprintf("create_snap %d %d", deviceID, baseDeviceID))
	return err
}

// DeleteDevice sends "delete <deviceID>" message to the given thin-pool
func DeleteDevice(poolName string, deviceID int) error {
	_, err := dmsetup("message", poolName, "0", fmt.Sprintf("delete %d", deviceID))
	return err
}

// RemoveDevice removes a device (see "dmsetup remove")
func RemoveDevice(deviceName string, force, retry, deferred bool) error {
	args := []string{
		"remove",
	}

	if force {
		args = append(args, "--force")
	}

	if retry {
		args = append(args, "--retry")
	}

	if deferred {
		args = append(args, "--deferred")
	}

	args = append(args, getFullDevicePath(deviceName))

	_, err := dmsetup(args...)
	return err
}

// Info outputs device information (see "dmsetup info").
// If device name is empty, all device infos will be returned.
func Info(deviceName string) ([]*DeviceInfo, error) {
	output, err := dmsetup(
		"info",
		"--columns",
		"--noheadings",
		"-o",
		"name,blkdevname,attr,major,minor,open,segments,events",
		"--separator",
		" ",
		deviceName)

	if err != nil {
		return nil, err
	}

	var (
		devices []*DeviceInfo
		lines   = strings.Split(output, "\n")
	)

	for _, line := range lines {
		var (
			attr = ""
			info = &DeviceInfo{}
		)

		_, err := fmt.Sscan(line,
			&info.Name,
			&info.BlockDeviceName,
			&attr,
			&info.Major,
			&info.Minor,
			&info.OpenCount,
			&info.TargetCount,
			&info.EventNumber)

		if err != nil {
			return nil, errors.Wrapf(err, "failed to parse line '%s'", line)
		}

		// Parse attributes (see "man dmsetup") for details
		info.Suspended = strings.Contains(attr, "s")
		info.ReadOnly = strings.Contains(attr, "r")
		info.TableLive = strings.Contains(attr, "L")
		info.TableInactive = strings.Contains(attr, "I")

		devices = append(devices, info)
	}

	return devices, nil
}

func Version() (string, error) {
	return dmsetup("version")
}

func getFullDevicePath(deviceName string) string {
	if strings.HasPrefix(deviceName, DevMapperDir) {
		return deviceName
	}

	return DevMapperDir + deviceName
}

func dmsetup(args ...string) (string, error) {
	data, err := exec.Command("dmsetup", args...).CombinedOutput()
	output := string(data)
	if err != nil {
		// It's useful to have Linux error codes like EBUSY, EPERM, ..., instead of just text.
		// Try matching with Linux error code, otherwise return generic error.
		// Unfortunately there is no better way than extracting/comparing error text.
		if text := extractErrorText(output); text != "" {
			errTableInit.Do(func() {
				// Precompute map of <text>=<errno> for optimal lookup
				errTable = make(map[string]unix.Errno)
				for errno := unix.EPERM; errno <= unix.EHWPOISON; errno++ {
					errTable[errno.Error()] = errno
				}
			})

			if errno, ok := errTable[text]; ok {
				return "", errno
			}
		}

		// Return generic error if can't get Linux error
		return "", errors.Wrapf(err, "dmsetup %s\nerror: %s\n", strings.Join(args, " "), output)
	}

	output = strings.TrimSuffix(output, "\n")
	output = strings.TrimSpace(output)

	return output, nil
}

func extractErrorText(output string) string {
	// dmsetup returns error messages in format:
	// 	device-mapper: message ioctl on <name> failed: File exists\n
	// 	Command failed\n
	// Extract text between "failed: " and "\n"
	re := regexp.MustCompilePOSIX("failed: [^:]+$")
	str := re.FindString(output)

	// Strip "failed: " prefix
	str = strings.TrimLeft(str, "failed: ")

	str = strings.ToLower(str)
	return str
}
