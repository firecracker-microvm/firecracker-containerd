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

package devmapper

import (
	"context"
	"os"
	"path/filepath"

	"github.com/containerd/containerd/log"
	"github.com/hashicorp/go-multierror"
	"github.com/pkg/errors"

	"github.com/firecracker-microvm/firecracker-containerd/snapshotter/pkg/dmsetup"
)

// PoolDevice ties together data and metadata volumes, represents thin-pool and manages volumes, snapshots and device ids.
type PoolDevice struct {
	poolName string
	metadata *PoolMetadata
}

// NewPoolDevice creates new thin-pool from existing data and metadata volumes.
// If pool 'poolName' already exists, it'll be reloaded with new parameters.
func NewPoolDevice(ctx context.Context, config *Config) (*PoolDevice, error) {
	log.G(ctx).Infof("initializing pool device %q", config.PoolName)

	version, err := dmsetup.Version()
	if err != nil {
		log.G(ctx).Errorf("dmsetup not available")
		return nil, err
	}

	log.G(ctx).Infof("using dmsetup: %s", version)

	dbpath := filepath.Join(config.RootPath, config.PoolName+".db")
	poolMetaStore, err := NewPoolMetadata(dbpath)
	if err != nil {
		return nil, err
	}

	poolPath := dmsetup.GetFullDevicePath(config.PoolName)
	if _, err := os.Stat(poolPath); err == nil {
		log.G(ctx).Debugf("reloading existing pool %q", poolPath)
		if err := dmsetup.ReloadPool(config.PoolName, config.DataDevice, config.MetadataDevice, config.DataBlockSizeSectors); err != nil {
			return nil, errors.Wrapf(err, "failed to reload pool %q", config.PoolName)
		}
	} else {
		if !os.IsNotExist(err) {
			return nil, errors.Wrapf(err, "failed to stat for %q", poolPath)
		}

		log.G(ctx).Debug("creating new pool device")
		if err := dmsetup.CreatePool(config.PoolName, config.DataDevice, config.MetadataDevice, config.DataBlockSizeSectors); err != nil {
			return nil, errors.Wrapf(err, "failed to create thin-pool with name %q", config.PoolName)
		}
	}

	return &PoolDevice{
		poolName: config.PoolName,
		metadata: poolMetaStore,
	}, nil
}

func (p *PoolDevice) CreateThinDevice(ctx context.Context, deviceName string, virtualSizeBytes uint64) error {
	deviceInfo := &DeviceInfo{
		Name: deviceName,
		Size: virtualSizeBytes,
	}

	// Create thin device and save metadata
	err := p.metadata.AddDevice(ctx, deviceInfo, func(devID int) error {
		return dmsetup.CreateDevice(p.poolName, devID)
	})

	if err != nil {
		return err
	}

	// Activate thin device
	err = p.metadata.UpdateDevice(ctx, deviceName, func(info *DeviceInfo) error {
		info.IsActivated = true
		return dmsetup.ActivateDevice(p.poolName, info.Name, info.DeviceID, info.Size, "")
	})

	return err
}

func (p *PoolDevice) CreateSnapshotDevice(ctx context.Context, deviceName string, snapshotName string, virtualSizeBytes uint64) error {
	baseDeviceInfo, err := p.metadata.GetDevice(ctx, deviceName)
	if err != nil {
		return err
	}

	// Suspend thin device if it was activated previously
	isActivated := baseDeviceInfo.IsActivated
	if isActivated {
		if err := dmsetup.SuspendDevice(deviceName); err != nil {
			return errors.Wrapf(err, "failed to suspend device %q", deviceName)
		}
	}

	snapshotDeviceInfo := &DeviceInfo{
		Name:       snapshotName,
		Size:       virtualSizeBytes,
		ParentName: deviceName,
	}

	err = p.metadata.AddDevice(ctx, snapshotDeviceInfo, func(devID int) error {
		return dmsetup.CreateSnapshot(p.poolName, devID, baseDeviceInfo.DeviceID)
	})

	if err != nil {
		return err
	}

	if isActivated {
		if err := dmsetup.ResumeDevice(deviceName); err != nil {
			return errors.Wrapf(err, "failed to resume device %q", deviceName)
		}
	}

	err = p.metadata.UpdateDevice(ctx, snapshotName, func(info *DeviceInfo) error {
		info.IsActivated = true
		return dmsetup.ActivateDevice(p.poolName, info.Name, info.DeviceID, info.Size, "")
	})

	return err
}

func (p *PoolDevice) RemoveDevice(ctx context.Context, deviceName string, deferred bool) error {
	opts := []dmsetup.RemoveDeviceOpt{dmsetup.RemoveWithForce, dmsetup.RemoveWithRetries}
	if deferred {
		opts = append(opts, dmsetup.RemoveDeferred)
	}

	return p.metadata.UpdateDevice(ctx, deviceName, func(info *DeviceInfo) error {
		info.IsActivated = false
		return dmsetup.RemoveDevice(deviceName, opts...)
	})
}

func (p *PoolDevice) RemovePool(ctx context.Context) error {
	deviceNames, err := p.metadata.GetDeviceNames(ctx)
	if err != nil {
		return errors.Wrap(err, "can't query device names")
	}

	var result *multierror.Error

	for _, name := range deviceNames {
		info, err := p.metadata.GetDevice(ctx, name)
		if err != nil {
			result = multierror.Append(result, errors.Wrapf(err, "failed to get device info %q", name))
			continue
		}

		if info.IsActivated {
			if err := p.RemoveDevice(ctx, name, true); err != nil {
				result = multierror.Append(result, errors.Wrapf(err, "failed to remove %q", name))
			}
		}
	}

	if err := dmsetup.RemoveDevice(p.poolName, dmsetup.RemoveWithForce, dmsetup.RemoveWithRetries, dmsetup.RemoveDeferred); err != nil {
		result = multierror.Append(result, errors.Wrapf(err, "failed to remove pool %q", p.poolName))
	}

	return result.ErrorOrNil()
}

func (p *PoolDevice) Close() error {
	return p.metadata.Close()
}
