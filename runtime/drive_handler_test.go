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
	"io/ioutil"
	"os"
	"path/filepath"
	"strconv"
	"testing"

	"github.com/containerd/containerd/log"
	"github.com/firecracker-microvm/firecracker-containerd/internal/vm"
	"github.com/firecracker-microvm/firecracker-containerd/proto"
	drivemount "github.com/firecracker-microvm/firecracker-containerd/proto/service/drivemount/ttrpc"
	firecracker "github.com/firecracker-microvm/firecracker-go-sdk"
	ops "github.com/firecracker-microvm/firecracker-go-sdk/client/operations"
	"github.com/firecracker-microvm/firecracker-go-sdk/fctesting"
	"github.com/gogo/protobuf/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type MockDriveMounter struct {
	drivemount.DriveMounterService

	t                       *testing.T
	expectedDestinationPath string
	expectedFilesystemType  string
	expectedOptions         []string
}

func (m *MockDriveMounter) MountDrive(ctx context.Context, req *drivemount.MountDriveRequest) (*types.Empty, error) {
	assert.Equal(m.t, m.expectedDestinationPath, req.DestinationPath)
	assert.Equal(m.t, m.expectedFilesystemType, req.FilesytemType)
	assert.Equal(m.t, m.expectedOptions, req.Options)
	return nil, nil
}

func TestContainerStubs(t *testing.T) {
	ctx := context.Background()
	logger := log.G(ctx)

	tmpDir := t.TempDir()

	stubDir := filepath.Join(tmpDir, "stubs")
	err := os.MkdirAll(stubDir, 0700)
	require.NoError(t, err, "failed to create stub dir")

	patchedSrcDir := filepath.Join(tmpDir, "patchedSrcs")
	err = os.MkdirAll(patchedSrcDir, 0700)
	require.NoError(t, err, "failed to create patched src dir")

	noopJailer := &noopJailer{
		shimDir: vm.Dir(stubDir),
		ctx:     ctx,
		logger:  logger,
	}

	containerCount := 8
	machineCfg := &firecracker.Config{}

	stubDriveHandler, err := CreateContainerStubs(machineCfg, noopJailer, containerCount, logger)
	require.NoError(t, err, "failed to create stub drive handler")
	dirents, err := ioutil.ReadDir(stubDir)
	require.NoError(t, err, "failed to read stub drive dir")
	assert.Len(t, dirents, containerCount)

	for i := 0; i < containerCount; i++ {
		fcDrive := machineCfg.Drives[i]
		assert.False(t, firecracker.BoolValue(fcDrive.IsReadOnly))
		assert.Nil(t, fcDrive.RateLimiter)

		id := strconv.Itoa(i)
		hostPath := filepath.Join(patchedSrcDir, id)
		vmPath := filepath.Join("/", id)
		fsType := "foo4"
		fsOptions := []string{"blah", "bleg"}

		mockMachine, err := firecracker.NewMachine(ctx, firecracker.Config{}, firecracker.WithClient(
			firecracker.NewClient("/path/to/socket", nil, false, firecracker.WithOpsClient(&fctesting.MockClient{
				PatchGuestDriveByIDFn: func(params *ops.PatchGuestDriveByIDParams) (*ops.PatchGuestDriveByIDNoContent, error) {
					assert.Equal(t, hostPath, firecracker.StringValue(&params.Body.PathOnHost))
					return nil, nil
				},
			}))))
		require.NoError(t, err, "failed to create new machine")

		mockDriveMounter := &MockDriveMounter{
			t:                       t,
			expectedDestinationPath: vmPath,
			expectedFilesystemType:  fsType,
			expectedOptions:         append(fsOptions, "rw"),
		}

		err = stubDriveHandler.Reserve(
			ctx, id, hostPath, vmPath, fsType, fsOptions, mockDriveMounter, mockMachine)
		assert.NoError(t, err, "failed to reserve stub drive")

		stub, ok := stubDriveHandler.usedDrives[id]
		assert.True(t, ok)
		assert.Equal(t, hostPath, stub.driveMount.HostPath)
		assert.Equal(t, vmPath, stub.driveMount.VMPath)
		assert.Equal(t, fsType, stub.driveMount.FilesystemType)
		assert.Equal(t, append(fsOptions, "rw"), stub.driveMount.Options)
		assert.True(t, stub.driveMount.IsWritable)
		assert.Nil(t, stub.driveMount.RateLimiter)
	}
}

