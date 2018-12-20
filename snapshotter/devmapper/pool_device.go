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

	"github.com/containerd/containerd/log"
	"github.com/hashicorp/go-multierror"
	"github.com/pkg/errors"
	"golang.org/x/sys/unix"

	"github.com/firecracker-microvm/firecracker-containerd/snapshotter/pkg/dmsetup"
)

const (
	maxDeviceID = 0xffffff // Device IDs are 24-bit numbers
)

// PoolDevice ties together data and metadata volumes, represents thin-pool and manages volumes, snapshots and device ids.
type PoolDevice struct {
	poolName        string
	currentDeviceID int
	devices         map[string]int
}

// NewPoolDevice creates new thin-pool from existing data and metadata volumes.
// If pool 'poolName' already exists, it'll be reloaded with new parameters.
func NewPoolDevice(ctx context.Context, poolName, dataVolume, metaVolume string, blockSizeSectors uint32) (*PoolDevice, error) {
	log.G(ctx).Infof("initializing pool device '%s'", poolName)

	version, err := dmsetup.Version()
	if err != nil {
		log.G(ctx).Errorf("dmsetup not available")
	} else {
		log.G(ctx).Debugf("using dmsetup: %s", version)
	}

	poolPath := dmsetup.GetFullDevicePath(poolName)
	if _, err := os.Stat(poolPath); err == nil {
		log.G(ctx).Debugf("reloading existing pool '%s'", poolPath)
		if err := dmsetup.ReloadPool(poolName, dataVolume, metaVolume, blockSizeSectors); err != nil {
			return nil, errors.Wrapf(err, "failed to reload pool '%s'", poolName)
		}
	} else {
		if !os.IsNotExist(err) {
			return nil, errors.Wrapf(err, "failed to stat for '%s'", poolPath)
		}

		log.G(ctx).Debug("creating new pool device")
		if err := dmsetup.CreatePool(poolName, dataVolume, metaVolume, blockSizeSectors); err != nil {
			return nil, errors.Wrapf(err, "failed to create thin-pool with name '%s'", poolName)
		}
	}

	return &PoolDevice{
		poolName: poolName,
		devices:  make(map[string]int),
	}, nil
}

func (p *PoolDevice) CreateThinDevice(deviceName string, virtualSizeBytes uint64) (int, error) {
	if _, ok := p.devices[deviceName]; ok {
		return 0, errors.Errorf("device with name '%s' already created", deviceName)
	}

	// Create device, retry if device id is taken
	deviceID, err := p.tryAcquireDeviceID(func(thinDeviceID int) error {
		return dmsetup.CreateDevice(p.poolName, thinDeviceID)
	})

	if err != nil {
		return 0, errors.Wrap(err, "failed to create thin device")
	}

	p.devices[deviceName] = deviceID
	if err := dmsetup.ActivateDevice(p.poolName, deviceName, deviceID, virtualSizeBytes, ""); err != nil {
		return 0, errors.Wrap(err, "failed to activate thin device")
	}

	return deviceID, nil
}

func (p *PoolDevice) CreateSnapshotDevice(deviceName string, snapshotName string, virtualSizeBytes uint64) (int, error) {
	deviceID, ok := p.devices[deviceName]
	if !ok {
		return 0, errors.Errorf("device '%s' not found", deviceName)
	}

	if _, ok := p.devices[snapshotName]; ok {
		return 0, errors.Errorf("snapshot with name '%s' already exists", snapshotName)
	}

	// TODO: suspend/resume not needed if thin-device not active
	if err := dmsetup.SuspendDevice(deviceName); err != nil {
		return 0, errors.Wrapf(err, "failed to suspend device %q", deviceName)
	}

	snapshotDeviceID, err := p.tryAcquireDeviceID(func(snapshotDeviceID int) error {
		return dmsetup.CreateSnapshot(p.poolName, snapshotDeviceID, deviceID)
	})

	if err != nil {
		return 0, errors.Wrapf(err, "failed to create snapshot %q", snapshotName)
	}

	if err := dmsetup.ResumeDevice(deviceName); err != nil {
		return 0, errors.Wrapf(err, "failed to resume device %q", deviceName)
	}

	// Activate snapshot
	if err := dmsetup.ActivateDevice(p.poolName, snapshotName, snapshotDeviceID, virtualSizeBytes, ""); err != nil {
		return 0, errors.Wrap(err, "failed to activate snapshot device")
	}

	p.devices[snapshotName] = snapshotDeviceID
	return snapshotDeviceID, nil
}

func (p *PoolDevice) RemoveDevice(deviceName string, deferred bool) error {
	if err := dmsetup.RemoveDevice(deviceName, true, true, deferred); err != nil {
		return errors.Wrapf(err, "failed to remove device '%s'", deviceName)
	}

	delete(p.devices, deviceName)
	return nil
}

func (p *PoolDevice) RemovePool() error {
	var result *multierror.Error

	for name := range p.devices {
		if err := p.RemoveDevice(name, true); err != nil {
			result = multierror.Append(result, errors.Wrapf(err, "failed to remove %q", name))
		}
	}

	if err := dmsetup.RemoveDevice(p.poolName, true, true, true); err != nil {
		result = multierror.Append(result, errors.Wrapf(err, "failed to remove pool %q", p.poolName))
	}

	return result.ErrorOrNil()
}

func (p *PoolDevice) getNextDeviceID() int {
	p.currentDeviceID++
	if p.currentDeviceID >= maxDeviceID {
		p.currentDeviceID = 0
	}

	return p.currentDeviceID
}

func (p *PoolDevice) tryAcquireDeviceID(acquire func(deviceID int) error) (int, error) {
	for attempt := 0; attempt < maxDeviceID; attempt++ {
		deviceID := p.getNextDeviceID()
		err := acquire(deviceID)
		if err == nil {
			return deviceID, nil
		}

		if err == unix.EEXIST {
			// This device ID already taken, try next one
			continue
		}

		// If errored for any other reason, just exit
		return 0, err
	}

	return 0, errors.Errorf("thin-pool error: all device ids are taken")
}
