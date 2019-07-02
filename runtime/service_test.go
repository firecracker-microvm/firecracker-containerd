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
	"syscall"
	"testing"

	"github.com/firecracker-microvm/firecracker-containerd/proto"
	"github.com/firecracker-microvm/firecracker-go-sdk"
	models "github.com/firecracker-microvm/firecracker-go-sdk/client/models"
	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	mac         = "AA:FC:00:00:00:01"
	hostDevName = "tap0"
)

func TestFindNextAvailableVsockCID(t *testing.T) {
	sysCall = func(trap, a1, a2, a3 uintptr) (r1, r2 uintptr, err syscall.Errno) {
		return 0, 0, 0
	}

	defer func() {
		sysCall = syscall.Syscall
	}()

	_, _, err := findNextAvailableVsockCID(context.Background())
	require.NoError(t, err,
		"Do you have permission to interact with /dev/vhost-vsock?\n"+
			"Grant yourself permission with `sudo setfacl -m u:${USER}:rw /dev/vhost-vsock`")
	// we generate a random CID, so it's not possible to make assertions on its value

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, _, err = findNextAvailableVsockCID(ctx)
	require.Equal(t, context.Canceled, err)
}

func TestBuildVMConfiguration(t *testing.T) {
	namespace := "TestBuildVMConfiguration"
	rootfsDrive := models.Drive{
		DriveID:      firecracker.String("stub0"),
		PathOnHost:   nil, // will be populated in the for loop
		IsReadOnly:   firecracker.Bool(false),
		IsRootDevice: firecracker.Bool(false),
	}
	testcases := []struct {
		name        string
		request     *proto.CreateVMRequest
		config      *Config
		expectedCfg *firecracker.Config
	}{
		{
			name:    "ConfigFile",
			request: &proto.CreateVMRequest{},
			config: &Config{
				KernelArgs:      "KERNEL ARGS",
				KernelImagePath: "KERNEL IMAGE",
				RootDrive:       "ROOT DRIVE",
				CPUTemplate:     "C3",
				CPUCount:        2,
			},
			expectedCfg: &firecracker.Config{
				KernelArgs:      "KERNEL ARGS",
				KernelImagePath: "KERNEL IMAGE",
				Drives: []models.Drive{
					rootfsDrive,
					{
						DriveID:      firecracker.String("root_drive"),
						PathOnHost:   firecracker.String("ROOT DRIVE"),
						IsReadOnly:   firecracker.Bool(false),
						IsRootDevice: firecracker.Bool(true),
					},
				},
				MachineCfg: models.MachineConfiguration{
					CPUTemplate: models.CPUTemplateC3,
					VcpuCount:   2,
					MemSizeMib:  defaultMemSizeMb,
				},
			},
		},
		{
			name: "Input",
			request: &proto.CreateVMRequest{
				KernelArgs:      "REQUEST KERNEL ARGS",
				KernelImagePath: "REQUEST KERNEL IMAGE",
				RootDrive: &proto.FirecrackerDrive{
					PathOnHost: "REQUEST ROOT DRIVE",
				},
				MachineCfg: &proto.FirecrackerMachineConfiguration{
					CPUTemplate: "C3",
					VcpuCount:   2,
				},
			},
			config: &Config{},
			expectedCfg: &firecracker.Config{
				KernelArgs:      "REQUEST KERNEL ARGS",
				KernelImagePath: "REQUEST KERNEL IMAGE",
				Drives: []models.Drive{
					rootfsDrive,
					{
						DriveID:      firecracker.String("root_drive"),
						PathOnHost:   firecracker.String("REQUEST ROOT DRIVE"),
						IsReadOnly:   firecracker.Bool(false),
						IsRootDevice: firecracker.Bool(true),
					},
				},
				MachineCfg: models.MachineConfiguration{
					CPUTemplate: models.CPUTemplateC3,
					VcpuCount:   2,
					MemSizeMib:  defaultMemSizeMb,
				},
			},
		},
		{
			name: "Priority",
			request: &proto.CreateVMRequest{
				KernelArgs: "REQUEST KERNEL ARGS",
				RootDrive: &proto.FirecrackerDrive{
					PathOnHost: "REQUEST ROOT DRIVE",
				},
				MachineCfg: &proto.FirecrackerMachineConfiguration{
					CPUTemplate: "T2",
					VcpuCount:   3,
				},
			},
			config: &Config{
				KernelArgs:      "KERNEL ARGS",
				KernelImagePath: "KERNEL IMAGE",
				CPUTemplate:     "C3",
				CPUCount:        2,
			},
			expectedCfg: &firecracker.Config{
				KernelArgs:      "REQUEST KERNEL ARGS",
				KernelImagePath: "KERNEL IMAGE",
				Drives: []models.Drive{
					rootfsDrive,
					{
						DriveID:      firecracker.String("root_drive"),
						PathOnHost:   firecracker.String("REQUEST ROOT DRIVE"),
						IsReadOnly:   firecracker.Bool(false),
						IsRootDevice: firecracker.Bool(true),
					},
				},
				MachineCfg: models.MachineConfiguration{
					CPUTemplate: models.CPUTemplateT2,
					VcpuCount:   3,
					MemSizeMib:  defaultMemSizeMb,
				},
			},
		},
	}

	for _, tc := range testcases {
		tc := tc // see https://github.com/kyoh86/scopelint/issues/4
		t.Run(tc.name, func(t *testing.T) {
			svc := &service{
				namespace: namespace,
				logger:    logrus.WithField("test", namespace+"/"+tc.name),
				config:    tc.config,
			}

			tempDir, err := ioutil.TempDir(os.TempDir(), namespace)
			assert.NoError(t, err)
			defer os.RemoveAll(tempDir)

			svc.stubDriveHandler = newStubDriveHandler(tempDir, svc.logger)
			// For values that remain constant between tests, they are written here
			tc.expectedCfg.SocketPath = svc.shimDir.FirecrackerSockPath()
			tc.expectedCfg.VsockDevices = []firecracker.VsockDevice{{Path: "root", CID: svc.machineCID}}
			tc.expectedCfg.LogFifo = svc.shimDir.FirecrackerLogFifoPath()
			tc.expectedCfg.MetricsFifo = svc.shimDir.FirecrackerMetricsFifoPath()
			tc.expectedCfg.Drives[0].PathOnHost = firecracker.String(filepath.Join(tempDir, "stub0"))

			actualCfg, err := svc.buildVMConfiguration(tc.request)
			assert.NoError(t, err)
			require.Equal(t, tc.expectedCfg, actualCfg)
		})
	}
}
