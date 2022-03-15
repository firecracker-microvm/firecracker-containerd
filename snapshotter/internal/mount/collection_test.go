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

package mount

import (
	"fmt"
	"testing"

	"github.com/containerd/containerd/mount"
)

func assertEqual(actual, expected []mount.Mount) error {
	if len(actual) != len(expected) {
		return fmt.Errorf("expected %s actual %s", expected, actual)
	}
	for k, v := range actual {
		if v.Type != expected[k].Type && v.Source != expected[k].Source {
			return fmt.Errorf("expected %s actual %s at index %d", expected[k], v, k)
		}
	}
	return nil
}

func okOnEmptySlice() error {
	expected := []mount.Mount{}
	actual := Map([]mount.Mount{}, nil)
	return assertEqual(actual, expected)
}

func applyFunctionToSlice() error {
	pre := []mount.Mount{
		{Type: "pre-type-1", Source: "pre-source-1"},
		{Type: "pre-type-2", Source: "pre-source-2"},
	}
	transform := func(m mount.Mount) mount.Mount {
		switch m.Type {
		case "pre-type-1":
			return mount.Mount{Type: "post-type-1", Source: "post-source-1"}
		case "pre-type-2":
			return mount.Mount{Type: "post-type-2", Source: "post-source-2"}
		default:
			return mount.Mount{}
		}
	}

	expected := []mount.Mount{
		{Type: "post-type-1", Source: "post-source-1"},
		{Type: "post-type-2", Source: "post-source-2"},
	}
	actual := Map(pre, transform)
	return assertEqual(actual, expected)
}

func TestMap(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		run  func() error
	}{
		{"EmptySlice", okOnEmptySlice},
		{"ApplyFoo", applyFunctionToSlice},
	}

	for _, test := range tests {
		if err := test.run(); err != nil {
			t.Fatalf("%s: %s", test.name, err.Error())
		}
	}
}
