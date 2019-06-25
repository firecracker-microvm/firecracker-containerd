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
	"io/ioutil"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/containerd/containerd/log"

	"github.com/firecracker-microvm/firecracker-go-sdk"
	models "github.com/firecracker-microvm/firecracker-go-sdk/client/models"
	ops "github.com/firecracker-microvm/firecracker-go-sdk/client/operations"
	"github.com/firecracker-microvm/firecracker-go-sdk/fctesting"
	"github.com/stretchr/testify/assert"
)

func TestStubDriveHandler(t *testing.T) {
	const tempPath = "test"
	err := os.Mkdir(tempPath, os.ModePerm)
	assert.NoError(t, err)
	defer func() {
		os.RemoveAll(tempPath)
	}()

	logger := log.G(context.Background())
	handler := newStubDriveHandler(tempPath, logger)
	paths, err := handler.StubDrivePaths(5)
	assert.NoError(t, err)
	assert.Equal(t, 5, len(paths))

	infos, err := ioutil.ReadDir(tempPath)
	assert.NoError(t, err)
	assert.Equal(t, 5, len(infos))
}

func TestPatchStubDrive(t *testing.T) {
	ctx := context.Background()
	index := 0
	expectedReplacements := []string{
		"/correct/path0",
		"/correct/path1",
		"/correct/path2",
	}

	mockClient := &fctesting.MockClient{
		PatchGuestDriveByIDFn: func(params *ops.PatchGuestDriveByIDParams) (*ops.PatchGuestDriveByIDNoContent, error) {
			assert.Equal(t, expectedReplacements[index], firecracker.StringValue(params.Body.PathOnHost))
			index++

			return nil, nil
		},
	}

	fcClient := firecracker.NewClient("/path/to/socket", nil, false, firecracker.WithOpsClient(mockClient))
	client, err := firecracker.NewMachine(ctx, firecracker.Config{}, firecracker.WithClient(fcClient))
	assert.NoError(t, err, "failed to create new machine")

	handler := stubDriveHandler{
		drives: []models.Drive{
			{
				DriveID:    firecracker.String("stub0"),
				PathOnHost: firecracker.String("/fake/stub/path0"),
			},
			{
				DriveID:    firecracker.String("stub1"),
				PathOnHost: firecracker.String("/fake/stub/path1"),
			},
			{
				DriveID:    firecracker.String("stub2"),
				PathOnHost: firecracker.String("/fake/stub/path2"),
			},
		},
	}

	expectedDriveIDs := []string{
		"stub0",
		"stub1",
		"stub2",
	}

	for i, path := range expectedReplacements {
		driveID, err := handler.PatchStubDrive(ctx, client, path)
		assert.NoError(t, err, "failed to patch stub drive")
		assert.Equal(t, expectedDriveIDs[i], firecracker.StringValue(driveID), "drive ids are not equal")
	}
}

func TestPatchStubDrive_concurrency(t *testing.T) {
	ctx := context.Background()
	mockClient := &fctesting.MockClient{
		PatchGuestDriveByIDFn: func(params *ops.PatchGuestDriveByIDParams) (*ops.PatchGuestDriveByIDNoContent, error) {
			return nil, nil
		},
	}

	fcClient := firecracker.NewClient("/path/to/socket", nil, false, firecracker.WithOpsClient(mockClient))
	client, err := firecracker.NewMachine(ctx, firecracker.Config{}, firecracker.WithClient(fcClient))
	assert.NoError(t, err, "failed to create new machine")

	handler := stubDriveHandler{
		drives: []models.Drive{
			{
				DriveID:    firecracker.String("stub0"),
				PathOnHost: firecracker.String("/fake/stub/path0"),
			},
			{
				DriveID:    firecracker.String("stub1"),
				PathOnHost: firecracker.String("/fake/stub/path1"),
			},
			{
				DriveID:    firecracker.String("stub2"),
				PathOnHost: firecracker.String("/fake/stub/path2"),
			},
			{
				DriveID:    firecracker.String("stub3"),
				PathOnHost: firecracker.String("/fake/stub/path3"),
			},
			{
				DriveID:    firecracker.String("stub4"),
				PathOnHost: firecracker.String("/fake/stub/path4"),
			},
			{
				DriveID:    firecracker.String("stub5"),
				PathOnHost: firecracker.String("/fake/stub/path5"),
			},
			{
				DriveID:    firecracker.String("stub6"),
				PathOnHost: firecracker.String("/fake/stub/path6"),
			},
			{
				DriveID:    firecracker.String("stub7"),
				PathOnHost: firecracker.String("/fake/stub/path7"),
			},
			{
				DriveID:    firecracker.String("stub8"),
				PathOnHost: firecracker.String("/fake/stub/path8"),
			},
			{
				DriveID:    firecracker.String("stub9"),
				PathOnHost: firecracker.String("/fake/stub/path9"),
			},
			{
				DriveID:    firecracker.String("stub10"),
				PathOnHost: firecracker.String("/fake/stub/path10"),
			},
			{
				DriveID:    firecracker.String("stub11"),
				PathOnHost: firecracker.String("/fake/stub/path11"),
			},
		},
	}

	replacementPaths := []string{
		"/correct/path0",
		"/correct/path1",
		"/correct/path2",
		"/correct/path3",
		"/correct/path4",
		"/correct/path5",
		"/correct/path6",
		"/correct/path7",
		"/correct/path8",
		"/correct/path9",
		"/correct/path10",
		"/correct/path11",
	}
	var wg sync.WaitGroup
	wg.Add(len(replacementPaths))
	for _, path := range replacementPaths {
		go func(path string) {
			defer wg.Done()
			_, err := handler.PatchStubDrive(ctx, client, path)
			assert.NoError(t, err, "failed to patch stub drive")
		}(path)
	}

	wg.Wait()

	validPaths := map[string]struct{}{}
	for _, path := range replacementPaths {
		validPaths[path] = struct{}{}
	}

	assert.Equal(t, len(validPaths), len(handler.drives), "incorrect drive amount")
	for _, drive := range handler.drives {
		path := firecracker.StringValue(drive.PathOnHost)
		_, ok := validPaths[path]
		assert.True(t, ok, "path was not in valid path map")
		delete(validPaths, path)
	}

}

func TestCreateStubDrive(t *testing.T) {
	cases := []struct {
		Name          string
		DriveID       string
		ExpectedSize  int64
		ExpectedError bool
	}{
		{
			Name:         "valid case",
			DriveID:      "foo",
			ExpectedSize: fcSectorSize,
		},
		{
			Name:         "residual bytes case",
			DriveID:      strings.Repeat("0", 0xFF),
			ExpectedSize: fcSectorSize,
		},
	}

	tmpDir := os.TempDir()
	path, err := ioutil.TempDir(tmpDir, "TestCreateStubDrive")
	assert.NoError(t, err, "failed to create test directory")
	defer os.RemoveAll(path)

	for _, c := range cases {
		c := c // see https://github.com/kyoh86/scopelint/issues/4
		t.Run(c.Name, func(t *testing.T) {
			logger := log.G(context.Background())
			handler := newStubDriveHandler(path, logger)
			stubDrivePath := filepath.Join(path, c.Name)
			err := handler.createStubDrive(c.DriveID, stubDrivePath)
			assert.Equal(t, c.ExpectedError, err != nil, "invalid error: %v", err)

			info, err := os.Stat(stubDrivePath)
			assert.NoError(t, err, "failed to stat %v", stubDrivePath)
			assert.Equal(t, c.ExpectedSize, info.Size(), "mismatch of sizes")
		})
	}
}
