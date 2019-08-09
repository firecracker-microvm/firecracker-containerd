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
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sync"

	"github.com/sirupsen/logrus"

	firecracker "github.com/firecracker-microvm/firecracker-go-sdk"
	models "github.com/firecracker-microvm/firecracker-go-sdk/client/models"

	"github.com/firecracker-microvm/firecracker-containerd/internal"
)

const (
	// fcSectorSize is the sector size of Firecracker
	fcSectorSize = 512
)

var (
	// ErrDrivesExhausted occurs when there are no more drives left to use. This
	// can happen by calling PatchStubDrive greater than the number of drives.
	ErrDrivesExhausted = fmt.Errorf("There are no remaining drives to be used")

	// ErrDriveIDNil should never happen, but we safe guard against nil dereferencing
	ErrDriveIDNil = fmt.Errorf("DriveID of current drive is nil")
)

// stubDriveHandler is used to manage stub drives.
type stubDriveHandler struct {
	RootPath       string
	stubDriveIndex int64
	drives         []models.Drive
	logger         *logrus.Entry
	mutex          sync.Mutex
}

func newStubDriveHandler(path string, logger *logrus.Entry, count int) (*stubDriveHandler, error) {
	h := stubDriveHandler{
		RootPath: path,
		logger:   logger,
	}
	drives, err := h.createStubDrives(count)
	if err != nil {
		return nil, err
	}
	h.drives = drives
	return &h, nil
}

func (h *stubDriveHandler) createStubDrives(stubDriveCount int) ([]models.Drive, error) {
	paths, err := h.stubDrivePaths(stubDriveCount)
	if err != nil {
		return nil, err
	}

	stubDrives := make([]models.Drive, 0, stubDriveCount)
	for i, path := range paths {
		stubDrives = append(stubDrives, models.Drive{
			DriveID:      firecracker.String(fmt.Sprintf("stub%d", i)),
			IsReadOnly:   firecracker.Bool(false),
			PathOnHost:   firecracker.String(path),
			IsRootDevice: firecracker.Bool(false),
		})
	}

	return stubDrives, nil
}

// stubDrivePaths will create stub drives and return the paths associated with
// the stub drives.
func (h *stubDriveHandler) stubDrivePaths(count int) ([]string, error) {
	paths := []string{}
	for i := 0; i < count; i++ {
		driveID := fmt.Sprintf("stub%d", i)
		path := filepath.Join(h.RootPath, driveID)

		if err := h.createStubDrive(driveID, path); err != nil {
			return nil, err
		}

		paths = append(paths, path)
	}

	return paths, nil
}

func (h *stubDriveHandler) createStubDrive(driveID, path string) error {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return err
	}
	defer func() {
		if err := f.Close(); err != nil {
			h.logger.WithError(err).Errorf("unexpected error during %v close", f.Name())
		}
	}()

	stubContent, err := internal.GenerateStubContent(driveID)
	if err != nil {
		return err
	}

	if _, err := f.WriteString(stubContent); err != nil {
		return err
	}

	info, err := f.Stat()
	if err != nil {
		return err
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
	if err := os.Truncate(path, driveSize); err != nil {
		return err
	}

	return nil
}

// GetDrives returns the associated stub drives
func (h *stubDriveHandler) GetDrives() []models.Drive {
	return h.drives
}

// InDriveSet will iterate through all the stub drives and see if the path
// exists on any of the drives
func (h *stubDriveHandler) InDriveSet(path string) bool {
	for _, d := range h.GetDrives() {
		if firecracker.StringValue(d.PathOnHost) == path {
			return true
		}
	}

	return false
}

// PatchStubDrive will replace the next available stub drive with the provided drive
func (h *stubDriveHandler) PatchStubDrive(ctx context.Context, client firecracker.MachineIface, pathOnHost string) (*string, error) {
	h.mutex.Lock()
	defer h.mutex.Unlock()

	// Check to see if stubDriveIndex has increased more than the drive amount.
	if h.stubDriveIndex >= int64(len(h.drives)) {
		return nil, ErrDrivesExhausted
	}

	d := h.drives[h.stubDriveIndex]
	d.PathOnHost = &pathOnHost

	if d.DriveID == nil {
		// this should never happen, but we want to ensure that we never nil
		// dereference
		return nil, ErrDriveIDNil
	}

	h.drives[h.stubDriveIndex] = d

	err := client.UpdateGuestDrive(ctx, firecracker.StringValue(d.DriveID), pathOnHost)
	if err != nil {
		return nil, err
	}

	h.stubDriveIndex++
	return d.DriveID, nil
}
