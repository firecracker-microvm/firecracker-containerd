// Copyright 2018-2019 Amazon.com, Inc. or its affiliates. All Rights Reserved.
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
	"encoding/json"
	"fmt"
	"io"
	"io/ioutil"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"

	"github.com/firecracker-microvm/firecracker-go-sdk"
	models "github.com/firecracker-microvm/firecracker-go-sdk/client/models"
	"github.com/opencontainers/runtime-spec/specs-go"
	"github.com/pkg/errors"
	"github.com/sirupsen/logrus"

	"github.com/firecracker-microvm/firecracker-containerd/internal"
	"github.com/firecracker-microvm/firecracker-containerd/internal/vm"
)

// runcJailer uses runc to set up a jailed environment for the Firecracker VM.
type runcJailer struct {
	ctx    context.Context
	logger *logrus.Entry
	// ociBundlePath is the path that will be used to create an OCI bundle,
	// https://github.com/opencontainers/runtime-spec/blob/master/bundle.md
	ociBundlePath string
	// runcBinaryPath is the path used to execute the runc binary from.
	runcBinaryPath string
	uid            uint32
	gid            uint32
}

func newRuncJailer(ctx context.Context, logger *logrus.Entry, ociBundlePath, runcBinPath string, uid, gid uint32) (*runcJailer, error) {
	l := logger.WithField("ociBundlePath", ociBundlePath).
		WithField("runcBinaryPath", runcBinPath)

	j := &runcJailer{
		ctx:            ctx,
		logger:         l,
		ociBundlePath:  ociBundlePath,
		runcBinaryPath: runcBinPath,
		uid:            uid,
		gid:            gid,
	}

	rootPath := j.RootPath()

	const mode = os.FileMode(0700)
	// Create the proper paths needed for the runc jailer
	j.logger.WithField("rootPath", rootPath).Debug("Creating root drive path")
	if err := mkdirAndChown(rootPath, mode, j.uid, j.gid); err != nil {
		return nil, errors.Wrapf(err, "%s failed to mkdirAndChown", rootPath)
	}

	return j, nil
}

// JailPath returns the base directory from where the jail binary will be ran
// from
func (j runcJailer) OCIBundlePath() string {
	return j.ociBundlePath
}

// RootPath returns the root fs of the jailed system.
func (j runcJailer) RootPath() string {
	return filepath.Join(j.OCIBundlePath(), rootfsFolder)
}

// JailPath will return the OCI bundle rootfs path
func (j runcJailer) JailPath() vm.Dir {
	return vm.Dir(j.RootPath())
}

// BuildJailedMachine will return the needed options for a jailed Firecracker
// instance. In addition, some configuration values will be overwritten to the
// jailed values, like SocketPath in the machineConfig.
func (j *runcJailer) BuildJailedMachine(cfg *Config, machineConfig *firecracker.Config, vmID string) ([]firecracker.Opt, error) {
	handler := j.BuildJailedRootHandler(cfg, &machineConfig.SocketPath, vmID)
	fifoHandler := j.BuildLinkFifoHandler()
	// Build a new client since BuildJailedRootHandler modifies the socket path value.
	client := firecracker.NewClient(machineConfig.SocketPath, j.logger, machineConfig.Debug)

	opts := []firecracker.Opt{
		firecracker.WithProcessRunner(j.jailerCommand(vmID)),
		firecracker.WithClient(client),
		func(m *firecracker.Machine) {
			m.Handlers.FcInit = m.Handlers.FcInit.Prepend(handler)
			// The fifo handler should be appended after the creation of the fifos,
			// ie CreateLogFilesHandlerName. The reason for this is the fifo handler
			// that was created links the files to the jailed path, and if they do
			// not exist an error will occur. The fifo handler should never do
			// anything more than link the fifos and which will make it safe from the
			// handler list changing order.
			m.Handlers.FcInit = m.Handlers.FcInit.AppendAfter(firecracker.CreateLogFilesHandlerName, fifoHandler)
		},
	}

	return opts, nil
}

