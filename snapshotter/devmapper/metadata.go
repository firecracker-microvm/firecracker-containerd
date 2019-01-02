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
	"encoding/json"
	"strconv"

	"github.com/pkg/errors"
	"go.etcd.io/bbolt"
)

// DeviceInfo represents metadata for thin device within thin-pool
type DeviceInfo struct {
	// 24-bit number assigned to a device within thin-pool device
	DeviceID int `json:"device_id"`
	// thin device size
	Size uint64 `json:"size"`
	// Device name to be used in /dev/mapper/
	Name string `json:"name"`
	// Name of parent device (if snapshot)
	ParentName string `json:"parent_name"`
	// true if thin device was actived
	IsActivated bool `json:"is_active"`
}

type (
	DeviceIDCallback   func(deviceID int) error
	DeviceInfoCallback func(deviceInfo *DeviceInfo) error
)

const (
	maxDeviceID = 0xffffff // Device IDs are 24-bit numbers
	deviceFree  = byte(0)
	deviceTaken = byte(1)
)

// Bucket names
var (
	devicesBucketName  = []byte("devices")    // Contains thin devices metadata <device_name>=<DeviceInfo>
	deviceIDBucketName = []byte("device_ids") // Tracks used device ids <device_id_[0..maxDeviceID)>=<byte_[0/1]>
)

var (
	ErrNotFound      = errors.New("not found")
	ErrAlreadyExists = errors.New("object already exists")
)

// PoolMetadata keeps device info for the given thin-pool device, it also reponsible for
// generating next available device ids and tracking devmapper transaction numbers
type PoolMetadata struct {
	db *bolt.DB
}

func NewPoolMetadata(dbfile string) (*PoolMetadata, error) {
	db, err := bolt.Open(dbfile, 0600, nil)
	if err != nil {
		return nil, err
	}

	metadata := &PoolMetadata{db: db}
	if err := metadata.ensureDatabaseInitialized(); err != nil {
		return nil, errors.Wrap(err, "failed to initialize database")
	}

	return metadata, nil
}

func (m *PoolMetadata) ensureDatabaseInitialized() error {
	return m.db.Update(func(tx *bolt.Tx) error {
		if _, err := tx.CreateBucketIfNotExists(devicesBucketName); err != nil {
			return err
		}

		if _, err := tx.CreateBucketIfNotExists(deviceIDBucketName); err != nil {
			return err
		}

		return nil
	})
}

// AddDevice saves device info to database.
// Callback should be used to aquire device ID or to rollback transaction in case of error.
func (m *PoolMetadata) AddDevice(ctx context.Context, info *DeviceInfo, fn DeviceIDCallback) error {
	return m.db.Update(func(tx *bolt.Tx) error {
		devicesBucket := tx.Bucket(devicesBucketName)

		// Make sure device name is unique
		if err := getObject(devicesBucket, info.Name, nil); err == nil {
			return ErrAlreadyExists
		}

		// Find next available device ID
		deviceID, err := getNextDeviceID(tx)
		if err != nil {
			return err
		}

		if err := fn(deviceID); err != nil {
			return err
		}

		info.DeviceID = deviceID

		if err := putObject(devicesBucket, info.Name, info, false); err != nil {
			return err
		}

		return nil
	})
}

func getNextDeviceID(tx *bolt.Tx) (int, error) {
	bucket := tx.Bucket(deviceIDBucketName)
	cursor := bucket.Cursor()

	// Check if any device id can be reused.
	// Bolt stores its keys in byte-sorted order within a bucket.
	// This makes sequential iteration extremely fast.
	for key, taken := cursor.First(); key != nil; key, taken = cursor.Next() {
		isFree := taken[0] == deviceFree
		if isFree {
			id, err := strconv.Atoi(string(key))
			if err != nil {
				return 0, err
			}

			if err := markDeviceID(tx, id, deviceTaken); err != nil {
				return 0, err
			}

			return id, nil
		}
	}

	// Try allocate new device ID
	seq, err := bucket.NextSequence()
	if err != nil {
		return 0, err
	}

	if seq >= maxDeviceID {
		return 0, errors.Errorf("couldn't find free device key")
	}

	id := int(seq)
	if err := markDeviceID(tx, id, deviceTaken); err != nil {
		return 0, err
	}

	return id, nil
}

