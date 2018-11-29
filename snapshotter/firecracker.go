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

package snapshotter

import (
	"context"
	"fmt"
	"io/ioutil"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/containerd/containerd/log"
	"github.com/containerd/containerd/mount"
	"github.com/containerd/containerd/snapshots"
	"github.com/containerd/containerd/snapshots/storage"
	"github.com/containerd/continuity/fs"
	"github.com/firecracker-microvm/firecracker-containerd/internal"
	"github.com/pkg/errors"
	"github.com/sirupsen/logrus"
)

const (
	metadataFileName = "metadata.db"
	imageDirName     = "images"
	mountsDirName    = "mounts"
)

type Snapshotter struct {
	root  string
	store *storage.MetaStore
}

// NewSnapshotter creates a snapshotter for Firecracker.
// Each layer is represented by separate Linux image and corresponding containerd's snapshot ID.
// Snapshotter has the following file structure:
// 	{root}/images/{ID} - keeps filesystem images
// 	{root}/mounts/{ID} - keeps mounts for correcponding images
// 	{root}/metadata.db - keeps metadata (info and relationships between layers)
func NewSnapshotter(ctx context.Context, root string) (snapshots.Snapshotter, error) {
	log.G(ctx).WithField("root", root).Info("creating snapshotter")

	root, err := filepath.Abs(root)
	if err != nil {
		log.G(ctx).WithError(err).Error("failed to get absoluate path")
		return nil, err
	}

	for _, path := range []string{
		root,
		filepath.Join(root, imageDirName),
		filepath.Join(root, mountsDirName),
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

	return s.buildMounts(snap), nil
}

func (s *Snapshotter) Prepare(ctx context.Context, key, parent string, opts ...snapshots.Opt) ([]mount.Mount, error) {
	log.G(ctx).WithFields(logrus.Fields{"key": key, "parent": parent}).Debug("prepare")
	return s.createSnapshot(ctx, snapshots.KindActive, key, parent, opts...)
}

func (s *Snapshotter) View(ctx context.Context, key, parent string, opts ...snapshots.Opt) ([]mount.Mount, error) {
	log.G(ctx).WithFields(logrus.Fields{"key": key, "parent": parent}).Debug("view")
	return s.createSnapshot(ctx, snapshots.KindView, key, parent, opts...)
}

// Commit unmounts mounted active filesystem image
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

	snapshotDir := s.getMountDir(snapID)
	log.G(ctx).Debugf("snapshot directory '%s'", snapshotDir)

	usage, err := fs.DiskUsage(ctx, snapshotDir)
	if err != nil {
		return complete(ctx, trans, err)
	}

	log.G(ctx).Infof("unmounting snapshot with id %s at '%s'", snapID, snapshotDir)
	if err := s.unmount(ctx, snapshotDir); err != nil {
		return complete(ctx, trans, err)
	}

	if _, err := storage.CommitActive(ctx, key, name, snapshots.Usage(usage), opts...); err != nil {
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

	// Unmount image
	mountPath := s.getMountDir(id)
	if exists, err := exists(mountPath); err != nil {
		return complete(ctx, trans, err)
	} else if exists {
		if err := s.unmount(ctx, mountPath); err != nil {
			log.G(ctx).WithError(err).Errorf("failed to unmount image '%s'", mountPath)
			return complete(ctx, trans, err)
		}
	}

	// Delete image
	imagePath := s.getImagePath(id)
	if exists, err := exists(imagePath); err != nil {
		return complete(ctx, trans, err)
	} else if exists {
		log.G(ctx).Infof("deleting image '%s'", imagePath)

		if err := os.Remove(imagePath); err != nil {
			log.G(ctx).WithError(err).Errorf("failed to delete image '%s'", imagePath)
		}
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

	// Make sure all images has been unmounted before closing snapshotter
	mountsPath := filepath.Join(s.root, mountsDirName)
	dirs, err := ioutil.ReadDir(mountsPath)
	if err != nil {
		return err
	}

	for _, dir := range dirs {
		if err := s.unmount(context.Background(), filepath.Join(mountsPath, dir.Name())); err != nil {
			return err
		}
	}

	return nil
}

// createSnapshot creates an active snapshot for containerd and returns mount path.
// Behind the scene it creates an image, builds ext4 file system, mounts it, and takes care of parent snapshot if needed.
// Command line for this will look like:
// 	dd if=/dev/zero of=drive-2.img bs=1k count=102400
// 	mkfs -t ext4 image.img
// 	mount -t ext4 image.img /mount/path
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

	mountDir := s.getMountDir(snap.ID)
	imageDir := s.getImagePath(snap.ID)

	// TODO: figure out what to do with file size
	if err := s.createImage(ctx, imageDir, 100); err != nil {
		return nil, complete(ctx, trans, err)
	}

	readonly := false
	if kind == snapshots.KindView {
		readonly = true
	}

	if err := s.mount(imageDir, mountDir, readonly); err != nil {
		return nil, complete(ctx, trans, err)
	}

	log.G(ctx).Infof("snapshot is mounted at '%s'", mountDir)

	if len(snap.ParentIDs) > 0 {
		parentID := snap.ParentIDs[0]
		parentMountDir := s.getMountDir(parentID)

		log.G(ctx).Infof("copying data from parent snapshot %s", parentMountDir)

		exists, err := exists(parentMountDir)
		if err != nil {
			return nil, complete(ctx, trans, err)
		} else if !exists {
			// Mount parent snapshot for copying
			if err := s.mount(s.getImagePath(parentID), parentMountDir, true); err != nil {
				return nil, complete(ctx, trans, err)
			}
		}

		if err := fs.CopyDir(mountDir, parentMountDir); err != nil {
			log.G(ctx).WithError(err).Error("failed to copy data from parent dir")
			return nil, complete(ctx, trans, err)
		}

		if !exists {
			if err := s.unmount(ctx, parentMountDir); err != nil {
				return nil, complete(ctx, trans, err)
			}
		}
	}

	return s.buildMounts(snap), complete(ctx, trans, nil)
}

func (s *Snapshotter) createImage(ctx context.Context, imagePath string, fileSizeMB int) error {
	// Create a new empty file and resize
	log.G(ctx).WithField("image", imagePath).Infof("creating new image of size %d MB", fileSizeMB)
	file, err := os.Create(imagePath)
	if err != nil {
		return err
	}

	if err := file.Truncate(int64(fileSizeMB) * 100000); err != nil {
		return err
	}

	if err := file.Close(); err != nil {
		return err
	}

	// Build a Linux filesystem
	log.G(ctx).WithField("image", file.Name()).Info("building file system")
	if err := run("mkfs", "-t", "ext4", "-F", file.Name()); err != nil {
		return err
	}

	return nil
}

func (s *Snapshotter) mount(imagePath string, mountPath string, readonly bool) error {
	log.L.WithFields(logrus.Fields{
		"image": imagePath,
		"mount": mountPath,
		"ro":    readonly,
	}).Info("mounting image")

	// Make sure mount path exists
	if _, err := os.Stat(mountPath); os.IsNotExist(err) {
		if err := os.Mkdir(mountPath, 0700); err != nil {
			return err
		}
	}

	// Unmount immediatelly followed by mount causes a mount with no files in it. I assume it needs some time
	// to flush data on disk (async flag used by default in 'mount' util and time.Sleep after unmount confirms
	// this theory). Hence 'sync' flags is needed here.
	if err := run("mount", "-t", "ext4", "-o", "sync", imagePath, mountPath); err != nil {
		return err
	}

	// lost+found breaks diff comparisons of containerd snapshotter test suite
	// Not needed for containerd snapshots as well
	os.Remove(filepath.Join(mountPath, "lost+found"))

	return nil
}

func (s *Snapshotter) unmount(ctx context.Context, mountPath string) error {
	log.G(ctx).WithField("mount", mountPath).Info("unmounting image")

	if err := mount.Unmount(mountPath, 0); err != nil {
		return err
	}

	// Delete empty directory after unmount
	if err := os.Remove(mountPath); err != nil {
		log.G(ctx).WithError(err).Warnf("failed to remove mount dir '%s'", mountPath)
	}

	return nil
}

func (s *Snapshotter) getImagePath(id string) string {
	return filepath.Join(s.root, imageDirName, id)
}

func (s *Snapshotter) getMountDir(id string) string {
	return filepath.Join(s.root, mountsDirName, id)
}

func (s *Snapshotter) buildMounts(snap storage.Snapshot) []mount.Mount {
	options := []string{"bind"}

	if snap.Kind == snapshots.KindView {
		options = append(options, "ro")
	} else {
		options = append(options, "rw")
	}

	// Save filesystem image path so it can be consumed by runtime
	options = append(options, fmt.Sprintf("%s=%s", internal.SnapshotterImageKey, s.getImagePath(snap.ID)))

	mounts := []mount.Mount{
		{
			Source:  s.getMountDir(snap.ID),
			Type:    internal.SnapshotterMountType,
			Options: options,
		},
	}

	return mounts
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

func exists(path string) (bool, error) {
	_, err := os.Stat(path)
	if err == nil {
		return true, nil
	}

	if os.IsNotExist(err) {
		return false, nil
	}

	return false, err
}
