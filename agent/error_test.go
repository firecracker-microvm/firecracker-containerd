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
	"fmt"
	"syscall"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestIsRetryableMountError(t *testing.T) {
	cases := []struct {
		Name     string
		Error    error
		Expected bool
	}{
		{
			Name:     "nil case",
			Error:    nil,
			Expected: false,
		},
		{
			Name:     "syscall.Errno EINVAL case",
			Error:    syscall.EINVAL,
			Expected: true,
		},
		{
			Name:     "syscall.Errno ENOENT case",
			Error:    syscall.ENOENT,
			Expected: false,
		},
		{
			Name:     "regular error case",
			Error:    fmt.Errorf("foo bar"),
			Expected: false,
		},
	}

	for _, c := range cases {
		c := c // see https://github.com/kyoh86/scopelint/issues/4
		t.Run(c.Name, func(t *testing.T) {
			assert.Equal(t, c.Expected, isRetryableMountError(c.Error))
		})
	}
}
