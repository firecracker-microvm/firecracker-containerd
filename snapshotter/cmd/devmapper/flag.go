// Copyright 2018 Amazon.com, Inc. or its affiliates. All Rights Reserved.
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
	"os"
	"strings"
)

// parseKeyValueOpt splits a string to key=value
func parseKeyValueOpt(opt string) (string, string, error) {
	pair := strings.SplitN(opt, "=", 2)
	if len(pair) != 2 {
		return "", "", fmt.Errorf("failed to split option: %q", opt)
	}

	return strings.TrimSpace(pair[0]), strings.TrimSpace(pair[1]), nil
}

// visitKeyValueOpts finds all --optName key=value in command line string and invokes callback for each pair
func visitKeyValueOpts(optName string, optFn func(key, value string) error) error {
	// Nothing to visit
	if len(os.Args) < 3 {
		return nil
	}

	// Skip exec path
	args := os.Args[1:]

	for i := 0; i < len(args)-1; i++ {
		if optName != args[i] {
			continue
		}

		// Next element must be key=value pair
		key, value, err := parseKeyValueOpt(args[i+1])
		if err != nil {
			return err
		}

		if err := optFn(key, value); err != nil {
			return err
		}
	}

	return nil
}
