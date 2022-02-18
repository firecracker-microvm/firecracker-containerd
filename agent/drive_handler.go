// Copyright Amazon.com, Inc. or its affiliates. All Rights Reserved.
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
	"context"
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/containerd/containerd/log"
	"github.com/containerd/containerd/mount"
	"github.com/firecracker-microvm/firecracker-containerd/internal"
	drivemount "github.com/firecracker-microvm/firecracker-containerd/proto/service/drivemount/ttrpc"
	"github.com/gogo/protobuf/types"
	"github.com/pkg/errors"
)

const (
	blockPath       = "/sys/block"
	drivePath       = "/dev"
	blockMajorMinor = "dev"
)

var (
	bannedSystemDirs = []string{
		"/proc",
		"/sys",
		"/dev",
	}
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
	// BlockPath contains the location of the block subdirectory under the sysfs
	// mount point.
	BlockPath string
	// DrivePath should contain the location of the drive block device nodes.
	DrivePath string
}

var _ drivemount.DriveMounterService = &driveHandler{}

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

func (dh driveHandler) MountDrive(ctx context.Context, req *drivemount.MountDriveRequest) (*types.Empty, error) {
	logger := log.G(ctx)
	logger.Debugf("%+v", req.String())
	logger = logger.WithField("drive_id", req.DriveID)

	drive, ok := dh.GetDrive(req.DriveID)
	if !ok {
		return nil, fmt.Errorf("drive %q could not be found", req.DriveID)
	}
	logger = logger.WithField("drive_path", drive.Path())

	// Do a basic check that we won't be mounting over any important system directories
	if err := isSystemDir(req.DestinationPath); err != nil {
		return nil, err
	}

	err := os.MkdirAll(req.DestinationPath, 0700)
	if err != nil {
		return nil, errors.Wrapf(err, "failed to create drive mount destination %q", req.DestinationPath)
	}

	// Retry the mount in the case of failure a fixed number of times. This works around a rare issue
	// where we get to this mount attempt before the guest OS has realized a drive was patched:
	// https://github.com/firecracker-microvm/firecracker-containerd/issues/214
	const (
		maxRetries = 100
		retryDelay = 10 * time.Millisecond
	)

	for i := 0; i < maxRetries; i++ {
		err := mount.All([]mount.Mount{{
			Source:  drive.Path(),
			Type:    req.FilesytemType,
			Options: req.Options,
		}}, req.DestinationPath)
		if err == nil {
			return &types.Empty{}, nil
		}

		if isRetryableMountError(err) {
			logger.WithError(err).Warnf("retryable failure mounting drive")
			time.Sleep(retryDelay)
			continue
		}

		return nil, errors.Wrapf(err, "non-retryable failure mounting drive from %q to %q",
			drive.Path(), req.DestinationPath)
	}

	return nil, errors.Errorf("exhausted retries mounting drive from %q to %q",
		drive.Path(), req.DestinationPath)
}

func (dh driveHandler) UnmountDrive(ctx context.Context, req *drivemount.UnmountDriveRequest) (*types.Empty, error) {
	drive, ok := dh.GetDrive(req.DriveID)
	if !ok {
		return nil, fmt.Errorf("drive %q could not be found", req.DriveID)
	}

	err := mount.Unmount(drive.Path(), 0)
	if err == nil {
		return &types.Empty{}, nil
	}

	return nil, errors.Errorf("failed to unmount the drive %q",
		drive.Path())
}

func isSystemDir(path string) error {
	resolvedDest, err := evalAnySymlinks(path)
	if err != nil {
		return errors.Wrapf(err,
			"failed to evaluate any symlinks in drive ummount destination %q", path)
	}

	for _, systemDir := range bannedSystemDirs {
		if isOrUnderDir(resolvedDest, systemDir) {
			return errors.Errorf(
				"drive mount destination %q resolves to path %q under banned system directory %q",
				path, resolvedDest, systemDir,
			)
		}
	}

	return nil
}

// evalAnySymlinks is similar to filepath.EvalSymlinks, except it will not return an error if part of the
// provided path does not exist. It will evaluate symlinks present in the path up to a component that doesn't
// exist, at which point it will just append the rest of the provided path to what has been resolved so far.
// We validate earlier that input to this function is an absolute path.
func evalAnySymlinks(path string) (string, error) {
	curPath := "/"
	pathSplit := strings.Split(filepath.Clean(path), "/")
	for len(pathSplit) > 0 {
		curPath = filepath.Join(curPath, pathSplit[0])
		pathSplit = pathSplit[1:]

		resolvedPath, err := filepath.EvalSymlinks(curPath)
		if os.IsNotExist(err) {
			return filepath.Join(append([]string{curPath}, pathSplit...)...), nil
		}
		if err != nil {
			return "", err
		}
		curPath = resolvedPath
	}

	return curPath, nil
}

// returns whether the given path is the provided baseDir or is under it
func isOrUnderDir(path, baseDir string) bool {
	path = filepath.Clean(path)
	baseDir = filepath.Clean(baseDir)

	if baseDir == "/" {
		return true
	}

	if path == baseDir {
		return true
	}

	return strings.HasPrefix(path, baseDir+"/")
}
