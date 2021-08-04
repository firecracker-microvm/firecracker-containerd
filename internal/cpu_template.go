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
	"bufio"
	"io"
	"os"
	"regexp"
	"runtime"
	"sync"
)

var (
	isIntel     bool
	isIntelOnce sync.Once
)

// SupportCPUTemplate returns true if Firecracker supports CPU templates on
// the current architecture.
func SupportCPUTemplate() (bool, error) {
	if runtime.GOARCH != "amd64" {
		return false, nil
	}

	var err error
	isIntelOnce.Do(func() {
		isIntel, err = checkIsIntel()
	})
	return isIntel, err
}

var vendorID = regexp.MustCompile(`^vendor_id\s*:\s*(.+)$`)

func checkIsIntel() (bool, error) {
	f, err := os.Open("/proc/cpuinfo")
	if err != nil {
		return false, err
	}
	defer f.Close()

	id, err := findFirstVendorID(f)
	if err != nil {
		return false, err
	}

	return id == "GenuineIntel", nil
}

func findFirstVendorID(r io.Reader) (string, error) {
	s := bufio.NewScanner(r)
	for s.Scan() {
		line := s.Text()
		matches := vendorID.FindStringSubmatch(line)
		if len(matches) == 2 {
			return matches[1], nil
		}
	}
	return "", nil
}
