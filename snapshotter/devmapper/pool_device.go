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
	"fmt"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/containerd/containerd/log"
	"github.com/hashicorp/go-multierror"
	"github.com/moby/moby/pkg/devicemapper"
	"github.com/pkg/errors"
)

const (
	maxDeviceID = 0xffffff // Device IDs are 24-bit numbers
)

// This needs to be global since 'devicemapper' package is not thread-safe, so
// containerd's snapshotter tests could not be passed.
var mutex sync.Mutex

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

	driverVersion, err := devicemapper.GetDriverVersion()
	if err != nil {
		return nil, errors.Wrap(err, "failed to get driver version")
	}

	log.G(ctx).Debugf("using driver: %s", driverVersion)

	libVersion, err := devicemapper.GetLibraryVersion()
	if err != nil {
		return nil, errors.Wrap(err, "failed to get library version")
	}

	log.G(ctx).Debugf("using lib version: %s", libVersion)

	dataFile, err := os.Open(dataVolume)
	if err != nil {
		return nil, errors.Wrap(err, "failed to open data volume")
	}

	defer dataFile.Close()

	metaFile, err := os.Open(metaVolume)
	if err != nil {
		return nil, errors.Wrap(err, "failed to open meta volume")
	}

	defer metaFile.Close()

	poolPath := getDevMapperPath(poolName)
	if _, err := os.Stat(poolPath); err == nil {
		log.G(ctx).Debugf("reloading existing pool '%s'", poolPath)
		if err := devicemapper.ReloadPool(poolName, dataFile, metaFile, blockSizeSectors); err != nil {
			return nil, errors.Wrapf(err, "failed to reload pool '%s'", poolName)
		}
	} else {
		if !os.IsNotExist(err) {
			return nil, errors.Wrapf(err, "failed to stat for '%s'", poolPath)
		}

		log.G(ctx).Debug("creating new pool device")
		if err := devicemapper.CreatePool(poolName, dataFile, metaFile, blockSizeSectors); err != nil {
			return nil, errors.Wrapf(err, "failed to create thin-pool with name '%s'", poolName)
		}
	}

	return &PoolDevice{
		poolName: poolName,
		devices:  make(map[string]int),
	}, nil
}

func (p *PoolDevice) CreateThinDevice(deviceName string, virtualSizeBytes uint64) (int, error) {
	mutex.Lock()
	defer mutex.Unlock()

	if _, ok := p.devices[deviceName]; ok {
		return 0, errors.Errorf("device with name '%s' already created", deviceName)
	}

	// Create device, retry if device id is taken
	deviceID, err := p.tryAcquireDeviceID(func(thinDeviceID int) error {
		return devicemapper.CreateDevice(p.poolName, thinDeviceID)
	})

	if err != nil {
		return 0, errors.Wrap(err, "failed to create thin device")
	}

	p.devices[deviceName] = deviceID

	devicePath := p.GetDevicePath(p.poolName)
	if err := devicemapper.ActivateDevice(devicePath, deviceName, deviceID, virtualSizeBytes); err != nil {
		return 0, errors.Wrap(err, "failed to activate thin device")
	}

	return deviceID, nil
}

func (p *PoolDevice) CreateSnapshotDevice(deviceName string, snapshotName string, virtualSizeBytes uint64) (int, error) {
	mutex.Lock()
	defer mutex.Unlock()

	deviceID, ok := p.devices[deviceName]
	if !ok {
		return 0, errors.Errorf("device '%s' not found", deviceName)
	}

	if _, ok := p.devices[snapshotName]; ok {
		return 0, errors.Errorf("snapshot with name '%s' already exists", snapshotName)
	}

	// Send 'create_snap' message to pool-device
	devicePoolPath := p.GetDevicePath(p.poolName)
	thinDevicePath := p.GetDevicePath(deviceName)
	snapshotDeviceID, err := p.tryAcquireDeviceID(func(snapshotDeviceID int) error {
		return devicemapper.CreateSnapDevice(devicePoolPath, snapshotDeviceID, thinDevicePath, deviceID)
	})

	if err != nil {
		return 0, errors.Wrap(err, "failed to create snapshot")
	}

	// Activate snapshot
	if err := devicemapper.ActivateDevice(devicePoolPath, snapshotName, snapshotDeviceID, virtualSizeBytes); err != nil {
		return 0, errors.Wrap(err, "failed to activate snapshot device")
	}

	p.devices[snapshotName] = snapshotDeviceID
	return snapshotDeviceID, nil
}

func (p *PoolDevice) RemoveDevice(deviceName string) error {
	mutex.Lock()
	defer mutex.Unlock()

	return p.removeDevice(deviceName, true)
}

func (p *PoolDevice) GetDevicePath(deviceName string) string {
	return getDevMapperPath(deviceName)
}

func (p *PoolDevice) Close(ctx context.Context, removeDevices, removePool bool) error {
	mutex.Lock()
	defer mutex.Unlock()

	var result *multierror.Error

	// Clean thin devices
	if removeDevices {
		for name, id := range p.devices {
			if err := p.removeDevice(name, false); err != nil {
				log.G(ctx).WithError(err).Errorf("failed to remove device '%s' (id: %d)", name, id)
				result = multierror.Append(result, err)
			}
		}
	}

	// Remove thin-pool
	if removePool {
		if err := p.removePool(ctx); err != nil {
			log.G(ctx).WithError(err).Errorf("failed to remove thin-pool '%s'", p.poolName)
			result = multierror.Append(result, err)
		}
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

		if devicemapper.DeviceIDExists(err) {
			// This device ID already taken, try next one
			continue
		}

		// If errored for any other reason, just exit
		return 0, err
	}

	return 0, errors.Errorf("thin-pool error: all device ids are taken")
}

func (p *PoolDevice) removeDevice(name string, deferred bool) error {
	var (
		err        error
		devicePath = p.GetDevicePath(name)
	)

	if deferred {
		if err := devicemapper.RemoveDeviceDeferred(devicePath); err != nil {
			return errors.Wrap(err, "deferred remove failed")
		}

		delete(p.devices, name)
		return nil
	}

	const (
		retryCount          = 10
		delayBetweenRetries = 1 * time.Second
	)

	for i := 0; i < retryCount; i++ {
		err = devicemapper.RemoveDevice(devicePath)
		if err == nil {
			delete(p.devices, name)
			return nil
		}

		// Device no longer exists, do not return any errors
		if err == devicemapper.ErrEnxio {
			return nil
		}

		if err == devicemapper.ErrBusy {
			time.Sleep(delayBetweenRetries)
			continue
		}

		return errors.Wrapf(err, "failed to remove device '%s'", name)
	}

	return errors.Wrapf(err, "failed to remove device '%s' after %d retries", name, retryCount)
}

func (p *PoolDevice) removePool(ctx context.Context) error {
	if err := devicemapper.RemoveDevice(p.poolName); err != nil {
		return errors.Wrap(err, "failed to remove pool")
	}

	if deps, err := devicemapper.GetDeps(p.poolName); err == nil {
		log.G(ctx).Warnf("thin-pool '%s' still has %d dependents", p.poolName, deps.Count)
	}

	return nil
}

func getDevMapperPath(deviceName string) string {
	if strings.HasPrefix(deviceName, "/dev/mapper/") {
		return deviceName
	}

	return fmt.Sprintf("/dev/mapper/%s", deviceName)
}
