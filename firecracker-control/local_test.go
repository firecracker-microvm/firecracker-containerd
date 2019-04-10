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

//go:generate mockgen -source=local.go -destination=local_mock_test.go -package=service

package service

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/firecracker-microvm/firecracker-go-sdk"
	"github.com/golang/mock/gomock"
	"github.com/stretchr/testify/assert"

	"github.com/firecracker-microvm/firecracker-containerd/proto"
)

var testCtx = context.Background()

func TestLocal_buildVMConfiguration(t *testing.T) {
	request := &proto.CreateVMRequest{
		KernelArgs:      "kernel_args",
		KernelImagePath: "kernel_path",
		MachineCfg:      &proto.FirecrackerMachineConfiguration{},
		RootDrive: &proto.FirecrackerDrive{
			PathOnHost: "/",
		},
		NetworkInterfaces: []*proto.FirecrackerNetworkInterface{
			{MacAddress: "mac", HostDevName: "host", AllowMMDS: true},
		},
	}

	obj := &local{
		rootPath:    "/",
		findVsockFn: func(context.Context) (*os.File, uint32, error) { return nil, 3, nil },
	}

	config, err := obj.buildVMConfiguration(testCtx, "1", 3, request)
	assert.NoError(t, err)
	assert.NotNil(t, config)

	assert.Len(t, config.VsockDevices, 1)
	assert.EqualValues(t, 3, config.VsockDevices[0].CID)

	assert.Equal(t, "kernel_args", config.KernelArgs)
	assert.Equal(t, "kernel_path", config.KernelImagePath)

	assert.Equal(t, "/vm_1.socket", config.SocketPath)
	assert.Equal(t, "/vm_1_log.fifo", config.LogFifo)
	assert.Equal(t, "/vm_1_metrics.fifo", config.MetricsFifo)

	assert.Len(t, config.Drives, 1)

	rootDrive := config.Drives[0]
	assert.True(t, *rootDrive.IsRootDevice)
	assert.Equal(t, "/", *rootDrive.PathOnHost)

	assert.Len(t, config.NetworkInterfaces, 1)

	ni := config.NetworkInterfaces[0]
	assert.Equal(t, "mac", ni.MacAddress)
	assert.Equal(t, "host", ni.HostDevName)
	assert.True(t, ni.AllowMMDS)
}

func TestLocal_buildInvalidConfiguration(t *testing.T) {
	obj := &local{
		rootPath:    "/",
		findVsockFn: func(context.Context) (*os.File, uint32, error) { return nil, 3, nil },
	}

	request := &proto.CreateVMRequest{}

	_, err := obj.buildVMConfiguration(testCtx, "1", 3, request)
	assert.EqualError(t, err, "invalid machine configuration")

	request.MachineCfg = &proto.FirecrackerMachineConfiguration{}
	_, err = obj.buildVMConfiguration(testCtx, "1", 3, request)
	assert.EqualError(t, err, "root drive can't be empty")
}

func TestLocal_makeID(t *testing.T) {
	obj := local{}
	id1, err := obj.makeID()
	assert.NoError(t, err)
	assert.NotEmpty(t, id1)

	id2, err := obj.makeID()
	assert.NoError(t, err)
	assert.NotEqual(t, id1, id2)
}

func TestLocal_startMachine(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	machine := NewMockmachineIface(ctrl)
	machine.EXPECT().Start(gomock.Any()).Times(1).Return(nil)
	machine.EXPECT().Wait(gomock.Any()).Times(1).Return(nil)

	obj := &local{
		vm: map[string]*instance{"1": {}},
	}

	err := obj.startMachine(testCtx, "1", machine)
	time.Sleep(100 * time.Microsecond) // Give it some time to spawn the goroutine
	assert.NoError(t, err)
	assert.Empty(t, obj.vm) // Make sure "1" removed from the VM list
}

func TestLocal_StopVM(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	machine := NewMockmachineIface(ctrl)
	machine.EXPECT().StopVMM().Times(1).Return(nil)

	obj := &local{
		vm: map[string]*instance{"1": {machine: machine}},
	}

	_, err := obj.StopVM(testCtx, &proto.StopVMRequest{VMID: "1"})
	assert.NoError(t, err)
}

func TestLocal_StopInvalidVM(t *testing.T) {
	obj := local{}
	_, err := obj.StopVM(testCtx, &proto.StopVMRequest{VMID: "2"})
	assert.Equal(t, ErrVMNotFound, err)
}

func TestLocal_GetVMAddress(t *testing.T) {
	obj := &local{
		vm: map[string]*instance{"1": {cfg: &firecracker.Config{SocketPath: "2"}}},
	}

	response, err := obj.GetVMAddress(testCtx, &proto.GetVMAddressRequest{VMID: "1"})
	assert.NoError(t, err)
	assert.NotNil(t, response)
	assert.Equal(t, "2", response.SocketPath)
}

func TestLocal_GetFifoPath(t *testing.T) {
	obj := &local{
		vm: map[string]*instance{"1": {cfg: &firecracker.Config{LogFifo: "logs", MetricsFifo: "metrics"}}},
	}

	response, err := obj.GetFifoPath(testCtx, &proto.GetFifoPathRequest{VMID: "1", FifoType: proto.FifoType_LOG})
	assert.NoError(t, err)
	assert.Equal(t, "logs", response.Path)

	response, err = obj.GetFifoPath(testCtx, &proto.GetFifoPathRequest{VMID: "1", FifoType: proto.FifoType_METRICS})
	assert.NoError(t, err)
	assert.Equal(t, "metrics", response.Path)
}

func TestLocal_SetVMMetadata(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	machine := NewMockmachineIface(ctrl)
	machine.EXPECT().SetMetadata(gomock.Any(), gomock.Eq("test")).Times(1).Return(nil)

	obj := &local{
		vm: map[string]*instance{"1": {machine: machine}},
	}

	_, err := obj.SetVMMetadata(testCtx, &proto.SetVMMetadataRequest{VMID: "1", Metadata: "test"})
	assert.NoError(t, err)
}
