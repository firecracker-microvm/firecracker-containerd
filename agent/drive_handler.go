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
	"io/ioutil"
	"os"
	"path/filepath"
	"strings"

	"github.com/firecracker-microvm/firecracker-containerd/internal"
)

const (
	blockPath       = "/sys/block"
	drivePath       = "/dev"
	blockMajorMinor = "dev"
)

type drive struct {
	Name       string
	DriveID    string
	MajorMinor string
	DrivePath  string
}

type driveHandler struct {
	// drives is a mapping to all the stub drives
	drives map[string]drive
	// BlockPath is the path which lists all block devices. In Linux this is
	// typically /sys/block
	BlockPath string
	// DrivePath is the directory where the list of drives will be found. In
	// Linux this is typically /dev
	DrivePath string
}

func newDriveHandler(blockPath, drivePath string) (*driveHandler, error) {
	d := &driveHandler{
		drives:    map[string]drive{},
		BlockPath: blockPath,
		DrivePath: drivePath,
	}

	err := d.discoverDrives()
	if err != nil {
		return nil, err
	}

	return d, nil
}

func (dh driveHandler) GetDrive(id string) (drive, bool) {
	v, ok := dh.drives[id]
	return v, ok
}

// discoverDrives will iterate the block path in the sys directory to retrieve all
// stub block devices.
func (dh *driveHandler) discoverDrives() error {
	names, err := getListOfBlockDeviceNames(dh.BlockPath)
	if err != nil {
		return err
	}

	drives := map[string]drive{}
	for _, name := range names {
		d, err := dh.buildDrive(name)
		if err != nil {
			return err
		}

		if !isStubDrive(d) {
			continue
		}

		f, err := os.Open(d.Path())
		if err != nil {
			return err
		}

		d.DriveID, err = internal.ParseStubContent(f)
		f.Close()
		if err != nil {
			return err
		}
		drives[d.DriveID] = d
	}

	dh.drives = drives
	return nil
}

func (d drive) Path() string {
	return filepath.Join(d.DrivePath, d.Name)
}

func getListOfBlockDeviceNames(path string) ([]string, error) {
	names := []string{}
	infos, err := ioutil.ReadDir(path)
	if err != nil {
		return nil, err
	}

	for _, info := range infos {
		names = append(names, info.Name())
	}

	return names, nil
}

// buildDrive uses the /sys/block folder to check a given name's block major
// and minor, and block size.
func (dh driveHandler) buildDrive(name string) (drive, error) {
	d := drive{
		Name:      name,
		DrivePath: dh.DrivePath,
	}

	majorMinorStr, err := ioutil.ReadFile(filepath.Join(dh.BlockPath, name, blockMajorMinor))
	if err != nil {
		return d, err
	}
	d.MajorMinor = strings.TrimSpace(string(majorMinorStr))

	return d, nil
}

// isStubDrive will check to see if a given drive is a stub drive.
func isStubDrive(d drive) bool {
	f, err := os.Open(d.Path())
	if err != nil {
		return false
	}
	defer f.Close()

	return internal.IsStubDrive(f)
}