// BuildJailedRootHandler will populate the jail with the necessary files, which may be
// device nodes, hard links, and/or bind-mount targets
func (j *runcJailer) BuildJailedRootHandler(cfg *Config, socketPath *string, vmID string) firecracker.Handler {
	ociBundlePath := j.OCIBundlePath()
	rootPath := j.RootPath()
	*socketPath = filepath.Join(rootPath, "api.socket")

	return firecracker.Handler{
		Name: jailerHandlerName,
		Fn: func(ctx context.Context, m *firecracker.Machine) error {

			rootPathToConfig := filepath.Join(ociBundlePath, "config.json")
			j.logger.WithField("rootPathToConfig", rootPathToConfig).Debug("Copying config")
			if err := copyFile(runcConfigPath, rootPathToConfig, 0444); err != nil {
				return errors.Wrapf(err, "failed to copy config from %v to %v", runcConfigPath, rootPathToConfig)
			}

			j.logger.Debug("Overwritting process args of config")
			if err := j.overwriteConfig(cfg, filepath.Base(m.Cfg.SocketPath), rootPathToConfig); err != nil {
				return errors.Wrap(err, "failed to overwrite config.json")
			}

			// copy the firecracker binary
			j.logger.WithField("root path", rootPath).Debug("copying firecracker binary")
			newFirecrackerBinPath := filepath.Join(rootPath, filepath.Base(cfg.FirecrackerBinaryPath))
			if err := copyFile(
				cfg.FirecrackerBinaryPath,
				newFirecrackerBinPath,
				0500,
			); err != nil {
				return errors.Wrapf(err, "could not copy firecracker binary from path %v", cfg.FirecrackerBinaryPath)
			}
			if err := os.Chown(newFirecrackerBinPath, int(j.uid), int(j.gid)); err != nil {
				return errors.Wrap(err, "failed to change ownership of binary")
			}

			// copy the kernel image
			newKernelImagePath := filepath.Join(rootPath, kernelImageFileName)
			j.logger.WithField("newKernelImagePath", newKernelImagePath).Debug("copying kernel image")

			if err := copyFile(m.Cfg.KernelImagePath, newKernelImagePath, 0444); err != nil {
				return errors.Wrap(err, "failed to mount kernel image")
			}

			m.Cfg.KernelImagePath = kernelImageFileName

			// copy drives to new contents path
			for i, d := range m.Cfg.Drives {
				drivePath := firecracker.StringValue(d.PathOnHost)
				fileName := filepath.Base(drivePath)
				newDrivePath := filepath.Join(rootPath, fileName)

				f, err := os.Open(drivePath)
				if err != nil {
					return errors.Wrap(err, "failed to open drive file")
				}

				// This closes the file in the event an error occurred, otherwise we
				// call close down below.
				defer f.Close()

				if !internal.IsStubDrive(f) {
					info, err := os.Stat(drivePath)
					if err != nil {
						return errors.Wrapf(err, "failed to stat drive %q", drivePath)
					}

					if err := copyFile(drivePath, newDrivePath, info.Mode()); err != nil {
						return errors.Wrapf(err, "failed to copy drive %v", drivePath)
					}
				}

				if err := f.Close(); err != nil {
					j.logger.WithError(err).Debug("failed to close drive file")
				}

				j.logger.WithField("drive", newDrivePath).Debug("Adding drive")
				m.Cfg.Drives[i].PathOnHost = firecracker.String(fileName)
			}

			// Setting the proper path to where the vsock path should be
			for i, v := range m.Cfg.VsockDevices {
				j.logger.WithField("vsock path", v.Path).Debug("vsock device path being set relative to jailed directory")

				filename := filepath.Base(v.Path)
				v.Path = filepath.Join("/", filename)
				m.Cfg.VsockDevices[i] = v
			}

			j.logger.Info("Successfully ran jailer handler")
			return nil
		},
	}
}

// BuildLinkFifoHandler will return a new firecracker.Handler with the function
// that will allow linking of the fifos making them visible to Firecracker.
func (j runcJailer) BuildLinkFifoHandler() firecracker.Handler {
	return firecracker.Handler{
		Name: jailerFifoHandlerName,
		Fn: func(ctx context.Context, m *firecracker.Machine) error {
			contentsPath := j.RootPath()
			fifoFileName := filepath.Base(m.Cfg.LogFifo)
			newFifoPath := filepath.Join(contentsPath, fifoFileName)
			if err := os.Link(m.Cfg.LogFifo, newFifoPath); err != nil {
				return err
			}
			m.Cfg.LogFifo = newFifoPath

			metricFifoFileName := filepath.Base(m.Cfg.MetricsFifo)
			newMetricFifoPath := filepath.Join(contentsPath, metricFifoFileName)
			if err := os.Link(m.Cfg.MetricsFifo, newMetricFifoPath); err != nil {
				return err
			}
			m.Cfg.MetricsFifo = newMetricFifoPath

			return nil
		},
	}
}

// StubDrivesOptions will return a set of options used to create a new stub
// drive handler.
func (j runcJailer) StubDrivesOptions() []stubDrivesOpt {
	return []stubDrivesOpt{
		func(drives []models.Drive) error {
			for _, drive := range drives {
				path := firecracker.StringValue(drive.PathOnHost)
				if err := os.Chown(path, int(j.uid), int(j.gid)); err != nil {
					return err
				}
			}
			return nil
		},
	}
}

