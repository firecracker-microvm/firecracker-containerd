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
	"path"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

var invalidContainerIDs = []string{"", "id?", "*", "id/1", "id\\"}

func TestShimDir(t *testing.T) {
	runDir := t.TempDir()

	tests := []struct {
		name   string
		ns     string
		id     string
		outDir string
		outErr string
	}{
		{name: "empty ns", outErr: "invalid namespace: identifier must not be empty"},
		{name: "ns with /", ns: "/", outErr: `invalid namespace: identifier "/" must match`},
		{name: "ns with ?", ns: "?", outErr: `invalid namespace: identifier "?" must match`},
		{name: "ns with *", ns: "*", outErr: `invalid namespace: identifier "*" must match`},
		{name: "ns with ,", ns: ",", outErr: `invalid namespace: identifier "," must match`},
		{name: "empty id", ns: "test", outErr: "invalid vm id: identifier must not be empty"},
		{name: "id with /", ns: "test", id: "/", outErr: `invalid vm id: identifier "/" must match`},
		{name: "id with ?", ns: "test", id: "?", outErr: `invalid vm id: identifier "?" must match`},
		{name: "id with *", ns: "test", id: "*", outErr: `invalid vm id: identifier "*" must match`},
		{name: "id with ,", ns: "test", id: ",", outErr: `invalid vm id: identifier "," must match`},
		{name: "valid", ns: "ns", id: "1", outDir: "ns#1"},
		{name: "valid with dashes", ns: "test-123", id: "123-456", outDir: "test-123#123-456"},
		{name: "valid with dots", ns: "test.123", id: "123.456", outDir: "test.123#123.456"},
		{name: "ns with aaa", ns: "aaa", id: "bbb-ccc", outDir: "aaa#bbb-ccc"},
		{name: "ns with aaa-bbb", ns: "aaa-bbb", id: "ccc", outDir: "aaa-bbb#ccc"},
	}

	for _, tc := range tests {
		test := tc
		t.Run(test.name, func(t *testing.T) {
			dir, err := shimDir(runDir, test.ns, test.id)

			if test.outErr != "" {
				assert.Error(t, err)
				assert.True(t, strings.Contains(err.Error(), test.outErr), err.Error())
			} else {
				assert.NoError(t, err)
				assert.EqualValues(t, dir, path.Join(runDir, test.outDir))
			}
		})
	}
}

func TestBundleLink(t *testing.T) {
	var root Dir = "/root/"

	dir, err := root.BundleLink("1")
	assert.NoError(t, err)
	assert.EqualValues(t, "/root/1", dir)

	dir, err = root.BundleLink("test-1")
	assert.NoError(t, err)
	assert.EqualValues(t, "/root/test-1", dir)
}

func TestBundleLinkInvalidID(t *testing.T) {
	var (
		root Dir = "/root/"
		err  error
	)

	for _, id := range invalidContainerIDs {
		_, err = root.BundleLink(id)
		assert.Error(t, err)
	}
}
