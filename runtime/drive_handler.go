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
	"encoding/base32"
	"fmt"
	"os"
	"path/filepath"
	"sync"

	"github.com/pkg/errors"
	"github.com/sirupsen/logrus"

	firecracker "github.com/firecracker-microvm/firecracker-go-sdk"
	"github.com/firecracker-microvm/firecracker-go-sdk/client/models"

	"github.com/firecracker-microvm/firecracker-containerd/internal"
	"github.com/firecracker-microvm/firecracker-containerd/proto"
	drivemount "github.com/firecracker-microvm/firecracker-containerd/proto/service/drivemount/ttrpc"
)

const (
	// fcSectorSize is the sector size of Firecracker drives
	fcSectorSize = 512
)

var (
	// ErrDrivesExhausted occurs when there are no more drives left to use. This
	// can happen by calling PatchStubDrive greater than the number of drives.
	ErrDrivesExhausted = fmt.Errorf("There are no remaining drives to be used")
)

// CreateContainerStubs will create a StubDriveHandler for managing the stub drives
// of container rootfs drives. The Firecracker drives are hardcoded to be read-write
// and have no rate limiter configuration.
func CreateContainerStubs(
	machineCfg *firecracker.Config,
	jail jailer,
	containerCount int,
	logger *logrus.Entry,
) (*StubDriveHandler, error) {
	var containerStubs []*stubDrive
	for i := 0; i < containerCount; i++ {
		isWritable := true
		var rateLimiter *proto.FirecrackerRateLimiter
		stubFileName := fmt.Sprintf("ctrstub%d", i)

		stubDrive, err := newStubDrive(
			filepath.Join(jail.JailPath().RootPath(), stubFileName),
			jail, isWritable, rateLimiter, logger)

		if err != nil {
			return nil, errors.Wrap(err, "failed to create container stub drive")
		}

		machineCfg.Drives = append(machineCfg.Drives, models.Drive{
			DriveID:      firecracker.String(stubDrive.driveID),
			PathOnHost:   firecracker.String(stubDrive.stubPath),
			IsReadOnly:   firecracker.Bool(!isWritable),
			RateLimiter:  rateLimiterFromProto(rateLimiter),
			IsRootDevice: firecracker.Bool(false),
		})
		containerStubs = append(containerStubs, stubDrive)
	}

	return &StubDriveHandler{
		freeDrives: containerStubs,
		usedDrives: make(map[string]*stubDrive),
	}, nil
}

// StubDriveHandler manages a set of stub drives. It currently only supports reserving
// one of the drives from its set.
// In the future, it may be expanded to also support recycling a drive to be used again
// for a different mount.
type StubDriveHandler struct {
	freeDrives []*stubDrive
	// map of id -> stub drive being used by that task
	usedDrives map[string]*stubDrive
	mu         sync.Mutex
}

// Reserve pops a unused stub drive and returns a MountableStubDrive that can be
// mounted with the provided options as the patched drive information.
func (h *StubDriveHandler) Reserve(
	requestCtx context.Context,
	id string,
	hostPath string,
	vmPath string,
	filesystemType string,
	options []string,
	driveMounter drivemount.DriveMounterService,
	machine firecracker.MachineIface,
) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	if len(h.freeDrives) == 0 {
		return ErrDrivesExhausted
	}
	if _, ok := h.usedDrives[id]; ok {
		// This case means that drive wasn't released or removed properly
		return fmt.Errorf("drive with ID %s already in use, a previous attempt to remove it may have failed", id)
	}

	freeDrive := h.freeDrives[0]
	options, err := setReadWriteOptions(options, freeDrive.driveMount.IsWritable)
	if err != nil {
		return err
	}

	stubDrive := freeDrive.withMountConfig(
		hostPath,
		vmPath,
		filesystemType,
		options,
	)
	freeDrive = &stubDrive

	err = stubDrive.PatchAndMount(requestCtx, machine, driveMounter)
	if err != nil {
		err = errors.Wrapf(err, "failed to mount drive inside vm")
		return err
	}

	h.freeDrives = h.freeDrives[1:]
	h.usedDrives[id] = freeDrive
	return nil
}

