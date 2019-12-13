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
	"os"
	"testing"

	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"

	"github.com/firecracker-microvm/firecracker-containerd/proto"
)

func TestCopyFile_simple(t *testing.T) {
	srcPath := "./firecracker-runc-config.json.example"
	dstPath := "./test-copy-file"

	const expectedMode = 0600
	err := copyFile(srcPath, dstPath, expectedMode)
	assert.NoError(t, err, "failed to copy file")
	defer os.Remove(dstPath)

	info, err := os.Stat(dstPath)
	assert.NoError(t, err, "failed to stat file")
	assert.Equal(t, os.FileMode(expectedMode), info.Mode())
}

func TestCopyFile_invalidPaths(t *testing.T) {
	srcPath := "./invalid.path"
	dstPath := "./test-copy-file"

	err := copyFile(srcPath, dstPath, 0600)
	assert.Error(t, err, "copyFile should have returned an error")
}

func TestJailer_invalidUIDGID(t *testing.T) {
	req := proto.CreateVMRequest{
		JailerConfig: &proto.JailerConfig{},
	}
	_, err := newJailer(context.Background(), logrus.NewEntry(logrus.New()), "/foo", &service{}, &req)
	assert.Error(t, err, "expected invalid uid and gid error, but received none")
}
