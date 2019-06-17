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
	"testing"

	"github.com/stretchr/testify/assert"
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
