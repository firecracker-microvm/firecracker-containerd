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

package internal

import (
	"bytes"
	"io"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestIsStubDrive(t *testing.T) {
	cases := []struct {
		name     string
		expected bool
		r        io.Reader
	}{
		{
			name:     "simple case",
			expected: false,
			r:        strings.NewReader("foo"),
		},
		{
			name:     "lengthy case",
			expected: false,
			r:        strings.NewReader(strings.Repeat("0", 512)),
		},
		{
			name:     "valid max_len+1 case",
			expected: true,
			r:        strings.NewReader(string(append(MagicStubBytes, 0xFF)) + strings.Repeat("0", 0xFF+1)),
		},
		{
			name:     "valid max_len case",
			expected: true,
			r:        strings.NewReader(string(append(MagicStubBytes, 0xFF)) + strings.Repeat("0", 0xFF)),
		},
		{
			name:     "valid case",
			expected: true,
			r:        strings.NewReader(string(append(MagicStubBytes, 3, 100, 200, 255))),
		},
	}

	for _, c := range cases {
		c := c // see https://github.com/kyoh86/scopelint/issues/4
		t.Run(c.name, func(t *testing.T) {
			if e, a := c.expected, IsStubDrive(c.r); e != a {
				t.Errorf("expected %t, but received %t", e, a)
			}
		})
	}
}

func TestGenerateStubContent(t *testing.T) {
	driveID := "foo"
	stubContent, err := GenerateStubContent(driveID)
	assert.NoError(t, err)

	expected := append([]byte{}, MagicStubBytes...)
	expected = append(expected, byte(len(driveID)))
	expected = append(expected, []byte(driveID)...)
	assert.Equal(t, string(expected), stubContent)
}

func TestGenerateStubContent_LongID(t *testing.T) {
	driveID := strings.Repeat("0", 0xFF+1)
	_, err := GenerateStubContent(driveID)
	assert.Error(t, err)
}

func TestParseStubContent(t *testing.T) {
	expectedDriveID := "foo"
	contents := append([]byte{}, MagicStubBytes...)
	contents = append(contents, byte(len(expectedDriveID)))
	contents = append(contents, []byte(expectedDriveID)...)
	contents = append(contents, []byte("junkcontent")...)

	driveID, err := ParseStubContent(bytes.NewBuffer(contents))
	assert.NoError(t, err)
	assert.Equal(t, expectedDriveID, driveID)
}

func TestStubID(t *testing.T) {
	const expectedDriveID = "foo"
	buf := bytes.NewBuffer(nil)

	stubContent, err := GenerateStubContent(expectedDriveID)
	assert.NoError(t, err)

	isStubBuffer := bytes.NewBuffer([]byte(stubContent))
	assert.True(t, IsStubDrive(isStubBuffer))

	buf.WriteString(stubContent)
	driveID, err := ParseStubContent(buf)
	assert.NoError(t, err)
	assert.Equal(t, expectedDriveID, driveID)
}
