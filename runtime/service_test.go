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
