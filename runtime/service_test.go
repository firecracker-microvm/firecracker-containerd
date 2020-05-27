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
	"testing"

	"github.com/firecracker-microvm/firecracker-go-sdk"
	models "github.com/firecracker-microvm/firecracker-go-sdk/client/models"
	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/firecracker-microvm/firecracker-containerd/config"
	"github.com/firecracker-microvm/firecracker-containerd/internal/debug"
	"github.com/firecracker-microvm/firecracker-containerd/internal/vm"
	"github.com/firecracker-microvm/firecracker-containerd/proto"
)

const (
	mac         = "AA:FC:00:00:00:01"
	hostDevName = "tap0"
)

func TestBuildVMConfiguration(t *testing.T) {
	debugHelper, err := debug.New()
	require.NoError(t, err, "failed to create debug helper")

	namespace := "TestBuildVMConfiguration"
	testcases := []struct {
		name                   string
		request                *proto.CreateVMRequest
		config                 *config.Config
		expectedCfg            *firecracker.Config
		expectedStubDriveCount int
	}{
		{
			name:    "Only Config",
			request: &proto.CreateVMRequest{},
			config: &config.Config{
				KernelArgs:      "KERNEL ARGS",
				KernelImagePath: "KERNEL IMAGE",
				RootDrive:       "ROOT DRIVE",
				CPUTemplate:     "C3",
				DebugHelper:     debugHelper,
			},
			expectedCfg: &firecracker.Config{
				KernelArgs:      "KERNEL ARGS",
				KernelImagePath: "KERNEL IMAGE",
				Drives: []models.Drive{
					{
						DriveID:      firecracker.String("root_drive"),
						PathOnHost:   firecracker.String("ROOT DRIVE"),
						IsReadOnly:   firecracker.Bool(true),
						IsRootDevice: firecracker.Bool(true),
					},
				},
				MachineCfg: models.MachineConfiguration{
					CPUTemplate: models.CPUTemplateC3,
					VcpuCount:   firecracker.Int64(defaultCPUCount),
					MemSizeMib:  firecracker.Int64(defaultMemSizeMb),
					HtEnabled:   firecracker.Bool(false),
				},
			},
			expectedStubDriveCount: 1,
		},
		{
			name: "Only Request",
			request: &proto.CreateVMRequest{
				KernelArgs:      "REQUEST KERNEL ARGS",
				KernelImagePath: "REQUEST KERNEL IMAGE",
				RootDrive: &proto.FirecrackerRootDrive{
					HostPath:   "REQUEST ROOT DRIVE",
					IsWritable: true,
				},
				MachineCfg: &proto.FirecrackerMachineConfiguration{
					CPUTemplate: "C3",
					VcpuCount:   2,
				},
			},
			config: &config.Config{
				DebugHelper: debugHelper,
			},
			expectedCfg: &firecracker.Config{
				KernelArgs:      "REQUEST KERNEL ARGS",
				KernelImagePath: "REQUEST KERNEL IMAGE",
				Drives: []models.Drive{
					{
						DriveID:      firecracker.String("root_drive"),
						PathOnHost:   firecracker.String("REQUEST ROOT DRIVE"),
						IsReadOnly:   firecracker.Bool(false),
						IsRootDevice: firecracker.Bool(true),
					},
				},
				MachineCfg: models.MachineConfiguration{
					CPUTemplate: models.CPUTemplateC3,
					VcpuCount:   firecracker.Int64(2),
					MemSizeMib:  firecracker.Int64(defaultMemSizeMb),
					HtEnabled:   firecracker.Bool(false),
				},
			},
			expectedStubDriveCount: 1,
		},
		{
			name: "Request is prioritized over Config",
			request: &proto.CreateVMRequest{
				KernelArgs:      "REQUEST KERNEL ARGS",
				KernelImagePath: "REQUEST KERNEL IMAGE",
				RootDrive: &proto.FirecrackerRootDrive{
					HostPath:   "REQUEST ROOT DRIVE",
					IsWritable: true,
				},
				MachineCfg: &proto.FirecrackerMachineConfiguration{
					CPUTemplate: "T2",
					VcpuCount:   3,
				},
			},
			config: &config.Config{
				KernelArgs:      "KERNEL ARGS",
				KernelImagePath: "KERNEL IMAGE",
				CPUTemplate:     "C3",
				DebugHelper:     debugHelper,
			},
			expectedCfg: &firecracker.Config{
				KernelArgs:      "REQUEST KERNEL ARGS",
				KernelImagePath: "REQUEST KERNEL IMAGE",
				Drives: []models.Drive{
					{
						DriveID:      firecracker.String("root_drive"),
						PathOnHost:   firecracker.String("REQUEST ROOT DRIVE"),
						IsReadOnly:   firecracker.Bool(false),
						IsRootDevice: firecracker.Bool(true),
					},
				},
				MachineCfg: models.MachineConfiguration{
					CPUTemplate: models.CPUTemplateT2,
					VcpuCount:   firecracker.Int64(3),
					MemSizeMib:  firecracker.Int64(defaultMemSizeMb),
					HtEnabled:   firecracker.Bool(false),
				},
			},
			expectedStubDriveCount: 1,
		},
		{
			name: "Request can omit some fields",
			request: &proto.CreateVMRequest{
				KernelArgs:      "REQUEST KERNEL ARGS",
				KernelImagePath: "REQUEST KERNEL IMAGE",
				RootDrive: &proto.FirecrackerRootDrive{
					HostPath: "REQUEST ROOT DRIVE",
				},
				MachineCfg: &proto.FirecrackerMachineConfiguration{},
			},
			config: &config.Config{
				KernelArgs:      "KERNEL ARGS",
				KernelImagePath: "KERNEL IMAGE",
				CPUTemplate:     "C3",
				DebugHelper:     debugHelper,
			},
			expectedCfg: &firecracker.Config{
				KernelArgs:      "REQUEST KERNEL ARGS",
				KernelImagePath: "REQUEST KERNEL IMAGE",
				Drives: []models.Drive{
					{
						DriveID:      firecracker.String("root_drive"),
						PathOnHost:   firecracker.String("REQUEST ROOT DRIVE"),
						IsReadOnly:   firecracker.Bool(true),
						IsRootDevice: firecracker.Bool(true),
					},
				},
				MachineCfg: models.MachineConfiguration{
					CPUTemplate: models.CPUTemplateC3,
					VcpuCount:   firecracker.Int64(defaultCPUCount),
					MemSizeMib:  firecracker.Int64(defaultMemSizeMb),
					HtEnabled:   firecracker.Bool(false),
				},
			},
			expectedStubDriveCount: 1,
		},
		{
			name:    "Container Count affects StubDriveCount",
			request: &proto.CreateVMRequest{ContainerCount: 2},
			config: &config.Config{
				KernelArgs:      "KERNEL ARGS",
				KernelImagePath: "KERNEL IMAGE",
				RootDrive:       "ROOT DRIVE",
				CPUTemplate:     "C3",
				DebugHelper:     debugHelper,
			},
			expectedCfg: &firecracker.Config{
				KernelArgs:      "KERNEL ARGS",
				KernelImagePath: "KERNEL IMAGE",
				Drives: []models.Drive{
					{
						DriveID:      firecracker.String("root_drive"),
						PathOnHost:   firecracker.String("ROOT DRIVE"),
						IsReadOnly:   firecracker.Bool(true),
						IsRootDevice: firecracker.Bool(true),
					},
				},
				MachineCfg: models.MachineConfiguration{
					CPUTemplate: models.CPUTemplateC3,
					VcpuCount:   firecracker.Int64(defaultCPUCount),
					MemSizeMib:  firecracker.Int64(defaultMemSizeMb),
					HtEnabled:   firecracker.Bool(false),
				},
			},
			expectedStubDriveCount: 2,
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

			svc.shimDir = vm.Dir(tempDir)
			svc.jailer = newNoopJailer(context.Background(), svc.logger, svc.shimDir)

			relSockPath, err := svc.shimDir.FirecrackerSockRelPath()
			require.NoError(t, err, "failed to get firecracker sock rel path")

			relVSockPath, err := svc.shimDir.FirecrackerVSockRelPath()
			require.NoError(t, err, "failed to get firecracker vsock rel path")

			// For values that remain constant between tests, they are written here
			tc.expectedCfg.SocketPath = relSockPath
			tc.expectedCfg.VsockDevices = []firecracker.VsockDevice{{
				Path: relVSockPath,
				ID:   "agent_api",
			}}
			tc.expectedCfg.LogFifo = svc.shimDir.FirecrackerLogFifoPath()
			tc.expectedCfg.MetricsFifo = svc.shimDir.FirecrackerMetricsFifoPath()

			drives := make([]models.Drive, tc.expectedStubDriveCount)
			for i := 0; i < tc.expectedStubDriveCount; i++ {
				hostPath := filepath.Join(tempDir, fmt.Sprintf("ctrstub%d", i))
				drives[i].PathOnHost = firecracker.String(hostPath)
				drives[i].DriveID = firecracker.String(stubPathToDriveID(hostPath))
				drives[i].IsReadOnly = firecracker.Bool(false)
				drives[i].IsRootDevice = firecracker.Bool(false)
			}
			tc.expectedCfg.Drives = append(tc.expectedCfg.Drives, drives...)

			actualCfg, err := svc.buildVMConfiguration(tc.request)
			assert.NoError(t, err)
			require.Equal(t, tc.expectedCfg, actualCfg)
		})
	}
}

