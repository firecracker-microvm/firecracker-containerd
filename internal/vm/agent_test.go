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

package vm

import (
	"testing"

	"github.com/containerd/containerd/cio"
	"github.com/containerd/containerd/log"
	"github.com/stretchr/testify/assert"
)

func TestAgentOnlyIO(t *testing.T) {
	assert.True(t, IsAgentOnlyIO(creator(t, cio.BinaryIO("/root/binary", nil)), log.L))
	assert.True(t, IsAgentOnlyIO(creator(t, cio.LogFile("/log.txt")), log.L))
}

func TestNonAgentIO(t *testing.T) {
	assert.False(t, IsAgentOnlyIO("fifo://test", log.L))
	assert.False(t, IsAgentOnlyIO("/tmp/path", log.L))
	assert.False(t, IsAgentOnlyIO("", log.L))
}

func creator(t *testing.T, creator cio.Creator) string {
	t.Helper()

	io, err := creator("!")
	assert.NoError(t, err)

	return io.Config().Stdout
}
