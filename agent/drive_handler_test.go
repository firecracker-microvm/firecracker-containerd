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
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDiscoverDrives(t *testing.T) {
	dh, err := newDriveHandler("./testdata/block", "./testdata/dev")
	assert.NoError(t, err)
	assert.Equal(t, 3, len(dh.drives))

	expectedDriveIDs := []string{
		"stub0",
		"stub1",
		"stub2",
	}

	for i := 0; i < len(expectedDriveIDs); i++ {
		expected := expectedDriveIDs[i]
		t.Run(expected, func(t *testing.T) {
			d, ok := dh.GetDrive(expected)
			assert.True(t, ok)
			assert.Equal(t, expected, d.DriveID)
		})
	}
}

func TestEvalAnySymlinks(t *testing.T) {
	existingSymlink := "/proc/self/cwd" // will always exist and be a symlink to our cwd
	nonExistentPath := filepath.Join(strconv.Itoa(int(time.Now().UnixNano())), "foo")
	testPath := filepath.Join(existingSymlink, nonExistentPath)

	resolvedPath, err := evalAnySymlinks(testPath)
	require.NoError(t, err, "failed to evaluate symlinks in %q", testPath)

	cwd, err := os.Getwd()
	require.NoError(t, err, "failed to get current working dir")
	assert.Equal(t, filepath.Join(cwd, nonExistentPath), resolvedPath)
}

func TestIsOrUnderDir(t *testing.T) {
	type testcase struct {
		baseDir      string
		path         string
		expectedTrue bool
	}

	for _, tc := range []testcase{
		{
			baseDir:      "/foo",
			path:         "/foo/bar",
			expectedTrue: true,
		},
		{
			baseDir:      "/foo/bar",
			path:         "/foo/bar/baz",
			expectedTrue: true,
		},
		{
			baseDir:      "/foo",
			path:         "/foo",
			expectedTrue: true,
		},
		{
			baseDir:      "/foo/bar",
			path:         "/foo/bar",
			expectedTrue: true,
		},
		{
			baseDir:      "/foo",
			path:         "/foobar",
			expectedTrue: false,
		},
		{
			baseDir:      "/foo",
			path:         "/bar",
			expectedTrue: false,
		},
		{
			baseDir:      "/foo/bar",
			path:         "/bar",
			expectedTrue: false,
		},
		{
			baseDir:      "/foo/bar",
			path:         "/foo",
			expectedTrue: false,
		},
		{
			baseDir:      "/foo/bar",
			path:         "/bar/bar",
			expectedTrue: false,
		},
		{
			baseDir:      "/foo",
			path:         "foo",
			expectedTrue: false,
		},
		{
			baseDir:      "/foo",
			path:         "bar",
			expectedTrue: false,
		},
		{
			baseDir:      "/foo",
			path:         "/foo/../foo",
			expectedTrue: true,
		},
		{
			baseDir:      "/foo/bar",
			path:         "/foo/../foo/bar",
			expectedTrue: true,
		},
		{
			baseDir:      "/foo",
			path:         "/foo/../bar",
			expectedTrue: false,
		},
		{
			baseDir:      "/foo",
			path:         "/foo/..bar",
			expectedTrue: true,
		},
		{
			baseDir:      "/foo",
			path:         "/foo/..bar/baz",
			expectedTrue: true,
		},
		{
			baseDir:      "/",
			path:         "/",
			expectedTrue: true,
		},
		{
			baseDir:      "/foo",
			path:         "/",
			expectedTrue: false,
		},
		{
			baseDir:      "/",
			path:         "/foo",
			expectedTrue: true,
		},
	} {
		assert.Equalf(t, tc.expectedTrue, isOrUnderDir(tc.path, tc.baseDir), "unexpected output for isOrUnderDir case %+v", tc)
	}
}
