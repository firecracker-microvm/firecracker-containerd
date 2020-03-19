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
	"os"
	"testing"
)

const (
	rootDisableEnvName         = "DISABLE_ROOT_TESTS"
	enableIsolatedTestsEnvName = "ENABLE_ISOLATED_TESTS"
)

var (
	rootDisabled         bool
	isolatedTestsEnabled bool
)

func init() {
	if v := os.Getenv(rootDisableEnvName); len(v) != 0 {
		rootDisabled = true
	}

	if v := os.Getenv(enableIsolatedTestsEnvName); len(v) != 0 {
		isolatedTestsEnabled = true
	}
}

// RequiresRoot will ensure that tests that require root access are actually
// root. In addition, this will skip root tests if the DISABLE_ROOT_TESTS is
// set to true
func RequiresRoot(t testing.TB) {
	if rootDisabled {
		t.Skip("skipping test that requires root")
	}

	if e, a := 0, os.Getuid(); e != a {
		t.Fatalf("This test must be run as root. To disable tests that "+
			"require root, run the tests with the %s environment variable set.",
			rootDisableEnvName)
	}
}

// RequiresIsolation will ensure that tests that must be run in an isolated
// environment are being run with the explicit ENABLE_ISOLATED_TESTS env var.
// If the env var has not been set, the test will be skipped
func RequiresIsolation(t testing.TB) {
	if !isolatedTestsEnabled {
		t.Skip("skipping test that requires isolation")
	}
}