// Release unmounts stub drive of just deleted container
// and pushes just released drive to freeDrives
func (h *StubDriveHandler) Release(
	requestCtx context.Context,
	id string,
	driveMounter drivemount.DriveMounterService,
	machine firecracker.MachineIface,
) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	drive, ok := h.usedDrives[id]
	if !ok {
		return errors.Errorf("container %s drive wasn't found", id)
	}

	_, err := driveMounter.UnmountDrive(requestCtx, &drivemount.UnmountDriveRequest{
		DriveID: drive.driveID,
	})
	if err != nil {
		return errors.Wrap(err, "failed to unmount drive")
	}

	err = machine.UpdateGuestDrive(requestCtx, drive.driveID, filepath.Base(drive.stubPath))
	if err != nil {
		return errors.Wrap(err, "failed to patch drive")
	}

	delete(h.usedDrives, id)
	h.freeDrives = append(h.freeDrives, drive)
	return nil
}

// CreateDriveMountStubs creates a set of MountableStubDrives from the provided DriveMount configs.
// The RateLimiter and ReadOnly settings need to be provided up front here as they currently
// cannot be patched after the Firecracker VM starts.
func CreateDriveMountStubs(
	machineCfg *firecracker.Config,
	jail jailer,
	driveMounts []*proto.FirecrackerDriveMount,
	logger *logrus.Entry,
) ([]MountableStubDrive, error) {
	containerStubs := make([]MountableStubDrive, len(driveMounts))
	for i, driveMount := range driveMounts {
		isWritable := driveMount.IsWritable
		rateLimiter := driveMount.RateLimiter
		cacheType := driveMount.CacheType
		stubFileName := fmt.Sprintf("drivemntstub%d", i)
		options, err := setReadWriteOptions(driveMount.Options, isWritable)
		if err != nil {
			return nil, err
		}

		stubDrive, err := newStubDrive(
			filepath.Join(jail.JailPath().RootPath(), stubFileName),
			jail, isWritable, rateLimiter, logger)
		if err != nil {
			return nil, errors.Wrap(err, "failed to create drive mount stub drive")
		}

		stubDrive.setCacheType(cacheType)

		machineCfg.Drives = append(machineCfg.Drives, models.Drive{
			DriveID:      firecracker.String(stubDrive.driveID),
			PathOnHost:   firecracker.String(stubDrive.stubPath),
			IsReadOnly:   firecracker.Bool(!isWritable),
			RateLimiter:  rateLimiterFromProto(rateLimiter),
			IsRootDevice: firecracker.Bool(false),
			CacheType:    cacheTypeFromProto(cacheType),
		})
		containerStubs[i] = stubDrive.withMountConfig(
			driveMount.HostPath,
			driveMount.VMPath,
			driveMount.FilesystemType,
			options)
	}

	return containerStubs, nil
}

func setReadWriteOptions(options []string, isWritable bool) ([]string, error) {
	var expectedOpt string
	if isWritable {
		expectedOpt = "rw"
	} else {
		expectedOpt = "ro"
	}

	for _, opt := range options {
		if opt == "ro" || opt == "rw" {
			if opt != expectedOpt {
				return nil, errors.Errorf("mount option %s is incompatible with IsWritable=%t", opt, isWritable)
			}
			return options, nil
		}
	}

	// if here, the neither "ro" or "rw" was specified, so explicitly set the option for the user
	return append(options, expectedOpt), nil
}

// A MountableStubDrive represents a stub drive that is ready to be patched and mounted
// once PatchAndMount is called.
type MountableStubDrive interface {
	PatchAndMount(
		requestCtx context.Context,
		machine firecracker.MachineIface,
		driveMounter drivemount.DriveMounterService,
	) error
}

func stubPathToDriveID(stubPath string) string {
	// Firecracker resource ids "can only contain alphanumeric characters and underscores", so
	// do a base32 encoding to remove any invalid characters (base32 avoids invalid "-" chars
	// from base64)
	return base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString([]byte(
		filepath.Base(stubPath)))
}

