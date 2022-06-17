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
	"context"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/containerd/go-runc"
	"github.com/hashicorp/go-multierror"
	"github.com/opencontainers/runtime-spec/specs-go"
	"github.com/sirupsen/logrus"
	"golang.org/x/sys/unix"

	"github.com/firecracker-microvm/firecracker-containerd/config"
	"github.com/firecracker-microvm/firecracker-containerd/internal"
	"github.com/firecracker-microvm/firecracker-containerd/internal/vm"
	"github.com/firecracker-microvm/firecracker-containerd/proto"
	firecracker "github.com/firecracker-microvm/firecracker-go-sdk"
)

const (
	networkNamespaceRuncName = "network"
	cacheTopologyPath        = "/sys/devices/system/cpu/cpu0/cache"
	cacheFolderPrefix        = "index"
)

var cacheTopologyPaths = []string{
	"sys",
	"devices",
	"system",
	"cpu",
	"cpu0",
	"cache",
}

var cacheTopologyFiles = []string{
	"level",
	"type",
	"size",
	"number_of_sets",
	"shared_cpu_map",
	"coherency_line_size",
}

// runcJailer uses runc to set up a jailed environment for the Firecracker VM.
type runcJailer struct {
	ctx        context.Context
	logger     *logrus.Entry
	Config     runcJailerConfig
	vmID       string
	configSpec specs.Spec
	runcClient runc.Runc
	started    bool
}

const firecrackerFileName = "firecracker"

type runcJailerConfig struct {
	OCIBundlePath  string
	RuncBinPath    string
	RuncConfigPath string
	UID            uint32
	GID            uint32
	CPUs           string
	Mems           string
	CgroupPath     string

	// DriveExposePolicy defines how the jailer exposes files.
	DriveExposePolicy proto.DriveExposePolicy
}

func newRuncJailer(
	ctx context.Context, logger *logrus.Entry, vmID string, cfg runcJailerConfig,
	mounts []*proto.FirecrackerDriveMount,
) (*runcJailer, error) {
	l := logger.WithField("ociBundlePath", cfg.OCIBundlePath).
		WithField("runcBinaryPath", cfg.RuncBinPath)

	j := &runcJailer{
		ctx:        ctx,
		logger:     l,
		Config:     cfg,
		vmID:       vmID,
		runcClient: runc.Runc{},
	}

	spec := specs.Spec{}
	configBytes, err := ioutil.ReadFile(cfg.RuncConfigPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read %s: %w", cfg.RuncConfigPath, err)
	}

	if err = json.Unmarshal(configBytes, &spec); err != nil {
		return nil, fmt.Errorf("failed to unmarshal %s: %w", cfg.RuncConfigPath, err)
	}

	j.configSpec = spec

	rootPath := j.RootPath()

	const mode = os.FileMode(0700)
	// Create the proper paths needed for the runc jailer
	j.logger.WithField("rootPath", rootPath).Debug("Creating root drive path")
	if err := mkdirAndChown(rootPath, mode, j.Config.UID, j.Config.GID); err != nil {
		return nil, fmt.Errorf("%s failed to mkdirAndChown: %w", rootPath, err)
	}

	if j.Config.DriveExposePolicy == proto.DriveExposePolicy_BIND {
		err := j.prepareBindMounts(mounts)
		if err != nil {
			return nil, err
		}
	}

	return j, nil
}

func (j *runcJailer) prepareBindMounts(mounts []*proto.FirecrackerDriveMount) error {
	for _, m := range mounts {
		stat := syscall.Stat_t{}
		if err := syscall.Stat(m.HostPath, &stat); err != nil {
			return err
		}
		// Only bindMount regular file.
		// To avoid duplicate files, for block device, we will use system call to create device file for it.
		if stat.Mode&syscall.S_IFMT == syscall.S_IFREG {
			err := j.bindMountFileToJail(m.HostPath, filepath.Join(j.RootPath(), m.HostPath))
			if err != nil {
				return err
			}
		}
	}
	return nil
}

// JailPath returns the base directory from where the jail binary will be ran
// from
func (j *runcJailer) OCIBundlePath() string {
	return j.Config.OCIBundlePath
}