func markDeviceID(tx *bolt.Tx, deviceID int, state byte) error {
	var (
		bucket = tx.Bucket(deviceIDBucketName)
		key    = strconv.Itoa(deviceID)
		value  = []byte{state}
	)

	if err := bucket.Put([]byte(key), value); err != nil {
		return errors.Wrapf(err, "failed to free device id %q", key)
	}

	return nil
}

// UpdateDevice updates device info in metadata store.
// Callback should be used to modify device properties or to rollback update transaction.
// Name and Device ID couldn't be changed and will be ignored.
func (m *PoolMetadata) UpdateDevice(ctx context.Context, name string, fn DeviceInfoCallback) error {
	return m.db.Update(func(tx *bolt.Tx) error {
		var (
			device = &DeviceInfo{}
			bucket = tx.Bucket(devicesBucketName)
		)

		if err := getObject(bucket, name, device); err != nil {
			return err
		}

		// Don't allow changing these values, keep things in sync with devmapper
		name := device.Name
		devID := device.DeviceID

		if err := fn(device); err != nil {
			return err
		}

		device.Name = name
		device.DeviceID = devID

		return putObject(bucket, name, device, true)
	})
}

// GetDevice retrieves device info by name from database
func (m *PoolMetadata) GetDevice(ctx context.Context, name string) (*DeviceInfo, error) {
	var (
		dev DeviceInfo
		err error
	)

	err = m.db.View(func(tx *bolt.Tx) error {
		bucket := tx.Bucket(devicesBucketName)
		return getObject(bucket, name, &dev)
	})

	return &dev, err
}

// RemoveDevice removes device info from store
func (m *PoolMetadata) RemoveDevice(ctx context.Context, name string, fn DeviceInfoCallback) error {
	return m.db.Update(func(tx *bolt.Tx) error {
		var (
			device = &DeviceInfo{}
			bucket = tx.Bucket(devicesBucketName)
		)

		if err := getObject(bucket, name, device); err != nil {
			return err
		}

		if err := bucket.Delete([]byte(name)); err != nil {
			return errors.Wrapf(err, "failed to delete device info for %q", name)
		}

		if err := markDeviceID(tx, device.DeviceID, deviceFree); err != nil {
			return err
		}

		return fn(device)
	})
}

// GetDeviceNames retrieves the list of device names currently stored in database
func (m *PoolMetadata) GetDeviceNames(ctx context.Context) ([]string, error) {
	var (
		names []string
		err   error
	)

	err = m.db.View(func(tx *bolt.Tx) error {
		bucket := tx.Bucket(devicesBucketName)
		return bucket.ForEach(func(k, _ []byte) error {
			names = append(names, string(k))
			return nil
		})
	})

	if err != nil {
		return nil, err
	}

	return names, nil
}

// Close closes metadata store
func (m *PoolMetadata) Close() error {
	if err := m.db.Close(); err != nil && err != bolt.ErrDatabaseNotOpen {
		return err
	}

	return nil
}

func putObject(bucket *bolt.Bucket, key string, obj interface{}, overwrite bool) error {
	keyBytes := []byte(key)

	if !overwrite && bucket.Get(keyBytes) != nil {
		return errors.Errorf("object with key %q already exists", key)
	}

	data, err := json.Marshal(obj)
	if err != nil {
		return errors.Wrapf(err, "failed to marshal object with key %q", key)
	}

	if err := bucket.Put(keyBytes, data); err != nil {
		return errors.Wrapf(err, "failed to insert object with key %q", key)
	}

	return nil
}

func getObject(bucket *bolt.Bucket, key string, obj interface{}) error {
	data := bucket.Get([]byte(key))
	if data == nil {
		return ErrNotFound
	}

	if obj != nil {
		if err := json.Unmarshal(data, obj); err != nil {
			return errors.Wrapf(err, "failed to unmarshal object with key %q", key)
		}
	}

	return nil
}