func newStubDrive(
	stubPath string,
	jail jailer,
	isWritable bool,
	rateLimiter *proto.FirecrackerRateLimiter,
	logger *logrus.Entry,
) (*stubDrive, error) {
	// use the stubPath as the drive ID since it needs to be unique per-stubdrive anyways
	driveID := stubPathToDriveID(stubPath)

	f, err := os.OpenFile(stubPath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return nil, err
	}
	defer func() {
		if err := f.Close(); err != nil {
			logger.WithError(err).Errorf("unexpected error during %v close", f.Name())
		}
	}()

	stubContent, err := internal.GenerateStubContent(driveID)
	if err != nil {
		return nil, err
	}

	if _, err := f.WriteString(stubContent); err != nil {
		return nil, err
	}

	info, err := f.Stat()
	if err != nil {
		return nil, err
	}

	fileSize := info.Size()
	sectorCount := fileSize / fcSectorSize
	driveSize := fcSectorSize * sectorCount

	remainingBytes := fileSize % fcSectorSize
	if remainingBytes != 0 {
		// If there are any residual bytes, this means we've need to fill the
		// appropriate sector size to ensure that the data is visible to
		// Firecracker.
		driveSize += fcSectorSize
	}

	// Firecracker will not show any drives smaller than 512 bytes. In
	// addition, the drive is read in chunks of 512 bytes; if the drive size is
	// not a multiple of 512 bytes, then the remainder will not be visible to
	// Firecracker. So we adjust to the appropriate size based on the residual
	// bytes remaining.
	if err := os.Truncate(stubPath, driveSize); err != nil {
		return nil, err
	}

	for _, opt := range jail.StubDrivesOptions() {
		err := opt(f)
		if err != nil {
			return nil, err
		}
	}

	return &stubDrive{
		stubPath: stubPath,
		jail:     jail,
		driveID:  driveID,
		driveMount: &proto.FirecrackerDriveMount{
			IsWritable:  isWritable,
			RateLimiter: rateLimiter,
		},
	}, nil
}

type stubDrive struct {
	stubPath   string
	jail       jailer
	driveID    string
	driveMount *proto.FirecrackerDriveMount
}

func (sd stubDrive) withMountConfig(
	hostPath string,
	vmPath string,
	filesystemType string,
	options []string,
) stubDrive {
	sd.driveMount = &proto.FirecrackerDriveMount{
		HostPath:       hostPath,
		VMPath:         vmPath,
		FilesystemType: filesystemType,
		Options:        options,
		IsWritable:     sd.driveMount.IsWritable,
		RateLimiter:    sd.driveMount.RateLimiter,
		CacheType:      sd.driveMount.CacheType,
	}
	return sd
}

func (sd stubDrive) PatchAndMount(
	requestCtx context.Context,
	machine firecracker.MachineIface,
	driveMounter drivemount.DriveMounterService,
) error {
	err := sd.jail.ExposeFileToJail(sd.driveMount.HostPath)
	if err != nil {
		return errors.Wrap(err, "failed to expose patched drive contents to jail")
	}

	err = machine.UpdateGuestDrive(requestCtx, sd.driveID, sd.driveMount.HostPath)
	if err != nil {
		return errors.Wrap(err, "failed to patch drive")
	}

	_, err = driveMounter.MountDrive(requestCtx, &drivemount.MountDriveRequest{
		DriveID:         sd.driveID,
		DestinationPath: sd.driveMount.VMPath,
		FilesytemType:   sd.driveMount.FilesystemType,
		Options:         sd.driveMount.Options,
	})
	if err != nil {
		return errors.Wrap(err, "failed to mount newly patched drive")
	}

	return nil
}

// CacheType sets the stub drive's cacheType value from the provided DriveMount configs.
func (sd *stubDrive) setCacheType(cacheType string) {
	if cacheType != "" {
		sd.driveMount.CacheType = cacheType
	}
}
