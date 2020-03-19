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

package cpuset

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestCPUSet(t *testing.T) {
	cases := []struct {
		name           string
		cpus           []int
		mems           []int
		cpuRanges      []_range
		memRanges      []_range
		expectedCPUSet CPUSet
	}{
		{
			name: "empty set",
		},
		{
			name: "single cpu only",
			cpus: []int{0},
			expectedCPUSet: CPUSet{
				cpus: "0",
			},
		},
		{
			name: "cpus only",
			cpus: []int{0, 5, 6},
			expectedCPUSet: CPUSet{
				cpus: "0,5,6",
			},
		},
		{
			name: "single mem only",
			mems: []int{2},
			expectedCPUSet: CPUSet{
				mems: "2",
			},
		},
		{
			name: "mems only",
			mems: []int{2, 8, 3},
			expectedCPUSet: CPUSet{
				mems: "2,8,3",
			},
		},
		{
			name: "cpu single range only",
			cpuRanges: []_range{
				{
					min: 0,
					max: 3,
				},
			},
			expectedCPUSet: CPUSet{
				cpus: "0-3",
			},
		},
		{
			name: "cpu ranges only",
			cpuRanges: []_range{
				{
					min: 0,
					max: 3,
				},
				{
					min: 5,
					max: 10,
				},
			},
			expectedCPUSet: CPUSet{
				cpus: "0-3,5-10",
			},
		},
		{
			name: "mem single range only",
			memRanges: []_range{
				{
					min: 0,
					max: 1,
				},
			},
			expectedCPUSet: CPUSet{
				mems: "0-1",
			},
		},
		{
			name: "mem ranges only",
			memRanges: []_range{
				{
					min: 0,
					max: 1,
				},
				{
					min: 2,
					max: 3,
				},
			},
			expectedCPUSet: CPUSet{
				mems: "0-1,2-3",
			},
		},
		{
			name: "all inclusive",
			cpus: []int{15, 17, 31},
			cpuRanges: []_range{
				{
					min: 0,
					max: 3,
				},
			},
			mems: []int{128, 131, 140},
			memRanges: []_range{
				{
					min: 0,
					max: 1,
				},
				{
					min: 2,
					max: 3,
				},
			},
			expectedCPUSet: CPUSet{
				cpus: "15,17,31,0-3",
				mems: "128,131,140,0-1,2-3",
			},
		},
	}

	for _, _c := range cases {
		c := _c
		t.Run(c.name, func(t *testing.T) {
			b := Builder{}
			for _, cpu := range c.cpus {
				b = b.AddCPU(cpu)
			}

			for _, r := range c.cpuRanges {
				b = b.AddCPURange(r.min, r.max)
			}

			for _, mem := range c.mems {
				b = b.AddMem(mem)
			}

			for _, r := range c.memRanges {
				b = b.AddMemRange(r.min, r.max)
			}

			cpuSet := b.Build()
			assert.Equal(t, c.expectedCPUSet, cpuSet)
		})
	}
}
