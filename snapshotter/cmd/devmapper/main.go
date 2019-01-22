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
	"strings"

	"github.com/containerd/containerd/log"
	"github.com/containerd/containerd/snapshots"
	"github.com/docker/go-units"

	"github.com/firecracker-microvm/firecracker-containerd/snapshotter"
	"github.com/firecracker-microvm/firecracker-containerd/snapshotter/devmapper"
	"github.com/firecracker-microvm/firecracker-containerd/snapshotter/pkg/dmsetup"
)

const (
	configPathEnvName = "DEVMAPPER_SNAPSHOTTER_CONFIG_PATH"
	defaultConfigPath = "/etc/containerd/devmapper-snapshotter.json"
)

func main() {
	var (
		ctx        = context.Background()
		config     = &devmapper.Config{}
		configPath = ""
		rootPath   = ""
	)

	flag.StringVar(&configPath, "config", "", "Path to devmapper configuration file")
	flag.StringVar(&rootPath, "path", "", "Path to snapshotter data")

	flag.Parse()

	// Try load file from disk
	if cfg, err := loadConfig(configPath); err == nil {
		config = cfg
	} else if err != os.ErrNotExist {
		log.G(ctx).WithError(err).Fatal("failed to load config file")
		return
	}

	if rootPath != "" {
		config.RootPath = rootPath
	}

	// Append and/or overwrite file configuration with --storage-opt dm.XXX=YYY command line flags
	if err := visitKVOpts("--storage-opt", func(key, value string) error {
		return applyStorageOpt(ctx, key, value, config)
	}); err != nil {
		log.G(ctx).WithError(err).Fatal("failed to apply storage options")
		return
	}

	if err := config.Validate(); err != nil {
		log.G(ctx).Fatal("invalid configuration")
		return
	}

	snapshotter.Run(func(ctx context.Context) (snapshots.Snapshotter, error) {
		return devmapper.NewSnapshotter(ctx, config)
	})
}

// loadConfig loads configuration file from disk.
// If file not exists, empty Config struct will be returned
func loadConfig(configPath string) (*devmapper.Config, error) {
	if configPath == "" {
		configPath = os.Getenv(configPathEnvName)
	}

	if configPath == "" {
		configPath = defaultConfigPath
	}

	config, err := devmapper.LoadConfig(configPath)
	if err != nil {
		return nil, err
	}

	return config, nil
}

// applyStorageOpt overwrites configuration with --storage-opt command line flags
func applyStorageOpt(ctx context.Context, key, val string, config *devmapper.Config) error {
	switch key {
	case "dm.basesize":
		size, err := units.RAMInBytes(val)
		if err != nil {
			return err
		}

		config.BaseImageSize = val
		config.BaseImageSizeBytes = uint64(size)
	case "dm.metadatadev":
		config.MetadataDevice = val
	case "dm.datadev":
		config.DataDevice = val
	case "dm.thinpooldev":
		config.PoolName = strings.TrimPrefix(val, dmsetup.DevMapperDir)
	case "dm.blocksize":
		size, err := units.RAMInBytes(val)
		if err != nil {
			return err
		}

		config.DataBlockSize = val
		config.DataBlockSizeSectors = uint32(size / dmsetup.SectorSize)
	default:
		log.G(ctx).Warnf("ignoring unsupported flag %q", key)
	}

	return nil
}
