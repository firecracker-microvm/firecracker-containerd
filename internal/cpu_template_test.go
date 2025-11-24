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

// Package internal contains internal utilities.
package internal

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFindFirstVendorID(t *testing.T) {
	cases := []struct {
		input    string
		vendorID string
	}{
		{"vendor_id : GenuineIntel", "GenuineIntel"},
		{"vendor_id : AuthenticAMD", "AuthenticAMD"},

		// aarch64 doesn't have vendor IDs.
		{"", ""},
	}
	for _, c := range cases {
		r := strings.NewReader(c.input)
		id, err := findFirstVendorID(r)
		require.NoError(t, err)
		assert.Equal(t, c.vendorID, id)
	}
}
