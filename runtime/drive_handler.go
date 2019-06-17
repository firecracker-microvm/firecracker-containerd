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
	"sync/atomic"

	firecracker "github.com/firecracker-microvm/firecracker-go-sdk"
	models "github.com/firecracker-microvm/firecracker-go-sdk/client/models"

	"github.com/firecracker-microvm/firecracker-containerd/internal"
)

const (
	// minFCDriveImageBytes is the minimum size that will be visible to Firecracker.
	minFCDriveImageBytes = 512
)

var (
	// ErrStubDrivesExhausted occurs when there are no more stub drives left to
	// use. This can happen by calling PatchStubDrive greater than the stub drive
	// list.
	ErrStubDrivesExhausted = fmt.Errorf("There are no remaining stub drives to use")

	// ErrDriveIDNil should never happen, but we safe guard against nil dereferencing
	ErrDriveIDNil = fmt.Errorf("DriveID of current drive is nil")
)

// stubDriveHandler is used to manage stub drives.
type stubDriveHandler struct {
	RootPath       string
	stubDriveIndex int64
	drives         []models.Drive
}

func newStubDriveHandler(path string) stubDriveHandler {
	return stubDriveHandler{
		RootPath: path,
	}
}

// StubDrivePaths will create stub drives and return the paths associated with
// the stub drives.
func (h *stubDriveHandler) StubDrivePaths(amount int) ([]string, error) {
	paths := []string{}
	for i := 0; i < amount; i++ {
		driveID := fmt.Sprintf("stub%d", i)
		path := filepath.Join(h.RootPath, driveID)

		f, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_RDWR, 0600)
		if err != nil {
			return nil, err
		}

		stubContent, err := internal.GenerateStubContent(driveID)
		if err != nil {
			f.Close()
			return nil, err
		}

		// Ensure that we do not exceed 512 bytes as that is the minimum amount of
		// bytes needed to be visibile in Firecracker.
		// Format is <Magic Stub Bytes><Byte><ID Bytes> and the size of those bytes
		// cannot exceed 512 bytes.
		if stubContentSize := len(stubContent); stubContentSize > minFCDriveImageBytes {
			return nil, fmt.Errorf(
				"Length of generated content, %d, exceeds %d bytes and would be truncated",
				stubContentSize,
				minFCDriveImageBytes,
			)
		}

		if _, err := f.WriteString(stubContent); err != nil {
			f.Close()
			return nil, err
		}

		// Firecracker will not show any drives smaller than 512 bytes. In
		// addition, the drive is read in chunks of 512 bytes; if the drive size is
		// not a multiple of 512 bytes, then the remainder will not be visible to
		// Firecracker.
		if err := os.Truncate(path, minFCDriveImageBytes); err != nil {
			f.Close()
			return nil, err
		}

		paths = append(paths, path)
		f.Close()
	}

	return paths, nil
}

// SetDrives will set the given drives based off the VM id. The index
// represents where the stub drive index starts.
func (h *stubDriveHandler) SetDrives(index int64, d []models.Drive) {
	h.drives = d
	h.stubDriveIndex = index
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

	if v := atomic.LoadInt64(&h.stubDriveIndex) - 1; v >= int64(len(h.drives)) {
		return nil, ErrStubDrivesExhausted
	}

	stubDriveIndex := atomic.AddInt64(&h.stubDriveIndex, 1) - 1
	// Check to see if stubDriveIndex has increased more than the drive amount.
	// This can occur when operations are racing on the atomic.LoadInt64
	if stubDriveIndex >= int64(len(h.drives)) {
		return nil, ErrStubDrivesExhausted
	}

	d := h.drives[stubDriveIndex]
	d.PathOnHost = &pathOnHost

	if d.DriveID == nil {
		// this should never happen, but we want to ensure that we never nil
		// dereference
		return nil, ErrDriveIDNil
	}

	h.drives[stubDriveIndex] = d

	err := client.UpdateGuestDrive(ctx, firecracker.StringValue(d.DriveID), pathOnHost)
	if err != nil {
		return nil, err
	}

	return d.DriveID, nil
}
