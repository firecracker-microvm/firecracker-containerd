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

func TestFindNextAvailableVsockCID(t *testing.T) {
	sysCall = func(trap, a1, a2, a3 uintptr) (r1, r2 uintptr, err syscall.Errno) {
		return 0, 0, 0
	}

	defer func() {
		sysCall = syscall.Syscall
	}()

	cid, err := findNextAvailableVsockCID(context.Background())
	require.NoError(t, err)
	require.EqualValues(t, uint32(3), cid)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err = findNextAvailableVsockCID(ctx)
	require.Equal(t, context.Canceled, err)
}
