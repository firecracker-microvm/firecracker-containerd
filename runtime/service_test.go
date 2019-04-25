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
	"syscall"
	"testing"

	"github.com/containerd/typeurl"
	proto "github.com/firecracker-microvm/firecracker-containerd/proto/grpc"
	"github.com/stretchr/testify/require"
	"gotest.tools/assert"
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

	_, err := findNextAvailableVsockCID(context.Background())
	require.NoError(t, err,
		"Do you have permission to interact with /dev/vhost-vsock?\n"+
			"Grant yourself permission with `sudo setfacl -m u:${USER}:rw /dev/vhost-vsock`")
	// we generate a random CID, so it's not possible to make assertions on its value

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err = findNextAvailableVsockCID(ctx)
	require.Equal(t, context.Canceled, err)
}

func TestParseCreateTaskOptsParsesFirecrackerConfig(t *testing.T) {
	inFirecrackerConfig := &proto.FirecrackerConfig{
		NetworkInterfaces: []*proto.FirecrackerNetworkInterface{
			{
				MacAddress:  mac,
				HostDevName: hostDevName,
			},
		},
	}

	protoFirecrackerConfig, err := typeurl.MarshalAny(inFirecrackerConfig)
	require.NoError(t, err, "unable to marshal firecracker config proto message")
	outFirecrackerConfig, outRuncOpts, err := parseCreateTaskOpts(context.TODO(), protoFirecrackerConfig)
	require.NoError(t, err, "unable to parse firecracker config from proto message")
	if outRuncOpts != nil {
		// assert.Equal is insufficient here as the nil comparison fails for
		// empty Any protobuf message
		t.Error("unexpected value parsed for runc options")
	}
	assert.Equal(t, 1, len(outFirecrackerConfig.NetworkInterfaces))
	assert.Equal(t, hostDevName, outFirecrackerConfig.NetworkInterfaces[0].HostDevName)
	assert.Equal(t, mac, outFirecrackerConfig.NetworkInterfaces[0].MacAddress)
}

func TestParseCreateTaskLeavesNonFirecrackerConfigAlong(t *testing.T) {
	in := &proto.FirecrackerNetworkInterface{
		MacAddress:  mac,
		HostDevName: hostDevName,
	}
	protoIn, err := typeurl.MarshalAny(in)
	require.NoError(t, err, "unable to marshal proto message")
	outFirecrackerConfig, outOpts, err := parseCreateTaskOpts(context.TODO(), protoIn)
	require.NoError(t, err, "unable to parse firecracker config from proto message")
	if outFirecrackerConfig != nil {
		// assert.Equal is insufficient here as the nil comparison fails for
		// empty Any protobuf message
		t.Error("unexpected value parsed for firecracker config")
	}
	assert.Equal(t, protoIn, outOpts)
}
