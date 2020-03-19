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
	"fmt"
	"strconv"
	"strings"
)

// Builder will allow building of cpuset fields such as cpuset.cpus and
// cpuset.mems
//
// Example:
//	cset := cpuset.Builder{}.
//		AddCPU(0).
//		AddMemRange(0).
//		Build()
//
//	fcClient, err := fcclient.New(containerdTTRPCAddress)
//	if err != nil {
//		return err
//	}
//
//	defer fcClient.Close()
//
//	vmID := "cpuset-builder-example"
//	createVMRequest := &proto.CreateVMRequest{
//		VMID: vmID,
//		JailerConfig: proto.JailerConfig{
//			CPUs: cset.CPUs(),
//			Mems: cset.Mems(),
//		},
//	}
//
//	_, err = fcClient.CreateVM(ctx, createVMRequest)
//	if err != nil {
//		return errors.Wrap(err, "failed to create VM")
//	}
type Builder struct {
	cpus      []int
	cpuRanges []_range

	mems      []int
	memRanges []_range
}

type _range struct {
	min, max int
}

func (r _range) String() string {
	return fmt.Sprintf("%d-%d", r.min, r.max)
}

// CPUSet represents the linux CPUSet which is a series of configurable values
// that allow processes to run on a specific CPUs and those CPUs are then bound
// to the memory nodes specified.
//
// More information can be found here: http://man7.org/linux/man-pages/man7/cpuset.7.html
type CPUSet struct {
	cpus string
	mems string
}

// CPUs returns the cpuset.cpus string
func (c CPUSet) CPUs() string {
	return c.cpus
}

// Mems returns the cpuset.mems string
func (c CPUSet) Mems() string {
	return c.mems
}

// AddCPU will add the physical CPU number that the process is allowed to run
// on.
func (b Builder) AddCPU(cpu int) Builder {
	b.cpus = append(b.cpus, cpu)
	return b
}

// AddCPURange adds a range of physical CPU numbers that the process is allowed
// to run on
func (b Builder) AddCPURange(min, max int) Builder {
	b.cpuRanges = append(b.cpuRanges, _range{
		min: min,
		max: max,
	})

	return b
}

// AddMem adds a memory node which limits where the cpus can allocate memory
func (b Builder) AddMem(mem int) Builder {
	b.mems = append(b.mems, mem)
	return b
}

// AddMemRange adds a range of memory nodes to be used.
func (b Builder) AddMemRange(min, max int) Builder {
	b.memRanges = append(b.memRanges, _range{
		min: min,
		max: max,
	})

	return b
}

// Build constructs a new CPUSet
func (b Builder) Build() CPUSet {
	cpus := stringify(b.cpus, b.cpuRanges)
	mems := stringify(b.mems, b.memRanges)

	return CPUSet{
		cpus: cpus,
		mems: mems,
	}
}

func stringify(elems []int, ranges []_range) string {
	strs := []string{}
	for _, elem := range elems {
		strs = append(strs, strconv.Itoa(elem))
	}

	for _, r := range ranges {
		strs = append(strs, r.String())
	}

	return strings.Join(strs, ",")
}
