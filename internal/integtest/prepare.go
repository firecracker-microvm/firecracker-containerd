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

package integtest

import (
	"testing"

	"github.com/firecracker-microvm/firecracker-containerd/config"
	"github.com/firecracker-microvm/firecracker-containerd/internal"
)

// Prepare is a common integration test setup function which ensures
// isolation and prepares the runtime configuration for firecracker-containerd
func Prepare(t testing.TB, options ...func(*config.Config)) {
	t.Helper()

	internal.RequiresIsolation(t)

	err := writeRuntimeConfig(options...)
	if err != nil {
		t.Error(err)
	}
}
