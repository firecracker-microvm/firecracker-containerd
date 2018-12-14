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

package naive

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/containerd/containerd/log"
	"github.com/containerd/containerd/mount"
	"github.com/containerd/containerd/snapshots"
	"github.com/containerd/containerd/snapshots/storage"
	"github.com/containerd/continuity/fs"
	"github.com/pkg/errors"
	"github.com/sirupsen/logrus"

	"github.com/firecracker-microvm/firecracker-containerd/snapshotter/pkg/losetup"
)

const (
	metadataFileName  = "metadata.db"
	imageDirName      = "images"
	imageFSType       = "ext4"
	sparseImageSizeMB = 256
)

type Snapshotter struct {
	root  string
	store *storage.MetaStore
}

// NewSnapshotter creates a snapshotter for Firecracker.
// Each layer is represented by separate Linux image and corresponding containerd's snapshot ID.
// Snapshotter has the following file structure:
// 	{root}/images/{ID} - keeps filesystem images
// 	{root}/metadata.db - keeps metadata (info and relationships between layers)
func NewSnapshotter(ctx context.Context, root string) (snapshots.Snapshotter, error) {
	log.G(ctx).WithField("root", root).Info("creating naive snapshotter")

	root, err := filepath.Abs(root)
	if err != nil {
		log.G(ctx).WithError(err).Error("failed to get absoluate path")
		return nil, err
	}

	for _, path := range []string{
		root,
		filepath.Join(root, imageDirName),
	} {
		if err := os.Mkdir(path, 0755); err != nil && !os.IsExist(err) {
			log.G(ctx).WithError(err).Errorf("mkdir failed for '%s'", path)
			return nil, err
		}
	}

	ms, err := storage.NewMetaStore(filepath.Join(root, metadataFileName))
	if err != nil {
		return nil, err
	}

	return &Snapshotter{
		root:  root,
		store: ms,
	}, nil
}

func (s *Snapshotter) Stat(ctx context.Context, key string) (snapshots.Info, error) {
	log.G(ctx).WithField("key", key).Debug("stat")

	ctx, trans, err := s.store.TransactionContext(ctx, false)
	if err != nil {
		return snapshots.Info{}, err
	}

	defer trans.Rollback()

	_, info, _, err := storage.GetInfo(ctx, key)
	if err != nil {
		return snapshots.Info{}, err
	}

	return info, nil
}

func (s *Snapshotter) Update(ctx context.Context, info snapshots.Info, fieldpaths ...string) (snapshots.Info, error) {
	log.G(ctx).Debugf("update: %s", strings.Join(fieldpaths, ", "))

	ctx, trans, err := s.store.TransactionContext(ctx, true)
	if err != nil {
		return snapshots.Info{}, err
	}

	info, err = storage.UpdateInfo(ctx, info, fieldpaths...)
	if err != nil {
		return snapshots.Info{}, complete(ctx, trans, err)
	}

	return info, complete(ctx, trans, nil)
}

func (s *Snapshotter) Usage(ctx context.Context, key string) (snapshots.Usage, error) {
	log.G(ctx).WithField("key", key).Debug("usage")

	return snapshots.Usage{}, errors.New("not implemented")
}

func (s *Snapshotter) Mounts(ctx context.Context, key string) ([]mount.Mount, error) {
	log.G(ctx).WithField("key", key).Debug("mounts")

	ctx, trans, err := s.store.TransactionContext(ctx, false)
	if err != nil {
		return nil, err
	}

	defer trans.Rollback()

	snap, err := storage.GetSnapshot(ctx, key)
	if err != nil {
		return nil, err
	}

	return s.buildMounts(snap)
}

func (s *Snapshotter) Prepare(ctx context.Context, key, parent string, opts ...snapshots.Opt) ([]mount.Mount, error) {
	log.G(ctx).WithFields(logrus.Fields{"key": key, "parent": parent}).Debug("prepare")
	return s.createSnapshot(ctx, snapshots.KindActive, key, parent, opts...)
}

func (s *Snapshotter) View(ctx context.Context, key, parent string, opts ...snapshots.Opt) ([]mount.Mount, error) {
	log.G(ctx).WithFields(logrus.Fields{"key": key, "parent": parent}).Debug("view")
	return s.createSnapshot(ctx, snapshots.KindView, key, parent, opts...)
}

func (s *Snapshotter) Commit(ctx context.Context, name, key string, opts ...snapshots.Opt) error {
	log.G(ctx).WithFields(logrus.Fields{"name": name, "key": key}).Debug("commit")

	ctx, trans, err := s.store.TransactionContext(ctx, true)
	if err != nil {
		return err
	}

	snapID, _, _, err := storage.GetInfo(ctx, key)
	if err != nil {
		return complete(ctx, trans, err)
	}

	imagePath := s.getImagePath(snapID)
	if err := losetup.RemoveLoopDevicesAssociatedWithImage(imagePath); err != nil {
		return complete(ctx, trans, err)
	}

	if _, err := storage.CommitActive(ctx, key, name, snapshots.Usage{}, opts...); err != nil {
		return complete(ctx, trans, err)
	}

	return complete(ctx, trans, nil)
}

// Remove unmounts an image and deletes it from images directory
func (s *Snapshotter) Remove(ctx context.Context, key string) error {
	log.G(ctx).WithField("key", key).Debug("remove")

	ctx, trans, err := s.store.TransactionContext(ctx, true)
	if err != nil {
		return err
	}

	id, _, err := storage.Remove(ctx, key)
	if err != nil {
		return complete(ctx, trans, err)
	}

	imagePath := s.getImagePath(id)

	if err := losetup.RemoveLoopDevicesAssociatedWithImage(imagePath); err != nil {
		log.G(ctx).WithError(err).Errorf("failed to detach loop devices from '%s'", imagePath)
		return complete(ctx, trans, err)
	}

	if err := os.Remove(imagePath); err != nil {
		log.G(ctx).WithError(err).Errorf("failed to delete image '%s'", imagePath)
		return complete(ctx, trans, err)
	}

	return complete(ctx, trans, nil)
}

