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

import "github.com/containerd/containerd/cmd/ctr/app"

// Right now, firecracker-ctr command is built by Makefile.
// Go's toolchain is not aware about the command and "go mod tidy" removes
// the command's dependencies.
// This test file makes the ctr command "visible" from Go's toolchain.
var _ = app.New