func TestDebugConfig(t *testing.T) {
	emptyDebugHelper, err := debug.New()
	require.NoError(t, err, "failed to create empty debug helper")
	fcDebugHelper, err := debug.New(debug.LogLevelFirecrackerDebug)
	require.NoError(t, err, "failed to create firecracker debug helper")

	cases := []struct {
		name    string
		service *service
	}{
		{
			name: "empty",
			service: &service{
				logger: logrus.NewEntry(logrus.New()),
				config: &config.Config{
					DebugHelper: emptyDebugHelper,
				},
			},
		},
		{
			name: "LogLevel set",
			service: &service{
				logger: logrus.NewEntry(logrus.New()),
				config: &config.Config{
					LogLevels:   []string{debug.LogLevelFirecrackerDebug},
					DebugHelper: fcDebugHelper,
				},
			},
		},
	}

	cwd, err := os.Getwd()
	require.NoError(t, err, "failed to get working dir")

	path, err := ioutil.TempDir(cwd, "TestDebugConfig")
	assert.NoError(t, err, "failed to create temp directory")

	defer os.RemoveAll(path)

	for i, c := range cases {
		c := c
		stubDrivePath := filepath.Join(path, fmt.Sprintf("%d", i))
		err := os.MkdirAll(stubDrivePath, os.ModePerm)
		assert.NoError(t, err, "failed to create stub drive path")

		c.service.shimDir = vm.Dir(stubDrivePath)
		c.service.jailer = newNoopJailer(context.Background(), c.service.logger, c.service.shimDir)

		req := proto.CreateVMRequest{}

		cfg, err := c.service.buildVMConfiguration(&req)
		assert.NoError(t, err, "failed to build firecracker configuration")
		assert.Equal(t, c.service.config.DebugHelper.GetFirecrackerLogLevel(), cfg.LogLevel)
	}
}