func TestDriveMountStubs(t *testing.T) {
	ctx := context.Background()
	logger := log.G(ctx)

	tmpDir := t.TempDir()

	stubDir := filepath.Join(tmpDir, "stubs")
	err := os.MkdirAll(stubDir, 0700)
	require.NoError(t, err, "failed to create stub dir")

	patchedSrcDir := filepath.Join(tmpDir, "patchedSrcs")
	err = os.MkdirAll(patchedSrcDir, 0700)
	require.NoError(t, err, "failed to create patched src dir")

	noopJailer := &noopJailer{
		shimDir: vm.Dir(stubDir),
		ctx:     ctx,
		logger:  logger,
	}

	machineCfg := &firecracker.Config{}
	inputDriveMounts := []*proto.FirecrackerDriveMount{
		{
			HostPath:       "/foo/1",
			VMPath:         "/bar/1",
			FilesystemType: "blah1",
			Options:        []string{"blerg1"},
			RateLimiter:    nil,
			IsWritable:     false,
		},
		{
			HostPath:       "/foo/2",
			VMPath:         "/bar/2",
			FilesystemType: "blah2",
			Options:        []string{"blerg2"},
			RateLimiter:    nil,
			IsWritable:     true,
		},
		{
			HostPath:       "/foo/3",
			VMPath:         "/bar/3",
			FilesystemType: "blah3",
			Options:        []string{"blerg3"},
			RateLimiter: &proto.FirecrackerRateLimiter{
				Bandwidth: &proto.FirecrackerTokenBucket{
					OneTimeBurst: 111,
					RefillTime:   222,
					Capacity:     333,
				},
				Ops: &proto.FirecrackerTokenBucket{
					OneTimeBurst: 1111,
					RefillTime:   2222,
					Capacity:     3333,
				},
			},
			IsWritable: false,
		},
		{
			HostPath:       "/foo/4",
			VMPath:         "/bar/4",
			FilesystemType: "blah4",
			Options:        []string{"blerg4"},
			RateLimiter: &proto.FirecrackerRateLimiter{
				Bandwidth: &proto.FirecrackerTokenBucket{
					OneTimeBurst: 999,
					RefillTime:   888,
					Capacity:     777,
				},
				Ops: &proto.FirecrackerTokenBucket{
					OneTimeBurst: 9999,
					RefillTime:   8888,
					Capacity:     7777,
				},
			},
			IsWritable: true,
		},
		{
			HostPath:       "/foo/5",
			VMPath:         "/bar/5",
			FilesystemType: "blah5",
			Options:        []string{"blerg5"},
			RateLimiter:    nil,
			IsWritable:     true,
			CacheType:      "",
		},
		{
			HostPath:       "/foo/6",
			VMPath:         "/bar/6",
			FilesystemType: "blah6",
			Options:        []string{"blerg6"},
			RateLimiter:    nil,
			IsWritable:     true,
			CacheType:      "Unsafe",
		},
	}

	mountableStubDrives, err := CreateDriveMountStubs(machineCfg, noopJailer, inputDriveMounts, logger)
	require.NoError(t, err, "failed to create stub drive handler")

	dirents, err := ioutil.ReadDir(stubDir)
	require.NoError(t, err, "failed to read stub drive dir")
	assert.Len(t, dirents, len(inputDriveMounts))

	for i, mountableStubDrive := range mountableStubDrives {
		inputDriveMount := inputDriveMounts[i]
		hostPath := inputDriveMount.HostPath
		vmPath := inputDriveMount.VMPath
		fsType := inputDriveMount.FilesystemType
		rateLimiter := inputDriveMount.RateLimiter
		isWritable := inputDriveMount.IsWritable
		cacheType := inputDriveMount.CacheType

		fsOptions := inputDriveMount.Options
		if isWritable {
			fsOptions = append(fsOptions, "rw")
		} else {
			fsOptions = append(fsOptions, "ro")
		}

		fcDrive := machineCfg.Drives[i]
		assert.Equal(t, !isWritable, firecracker.BoolValue(fcDrive.IsReadOnly))
		assert.Equal(t, rateLimiterFromProto(rateLimiter), fcDrive.RateLimiter)

		stub := mountableStubDrive.(stubDrive)
		assert.Equal(t, hostPath, stub.driveMount.HostPath)
		assert.Equal(t, vmPath, stub.driveMount.VMPath)
		assert.Equal(t, fsType, stub.driveMount.FilesystemType)
		assert.Equal(t, fsOptions, stub.driveMount.Options)
		assert.Equal(t, rateLimiter, stub.driveMount.RateLimiter)
		assert.Equal(t, isWritable, stub.driveMount.IsWritable)
		assert.Equal(t, cacheType, stub.driveMount.CacheType)

		mockMachine, err := firecracker.NewMachine(ctx, firecracker.Config{}, firecracker.WithClient(
			firecracker.NewClient("/path/to/socket", nil, false, firecracker.WithOpsClient(&fctesting.MockClient{
				PatchGuestDriveByIDFn: func(params *ops.PatchGuestDriveByIDParams) (*ops.PatchGuestDriveByIDNoContent, error) {
					assert.Equal(t, hostPath, firecracker.StringValue(&params.Body.PathOnHost))
					return nil, nil
				},
			}))))
		require.NoError(t, err, "failed to create new machine")

		mockDriveMounter := &MockDriveMounter{
			t:                       t,
			expectedDestinationPath: vmPath,
			expectedFilesystemType:  fsType,
			expectedOptions:         fsOptions,
		}

		err = mountableStubDrive.PatchAndMount(ctx, mockMachine, mockDriveMounter)
		assert.NoError(t, err, "failed to patch and mount stub drive")
	}
}

func TestStubPathToDriveID(t *testing.T) {
	for _, stubPath := range []string{
		"/a/b/c",
		"foo-bar",
	} {
		assert.Regexp(t, `^[a-zA-Z0-9_]*$`, stubPathToDriveID(stubPath),
			"unexpected invalid characters in drive ID")
	}
}