// ExposeDeviceToJail will inspect the given file, srcDevicePath, and based on the
// file type, proper handling will occur to ensure that the file is visible in
// the jail. For block devices we will use mknod to create the device and then
// set the correct permissions to ensure visibility in the jail.
func (j runcJailer) ExposeDeviceToJail(srcDevicePath string) error {
	uid := j.uid
	gid := j.gid

	stat := syscall.Stat_t{}
	if err := syscall.Stat(srcDevicePath, &stat); err != nil {
		return err
	}

	// Checks file type using S_IFMT which is the bit mask for the file type.
	// Here we only care about block devices, ie S_IFBLK.  If it is a block type
	// we will manually call mknod and create that device.
	if (stat.Mode & syscall.S_IFMT) == syscall.S_IFBLK {
		path := filepath.Join(j.RootPath(), filepath.Dir(srcDevicePath))
		if err := mkdirAllWithPermissions(path, 0700, uid, gid); err != nil {
			return err
		}

		dst := filepath.Join(path, filepath.Base(srcDevicePath))
		if err := exposeBlockDeviceToJail(dst, int(stat.Rdev), int(uid), int(gid)); err != nil {
			return err
		}
	} else {
		return fmt.Errorf("unsupported mode: %v", stat.Mode)
	}

	return nil
}

// exposeBlockDeviceToJail will call mknod on the block device to ensure
// visibility of the device
func exposeBlockDeviceToJail(dst string, rdev, uid, gid int) error {
	if err := syscall.Mknod(dst, syscall.S_IFBLK, rdev); err != nil {
		return err
	}

	if err := os.Chmod(dst, 0600); err != nil {
		return err
	}

	if err := os.Chown(dst, uid, gid); err != nil {
		return err
	}

	return nil
}

func copyFile(src, dst string, mode os.FileMode) error {
	srcFile, err := os.Open(src)
	if err != nil {
		return errors.Wrapf(err, "failed to open %v", src)
	}
	defer srcFile.Close()

	dstFile, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_EXCL, mode)
	if err != nil {
		return errors.Wrapf(err, "failed to open %v", dstFile)
	}
	defer dstFile.Close()

	_, err = io.Copy(dstFile, srcFile)
	if err != nil {
		return errors.Wrap(err, "failed to copy to destination")
	}
	return nil
}

func (j runcJailer) jailerCommand(containerName string) *exec.Cmd {
	cmd := exec.CommandContext(j.ctx, j.runcBinaryPath, "run", containerName)
	cmd.Dir = j.OCIBundlePath()
	return cmd
}

// overwriteConfig will set the proper default values if a field had not been set.
//
// TODO: Add netns
func (j runcJailer) overwriteConfig(cfg *Config, socketPath, configPath string) error {
	spec := specs.Spec{}
	configBytes, err := ioutil.ReadFile(configPath)
	if err != nil {
		return err
	}

	if err := json.Unmarshal(configBytes, &spec); err != nil {
		return err
	}

	if spec.Process.User.UID != 0 ||
		spec.Process.User.GID != 0 {
		return fmt.Errorf(
			"using UID %d and GID %d, these values must not be set",
			spec.Process.User.UID,
			spec.Process.User.GID,
		)
	}

	spec = j.setDefaultConfigValues(cfg, socketPath, spec)

	spec.Root.Path = rootfsFolder
	spec.Root.Readonly = false
	spec.Process.User.UID = j.uid
	spec.Process.User.GID = j.gid

	configBytes, err = json.Marshal(&spec)
	if err != nil {
		return err
	}

	if err := ioutil.WriteFile(configPath, configBytes, 0444); err != nil {
		return err
	}

	return nil
}

// setDefaultConfigValues will process the spec file provided and allow any
// empty/zero values to be replaced with default values.
func (j runcJailer) setDefaultConfigValues(cfg *Config, socketPath string, spec specs.Spec) specs.Spec {
	if spec.Process == nil {
		spec.Process = &specs.Process{}
	}

	if spec.Process.Args == nil {
		cmd := firecracker.VMCommandBuilder{}.
			WithBin("/firecracker").
			WithSocketPath(socketPath).
			// Don't need to pass in an actual context here as we are only building
			// the command arguments and not actually building a command
			Build(context.Background())

		spec.Process.Args = cmd.Args
	}

	return spec
}

func mkdirAndChown(path string, mode os.FileMode, uid, gid uint32) error {
	if err := os.Mkdir(path, mode); err != nil {
		return err
	}

	if err := os.Chown(path, int(uid), int(gid)); err != nil {
		return err
	}

	return nil
}

// mkdirAllWithPermissions will create any directories in the provided path that
// don't exist (similar to os.MkdirAll) and will chmod/chown newly created
// directories using the provided mode, uid and gid. If a directory in the path
// already exists, its mode and ownership are left unmodified.
func mkdirAllWithPermissions(path string, mode os.FileMode, uid, gid uint32) error {
	var workingPath string
	if strings.HasPrefix(path, "/") {
		workingPath = "/"
	}

	for _, pathPart := range strings.Split(filepath.Clean(path), "/") {
		workingPath = filepath.Join(workingPath, pathPart)

		err := mkdirAndChown(workingPath, mode, uid, gid)
		if err != nil && !os.IsExist(err) {
			return err
		}
	}

	return nil
}
