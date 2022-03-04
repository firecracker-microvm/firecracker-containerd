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
	"os"

	"github.com/containerd/containerd/cmd/containerd/command"
	"github.com/containerd/containerd/pkg/seed"

	// Register containerd builtins
	// See https://github.com/containerd/containerd/blob/main/cmd/containerd/builtins.go
	_ "github.com/containerd/containerd/diff/walking/plugin"
	_ "github.com/containerd/containerd/events/plugin"
	_ "github.com/containerd/containerd/gc/scheduler"
	_ "github.com/containerd/containerd/runtime/restart/monitor"
	_ "github.com/containerd/containerd/services/containers"
	_ "github.com/containerd/containerd/services/content"
	_ "github.com/containerd/containerd/services/diff"
	_ "github.com/containerd/containerd/services/events"
	_ "github.com/containerd/containerd/services/healthcheck"
	_ "github.com/containerd/containerd/services/images"
	_ "github.com/containerd/containerd/services/introspection"
	_ "github.com/containerd/containerd/services/leases"
	_ "github.com/containerd/containerd/services/namespaces"
	_ "github.com/containerd/containerd/services/opt"
	_ "github.com/containerd/containerd/services/snapshots"
	_ "github.com/containerd/containerd/services/tasks"
	_ "github.com/containerd/containerd/services/version"

	// Linux specific builtins
	// See https://github.com/containerd/containerd/blob/main/cmd/containerd/builtins_linux.go
	_ "github.com/containerd/containerd/metrics/cgroups"
	_ "github.com/containerd/containerd/runtime/v1/linux"
	_ "github.com/containerd/containerd/runtime/v2"
	_ "github.com/containerd/containerd/runtime/v2/runc/options"

	// Snapshotters
	_ "github.com/containerd/containerd/snapshots/devmapper/plugin"
	_ "github.com/containerd/containerd/snapshots/overlay/plugin"

	// Register cri plugin
	_ "github.com/containerd/containerd/pkg/cri"

	// Register fc-control plugin
	_ "github.com/firecracker-microvm/firecracker-containerd/firecracker-control"
)

func init() {
	seed.WithTimeAndRand()
}

func main() {
	app := command.App()
	if err := app.Run(os.Args); err != nil {
		fmt.Fprintf(os.Stderr, "containerd: %s\n", err)
		os.Exit(1)
	}
}
