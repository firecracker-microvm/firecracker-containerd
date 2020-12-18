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

package io

import (
	"testing"

	"github.com/firecracker-microvm/firecracker-containerd/proto"
	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFind(t *testing.T) {
	m := NewManager(logrus.WithFields(logrus.Fields{}))
	_, err := m.Find("task1", "exec1")
	require.Error(t, err)
}

func TestReservePorts(t *testing.T) {
	m := NewManager(logrus.WithFields(logrus.Fields{}))

	p1 := m.ReservePorts(proto.ExtraData{})
	assert.Equal(t, minVsockIOPort, p1.StdinPort)
	assert.Equal(t, minVsockIOPort+1, p1.StdoutPort)
	assert.Equal(t, minVsockIOPort+2, p1.StderrPort)

	p2 := m.ReservePorts(proto.ExtraData{})
	assert.Equal(t, minVsockIOPort+3, p2.StdinPort)
	assert.Equal(t, minVsockIOPort+4, p2.StdoutPort)
	assert.Equal(t, minVsockIOPort+5, p2.StderrPort)
}