func (s *Snapshotter) Walk(ctx context.Context, fn func(context.Context, snapshots.Info) error) error {
	log.G(ctx).Debug("walk")

	ctx, trans, err := s.store.TransactionContext(ctx, false)
	if err != nil {
		return err
	}

	defer trans.Rollback()
	return storage.WalkInfo(ctx, fn)
}

func (s *Snapshotter) Close() error {
	log.L.Debug("close")

	if err := s.store.Close(); err != nil {
		return err
	}

	// Find all images and detach loop devices if any
	imageDir := filepath.Join(s.root, imageDirName)
	err := filepath.Walk(imageDir, func(path string, info os.FileInfo, err error) error {
		if info.IsDir() {
			return nil
		}

		fullImagePath := filepath.Join(imageDir, info.Name())
		return losetup.RemoveLoopDevicesAssociatedWithImage(fullImagePath)
	})

	return err
}

// createSnapshot creates an active snapshot for containerd and returns mount path.
// Behind the scene it creates an image, builds ext4 file system, and takes care of parent snapshot if needed.
// Command line for this will look like:
// 	dd if=/dev/zero of=drive-2.img bs=1k count=102400
// 	mkfs -t ext4 image.img
// If snapshot has a parent, all files from parent will be copied to this snapshot first.
func (s *Snapshotter) createSnapshot(ctx context.Context, kind snapshots.Kind, key, parent string, opts ...snapshots.Opt) ([]mount.Mount, error) {
	ctx, trans, err := s.store.TransactionContext(ctx, true)
	if err != nil {
		return nil, err
	}

	snap, err := storage.CreateSnapshot(ctx, kind, key, parent, opts...)
	if err != nil {
		return nil, complete(ctx, trans, err)
	}

	hasParent := len(snap.ParentIDs) > 0
	if !hasParent {
		// TODO: figure out what to do with file size
		imagePath := s.getImagePath(snap.ID)
		if err := s.createImage(ctx, imagePath, sparseImageSizeMB); err != nil {
			return nil, complete(ctx, trans, err)
		}
	} else {
		parentID := snap.ParentIDs[0]
		log.G(ctx).Infof("copying data from parent snapshot %s", parentID)

		if err := fs.CopyFile(s.getImagePath(snap.ID), s.getImagePath(parentID)); err != nil {
			log.G(ctx).WithError(err).Errorf("failed copy parent layer")
			return nil, complete(ctx, trans, err)
		}
	}

	mounts, err := s.buildMounts(snap)
	if err != nil {
		return nil, complete(ctx, trans, err)
	}

	if !hasParent {
		// lost+found breaks diff comparisons of containerd snapshotter test suite
		// Not needed for containerd snapshots
		_ = mount.WithTempMount(ctx, mounts, func(root string) error {
			return os.Remove(filepath.Join(root, "lost+found"))
		})
	}

	return mounts, complete(ctx, trans, nil)
}

func (s *Snapshotter) createImage(ctx context.Context, imagePath string, fileSizeMB int) error {
	// Create a new empty file and resize
	log.G(ctx).WithField("image", imagePath).Infof("creating new image of size %d MB", fileSizeMB)
	file, err := os.Create(imagePath)
	if err != nil {
		return err
	}

	if err := file.Truncate(int64(fileSizeMB) * 1048576); err != nil {
		return err
	}

	if err := file.Close(); err != nil {
		return err
	}

	// Build a Linux filesystem
	log.G(ctx).WithField("image", file.Name()).Info("building file system")
	if err := run("mkfs", "-t", imageFSType, "-F", file.Name()); err != nil {
		return err
	}

	return nil
}

func (s *Snapshotter) getImagePath(id string) string {
	return filepath.Join(s.root, imageDirName, id)
}

func (s *Snapshotter) buildMounts(snap storage.Snapshot) ([]mount.Mount, error) {
	options := []string{"sync", "dirsync"}

	if snap.Kind != snapshots.KindActive {
		options = append(options, "ro")
	}

	var (
		imagePath  = s.getImagePath(snap.ID)
		loopDevice string
	)

	// Try find existing loop device attached to the given image
	loopDeviceList, err := losetup.FindAssociatedLoopDevices(imagePath)
	if err != nil {
		return nil, err
	}

	if len(loopDeviceList) > 0 {
		loopDevice = loopDeviceList[0]
	} else {
		// Find first unused loop device and attach to image
		loopDevice, err = losetup.AttachLoopDevice(imagePath)
		if err != nil {
			return nil, err
		}
	}

	mounts := []mount.Mount{
		{
			Source:  loopDevice,
			Type:    imageFSType,
			Options: options,
		},
	}

	return mounts, nil
}

func run(cmd string, args ...string) error {
	command := exec.Command(cmd, args...)
	if err := command.Run(); err != nil {
		return errors.Wrapf(err, "exec failed: %s %s", cmd, strings.Join(args, " "))
	}

	return nil
}

func complete(ctx context.Context, trans storage.Transactor, err error) error {
	if err != nil {
		if terr := trans.Rollback(); terr != nil {
			log.G(ctx).WithError(terr).Error("failed to rollback transaction")
		}
	} else {
		if terr := trans.Commit(); terr != nil {
			log.G(ctx).WithError(terr).Error("failed to commit transaction")
		}
	}

	return err
}
