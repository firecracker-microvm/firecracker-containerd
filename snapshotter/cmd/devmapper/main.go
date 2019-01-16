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
	"context"
	"flag"
	"os"

	"github.com/containerd/containerd/snapshots"

	"github.com/firecracker-microvm/firecracker-containerd/snapshotter"
	"github.com/firecracker-microvm/firecracker-containerd/snapshotter/devmapper"
)

const (
	configPathEnvName = "DEVMAPPER_SNAPSHOTTER_CONFIG_PATH"
	defaultConfigPath = "/etc/containerd/devmapper-snapshotter.json"
)

func main() {
	var configPath string
	flag.StringVar(&configPath, "config", "", "Path to devmapper configuration file")
	flag.Parse()

	if configPath == "" {
		configPath = os.Getenv(configPathEnvName)
	}

	if configPath == "" {
		configPath = defaultConfigPath
	}

	snapshotter.Run(func(ctx context.Context) (snapshots.Snapshotter, error) {
		return devmapper.NewSnapshotter(ctx, configPath)
	})
}