// RootPath returns the root fs of the jailed system.
func (j *runcJailer) RootPath() string {
	return filepath.Join(j.OCIBundlePath(), rootfsFolder)
}

// JailPath will return the OCI bundle rootfs path
func (j *runcJailer) JailPath() vm.Dir {
	return vm.Dir(j.RootPath())
}

// BuildJailedMachine will return the needed options for a jailed Firecracker
// instance. In addition, some configuration values will be overwritten to the
// jailed values, like SocketPath in the machineConfig.
func (j *runcJailer) BuildJailedMachine(cfg *config.Config, machineConfig *firecracker.Config, vmID string) ([]firecracker.Opt, error) {
	handler := j.BuildJailedRootHandler(cfg, machineConfig, vmID)
	fifoHandler := j.BuildLinkFifoHandler()

	var debugSDK bool
	if level, set := cfg.DebugHelper.GetFirecrackerSDKLogLevel(); set {
		debugSDK = level == logrus.DebugLevel
	}
	// Build a new client since BuildJailedRootHandler modifies the socket path value.
	client := firecracker.NewClient(machineConfig.SocketPath, j.logger, debugSDK)

	if machineConfig.NetNS == "" {
		if netns := getNetNS(j.configSpec); netns != "" {
			machineConfig.NetNS = netns
		}
	}

	opts := []firecracker.Opt{
		firecracker.WithProcessRunner(j.jailerCommand(vmID, cfg.DebugHelper.LogFirecrackerOutput())),
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
func (j *runcJailer) BuildJailedRootHandler(cfg *config.Config, machineConfig *firecracker.Config, vmID string) firecracker.Handler {
	ociBundlePath := j.OCIBundlePath()
	rootPath := j.RootPath()
	machineConfig.SocketPath = filepath.Join(rootfsFolder, "api.socket")

	return firecracker.Handler{
		Name: jailerHandlerName,
		Fn: func(ctx context.Context, m *firecracker.Machine) error {

			rootPathToConfig := filepath.Join(ociBundlePath, "config.json")
			j.logger.WithField("rootPathToConfig", rootPathToConfig).Debug("Copying config")
			if err := copyFile(j.Config.RuncConfigPath, rootPathToConfig, 0400); err != nil {
				return fmt.Errorf("failed to copy config from %v to %v: %w", j.Config.RuncConfigPath, rootPathToConfig, err)
			}

			// copy the firecracker binary
			j.logger.WithField("root path", rootPath).Debug("copying firecracker binary")
			newFirecrackerBinPath := filepath.Join(rootPath, firecrackerFileName)
			if err := j.copyFileToJail(cfg.FirecrackerBinaryPath, newFirecrackerBinPath, 0500); err != nil {
				return err
			}

			// copy the kernel image
			newKernelImagePath := filepath.Join(rootPath, kernelImageFileName)
			j.logger.WithField("newKernelImagePath", newKernelImagePath).Debug("copying kernel image")
			if err := j.copyFileToJail(m.Cfg.KernelImagePath, newKernelImagePath, 0400); err != nil {
				return err
			}

			m.Cfg.KernelImagePath = kernelImageFileName

			// copy drives to new contents path
			for i, d := range m.Cfg.Drives {
				drivePath := firecracker.StringValue(d.PathOnHost)
				fileName := filepath.Base(drivePath)
				newDrivePath := filepath.Join(rootPath, fileName)

				f, err := os.Open(drivePath)
				if err != nil {
					return fmt.Errorf("failed to open drive file: %w", err)
				}

				// This closes the file in the event an error occurred, otherwise we
				// call close down below.
				defer f.Close()

				if !internal.IsStubDrive(f) {
					mode := 0600
					if firecracker.BoolValue(d.IsReadOnly) {
						mode = 0400
					}
					if err := j.exposeFileToJail(drivePath, newDrivePath, os.FileMode(mode)); err != nil {
						return err
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

			if err := j.setupCacheTopology(rootPath); err != nil {
				return err
			}

			j.logger.Debugf("Writing %q for runc", rootPathToConfig)
			// we pass m.Cfg as opposed to machineConfig as we want the populated
			// config defaults when calling NewMachine
			if err := j.overwriteConfig(&m.Cfg, filepath.Base(m.Cfg.SocketPath), rootPathToConfig); err != nil {
				return fmt.Errorf("failed to overwrite config.json: %w", err)
			}

			j.logger.Info("Successfully ran jailer handler")
			j.started = true

			return nil
		},
	}
}

// makeLinkInJail creates a hard link to `src` inside the jail directory.
func (j *runcJailer) makeLinkInJail(src, base string) (string, error) {
	root := j.RootPath()

	if strings.ContainsRune(base, os.PathSeparator) {
		return "", fmt.Errorf("%q must not contain %q", base, os.PathSeparator)
	}

	dst := filepath.Join(root, base)

	// Since Firecracker is unaware that we are in a jailed environment and
	// what owner/group to set this as when creating, we will manually have
	// to adjust the permission bits ourselves
	if err := linkAndChown(src, dst, j.Config.UID, j.Config.GID); err != nil {
		return "", err
	}

	// this path needs to be relative to the root path, and since we are
	// placing the file in the root path the value should just be the file name.
	return base, nil
}

// BuildLinkFifoHandler will return a new firecracker.Handler with the function
// that will allow linking of the fifos making them visible to Firecracker.
func (j *runcJailer) BuildLinkFifoHandler() firecracker.Handler {
	return firecracker.Handler{
		Name: jailerFifoHandlerName,
		Fn: func(ctx context.Context, m *firecracker.Machine) error {
			logFifo, err := j.makeLinkInJail(m.Cfg.LogPath, internal.FirecrackerLogFifoName)
			if err != nil {
				return err
			}
			m.Cfg.LogFifo = logFifo

			metricsFifo, err := j.makeLinkInJail(m.Cfg.MetricsPath, internal.FirecrackerMetricsFifoName)
			if err != nil {
				return err
			}
			m.Cfg.MetricsFifo = metricsFifo

			return nil
		},
	}
}

// StubDrivesOptions will return a set of options used to create a new stub
// drive handler.
func (j runcJailer) StubDrivesOptions() []FileOpt {
	return []FileOpt{
		func(file *os.File) error {
			err := unix.Fchown(int(file.Fd()), int(j.Config.UID), int(j.Config.GID))
			if err != nil {
				return fmt.Errorf("failed to chown stub file %q: %w", file.Name(), err)
			}
			return nil
		},
	}
}

// ExposeFileToJail will inspect the given file, srcPath, and based on the
// file type, proper handling will occur to ensure that the file is visible in
// the jail. For block devices we will use mknod to create the device and then
// set the correct permissions to ensure visibility in the jail. Regular files
// will be copied into the jail.
func (j *runcJailer) ExposeFileToJail(srcPath string) error {
	uid := j.Config.UID
	gid := j.Config.GID

	stat := syscall.Stat_t{}
	if err := syscall.Stat(srcPath, &stat); err != nil {
		return err
	}

	// Checks file type using S_IFMT which is the bit mask for the file type.
	switch stat.Mode & syscall.S_IFMT {
	case syscall.S_IFBLK:
		parentDir := filepath.Join(j.RootPath(), filepath.Dir(srcPath))
		if err := mkdirAllWithPermissions(parentDir, 0700, uid, gid); err != nil {
			return err
		}

		dst := filepath.Join(parentDir, filepath.Base(srcPath))
		if err := exposeBlockDeviceToJail(dst, int(stat.Rdev), int(uid), int(gid)); err != nil {
			return err
		}

	case syscall.S_IFREG:
		parentDir := filepath.Join(j.RootPath(), filepath.Dir(srcPath))
		if err := mkdirAllWithPermissions(parentDir, 0700, uid, gid); err != nil {
			return err
		}

		dst := filepath.Join(parentDir, filepath.Base(srcPath))
		if err := j.exposeFileToJail(srcPath, dst, os.FileMode(stat.Mode)); err != nil {
			return err
		}

	default:
		return fmt.Errorf("unsupported mode: %v", stat.Mode)
	}

	return nil
}

// exposeFileToJail will make the file accessible from the jail.
func (j *runcJailer) exposeFileToJail(src, dst string, mode os.FileMode) error {
	if j.Config.DriveExposePolicy == proto.DriveExposePolicy_BIND {
		return j.bindMountFileToJail(src, dst)
	}
	return j.copyFileToJail(src, dst, mode)
}

// copyFileToJail copies a file from src to dst, and chown the new file to the jail user.
func (j *runcJailer) copyFileToJail(src, dst string, mode os.FileMode) error {
	if err := copyFile(src, dst, mode); err != nil {
		return err
	}
	if err := os.Chown(dst, int(j.Config.UID), int(j.Config.GID)); err != nil {
		return err
	}
	return nil
}

// bindMountFileToJail mounts a file from src to dst, and chown the new file to
// the jail user. Note that actual mount is not happening until runc is invoked.
func (j *runcJailer) bindMountFileToJail(src, dst string) error {
	// Once runc has started, the jailer cannot mount any new files.
	if j.started {
		_, err := os.Stat(dst)
		if err != nil {
			return fmt.Errorf("%q must be created by runc: %w", dst, err)
		}
		return nil
	}

	// The directory must be traversable from runc (running as root) and Firecracker (running as j.Config.UID).
	// These two users don't have a shared group. Hence the permission must be 701, not 700.
	err := os.MkdirAll(filepath.Dir(dst), 0701)
	if err != nil {
		return err
	}

	err = os.Chown(filepath.Dir(dst), int(j.Config.UID), int(j.Config.GID))
	if err != nil {
		return err
	}

	f, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer f.Close()

	rel, err := filepath.Rel(j.RootPath(), dst)
	if err != nil {
		return err
	}

	j.configSpec.Mounts = append(j.configSpec.Mounts, specs.Mount{
		Destination: "/" + rel,
		Source:      src,
		Type:        "none",
		Options:     []string{"bind"},
	})

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
	// --sparse=always is a GNU-only option
	output, err := exec.Command("cp", "--sparse=always", src, dst).CombinedOutput()
	if err != nil {
		return fmt.Errorf("failed to cpy %q to %q: %s: %w", src, dst, output, err)
	}
	return os.Chmod(dst, mode)
}

func (j *runcJailer) jailerCommand(containerName string, isDebug bool) *exec.Cmd {
	cmd := exec.CommandContext(j.ctx, j.Config.RuncBinPath, "run", containerName)
	cmd.Dir = j.OCIBundlePath()

	if isDebug {
		cmd.Stdout = j.logger.WithField("vmm_stream", "stdout").WriterLevel(logrus.DebugLevel)
		cmd.Stderr = j.logger.WithField("vmm_stream", "stderr").WriterLevel(logrus.DebugLevel)
	}

	return cmd
}

// overwriteConfig will set the proper default values if a field had not been set.
func (j *runcJailer) overwriteConfig(machineConfig *firecracker.Config, socketPath, configPath string) error {
	spec := j.configSpec
	if spec.Process.User.UID != 0 ||
		spec.Process.User.GID != 0 {
		return fmt.Errorf(
			"using UID %d and GID %d, these values must not be set",
			spec.Process.User.UID,
			spec.Process.User.GID,
		)
	}

	spec = j.setDefaultConfigValues(socketPath, spec)
	spec.Root.Path = rootfsFolder
	spec.Root.Readonly = false
	spec.Process.User.UID = j.Config.UID
	spec.Process.User.GID = j.Config.GID

	if machineConfig.NetNS != "" {
		for i, ns := range spec.Linux.Namespaces {
			if ns.Type == networkNamespaceRuncName {
				ns.Path = machineConfig.NetNS
				spec.Linux.Namespaces[i] = ns
				break
			}
		}
	}

	if spec.Linux.Resources == nil {
		spec.Linux.Resources = &specs.LinuxResources{}
	}

	if spec.Linux.Resources.CPU == nil {
		spec.Linux.Resources.CPU = &specs.LinuxCPU{}
	}

	spec.Linux.Resources.CPU.Cpus = j.Config.CPUs
	spec.Linux.Resources.CPU.Mems = j.Config.Mems

	configBytes, err := json.Marshal(&spec)
	if err != nil {
		return err
	}

	if err := ioutil.WriteFile(configPath, configBytes, 0400); err != nil {
		return err
	}

	return nil
}

func (j runcJailer) CgroupPath() string {
	basePath := "/firecracker-containerd"
	if j.Config.CgroupPath != "" {
		basePath = j.Config.CgroupPath
	}

	return filepath.Join(basePath, j.vmID)
}

// setDefaultConfigValues will override the spec to start Firecracker inside.
func (j *runcJailer) setDefaultConfigValues(socketPath string, spec specs.Spec) specs.Spec {
	if spec.Process == nil {
		spec.Process = &specs.Process{}
	}

	cmd := firecracker.VMCommandBuilder{}.
		WithBin("/" + firecrackerFileName).
		WithSocketPath(socketPath).
		WithArgs([]string{"--id", j.vmID}).
		// Don't need to pass in an actual context here as we are only building
		// the command arguments and not actually building a command
		Build(context.Background())

	spec.Process.Args = cmd.Args

	cgroupPath := j.CgroupPath()
	j.logger.WithField("CgroupPath", cgroupPath).Debug("using cgroup path")
	spec.Linux.CgroupsPath = cgroupPath

	return spec
}

// Close will cleanup the container that may be left behind if the jailing
// process was killed via SIGKILL.
func (j *runcJailer) Close() error {
	// Even the jailer's associated context is cancelled,
	// we'd like to do the cleanups below just in case.
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// Delete the container, if it is still running.
	runcErr := j.runcClient.Delete(ctx, j.vmID, &runc.DeleteOpts{Force: true})

	// Regardless of the result, remove the directory.
	removeErr := os.RemoveAll(j.OCIBundlePath())

	return multierror.Append(runcErr, removeErr).ErrorOrNil()
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

func linkAndChown(src, dst string, uid, gid uint32) error {
	if err := os.Link(src, dst); err != nil {
		return err
	}

	if err := os.Chown(dst, int(uid), int(gid)); err != nil {
		return err
	}

	return nil
}

func getNetNS(spec specs.Spec) string {
	for _, ns := range spec.Linux.Namespaces {
		if ns.Type == networkNamespaceRuncName {
			return ns.Path
		}
	}

	return ""
}

func (j runcJailer) Stop(force bool) error {
	signal := syscall.SIGTERM
	if force {
		signal = syscall.SIGKILL
	}
	return j.runcClient.Kill(j.ctx, j.vmID, int(signal), &runc.KillOpts{All: true})
}

// setupCacheTopology will copy indexed contents from the cacheTopologyPath to
// the jailer. This is needed for arm architecture as arm does not
// automatically setup any cache topology
func (j runcJailer) setupCacheTopology(path string) error {
	j.logger.WithField("path", path).Debug("Creating cache topology")
	const mode = os.FileMode(0700)

	// builds the cache topology path from the root directory
	for _, p := range cacheTopologyPaths {
		path = filepath.Join(path, p)
		if err := mkdirAndChown(path, mode, j.Config.UID, j.Config.GID); err != nil {
			return err
		}
	}

	err := filepath.Walk(cacheTopologyPath, func(cachePath string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if !info.IsDir() {
			return nil
		}

		folder := filepath.Base(cachePath)
		if !strings.HasPrefix(folder, cacheFolderPrefix) {
			return nil
		}

		indexPath := filepath.Join(path, folder)
		if err := mkdirAndChown(indexPath, info.Mode(), j.Config.UID, j.Config.GID); err != nil {
			return err
		}

		j.logger.WithField("src path", cachePath).WithField("dst path", indexPath).Debug("copying cache folder")
		for _, file := range cacheTopologyFiles {
			cacheFilePath := filepath.Join(cachePath, file)
			info, err := os.Stat(cacheFilePath)
			if err != nil {
				return err
			}

			// This is suppose to be a hard copy as intructed by the Firecracker team.
			// Bind mounting here may cause some issues on the host's machine
			if err := j.copyFileToJail(cacheFilePath, filepath.Join(indexPath, file), info.Mode()); err != nil {
				return err
			}
		}

		return nil
	})

	return err
}
