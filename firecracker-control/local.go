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

package service

import (
	"context"
	"fmt"
	"net"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"syscall"

	"github.com/containerd/containerd/identifiers"
	"github.com/containerd/containerd/log"
	"github.com/containerd/containerd/namespaces"
	"github.com/containerd/containerd/plugin"
	"github.com/containerd/containerd/runtime/v2/shim"
	"github.com/containerd/containerd/sys"
	"github.com/golang/protobuf/ptypes/empty"
	"github.com/pkg/errors"
	"github.com/sirupsen/logrus"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	fcclient "github.com/firecracker-microvm/firecracker-containerd/firecracker-control/client"
	"github.com/firecracker-microvm/firecracker-containerd/internal"
	fcShim "github.com/firecracker-microvm/firecracker-containerd/internal/shim"
	"github.com/firecracker-microvm/firecracker-containerd/internal/vm"
	"github.com/firecracker-microvm/firecracker-containerd/proto"
	fccontrolTtrpc "github.com/firecracker-microvm/firecracker-containerd/proto/service/fccontrol/ttrpc"
)

var (
	_ fccontrolTtrpc.FirecrackerService = (*local)(nil)
)

func init() {
	plugin.Register(&plugin.Registration{
		Type: plugin.ServicePlugin,
		ID:   localPluginID,
		InitFn: func(ic *plugin.InitContext) (interface{}, error) {
			log.G(ic.Context).Debugf("initializing %s plugin (root: %q)", localPluginID, ic.Root)
			return newLocal(ic)
		},
	})
}

type local struct {
	containerdAddress string
	logger            *logrus.Entry
}

func newLocal(ic *plugin.InitContext) (*local, error) {
	if err := os.MkdirAll(ic.Root, 0750); err != nil && !os.IsExist(err) {
		return nil, errors.Wrapf(err, "failed to create root directory: %s", ic.Root)
	}

	return &local{
		containerdAddress: ic.Address,
		logger:            log.G(ic.Context),
	}, nil
}

// CreateVM creates new Firecracker VM instance. It creates a runtime shim for the VM and the forwards
// the CreateVM request to that shim. If there is already a VM created with the provided VMID, then
// AlreadyExists is returned.
func (s *local) CreateVM(requestCtx context.Context, req *proto.CreateVMRequest) (*empty.Empty, error) {
	var err error

	id := req.GetVMID()
	if err := identifiers.Validate(id); err != nil {
		s.logger.WithError(err).Error()
		return nil, err
	}

	ns, err := namespaces.NamespaceRequired(requestCtx)
	if err != nil {
		err = errors.Wrap(err, "error retrieving namespace of request")
		s.logger.WithError(err).Error()
		return nil, err
	}

	s.logger.Debugf("using namespace: %s", ns)

	// We determine if there is already a shim managing a VM with the current VMID by attempting
	// to listen on the abstract socket address (which is parameterized by VMID). If we get
	// EADDRINUSE, then we assume there is already a shim for the VM and return an AlreadyExists error.
	shimSocketAddress, err := fcShim.SocketAddress(requestCtx, id)
	if err != nil {
		err = errors.Wrap(err, "failed to obtain shim socket address")
		s.logger.WithError(err).Error()
		return nil, err
	}

	shimSocket, err := shim.NewSocket(shimSocketAddress)
	if isEADDRINUSE(err) {
		return nil, status.Errorf(codes.AlreadyExists, "VM with ID %q already exists", id)
	} else if err != nil {
		err = errors.Wrapf(err, "failed to open shim socket at address %q", shimSocketAddress)
		s.logger.WithError(err).Error()
		return nil, err
	}

	// If we're here, there is no pre-existing shim for this VMID, so we spawn a new one
	defer shimSocket.Close()

	shimDir, err := vm.ShimDir(ns, id)
	if err != nil {
		err = errors.Wrapf(err, "failed to build shim path")
		s.logger.WithError(err).Error()
		return nil, err
	}

	err = shimDir.Mkdir()
	if err != nil {
		err = errors.Wrapf(err, "failed to create VM dir %q", shimDir.RootPath())
		s.logger.WithError(err).Error()
		return nil, err
	}

	defer func() {
		if err != nil {
			removeErr := os.RemoveAll(shimDir.RootPath())
			if removeErr != nil {
				s.logger.WithError(removeErr).WithField("path", shimDir.RootPath()).Error("failed to cleanup VM dir")
			}
		}
	}()

	// TODO we have to create separate listeners for the fccontrol service and shim service because
	// containerd does not currently expose the shim server for us to register the fccontrol service with too.
	// This is likely addressable through some relatively small upstream contributions; the following is a stop-gap
	// solution until that time.
	fcSocketAddress, err := fcShim.FCControlSocketAddress(requestCtx, id)
	if err != nil {
		err = errors.Wrap(err, "failed to obtain shim socket address")
		s.logger.WithError(err).Error()
		return nil, err
	}

	fcSocket, err := shim.NewSocket(fcSocketAddress)
	if err != nil {
		err = errors.Wrapf(err, "failed to open fccontrol socket at address %q", fcSocketAddress)
		s.logger.WithError(err).Error()
		return nil, err
	}

	defer fcSocket.Close()

	cmd, err := s.newShim(ns, id, s.containerdAddress, shimSocket, fcSocket)
	if err != nil {
		return nil, err
	}

	defer func() {
		if err != nil {
			cmd.Process.Kill()
		}
	}()

	client, err := s.shimFirecrackerClient(requestCtx, id)
	if err != nil {
		err = errors.Wrap(err, "failed to create firecracker shim client")
		s.logger.WithError(err).Error()
		return nil, err
	}

	defer client.Close()

	resp, err := client.CreateVM(requestCtx, req)
	if err != nil {
		err = errors.Wrap(err, "shim CreateVM returned error")
		s.logger.WithError(err).Error()
		return nil, err
	}

	return resp, nil
}

func (s *local) shimFirecrackerClient(requestCtx context.Context, vmID string) (*fcclient.Client, error) {
	if err := identifiers.Validate(vmID); err != nil {
		return nil, errors.Wrap(err, "invalid id")
	}

	socketAddr, err := fcShim.FCControlSocketAddress(requestCtx, vmID)
	if err != nil {
		err = errors.Wrap(err, "failed to get shim's fccontrol socket address")
		s.logger.WithError(err).Error()
		return nil, err
	}

	return fcclient.New("\x00" + socketAddr)
}

// StopVM stops running VM instance by VM ID. This stops the VM, all tasks within the VM and the runtime shim
// managing the VM.
func (s *local) StopVM(requestCtx context.Context, req *proto.StopVMRequest) (*empty.Empty, error) {
	client, err := s.shimFirecrackerClient(requestCtx, req.VMID)
	if err != nil {
		return nil, err
	}

	defer client.Close()

	resp, err := client.StopVM(requestCtx, req)
	if err != nil {
		err = errors.Wrap(err, "shim client failed to stop VM")
		s.logger.WithError(err).Error()
		return nil, err
	}

	return resp, err
}

// GetVMInfo returns metadata for the VM with the given VMID.
func (s *local) GetVMInfo(requestCtx context.Context, req *proto.GetVMInfoRequest) (*proto.GetVMInfoResponse, error) {
	client, err := s.shimFirecrackerClient(requestCtx, req.VMID)
	if err != nil {
		return nil, err
	}

	defer client.Close()

	resp, err := client.GetVMInfo(requestCtx, req)
	if err != nil {
		err = errors.Wrap(err, "shim client failed to get vm info")
		s.logger.WithError(err).Error()
		return nil, err
	}

	return resp, nil
}

// SetVMMetadata sets Firecracker instance metadata for the VM with the given VMID.
func (s *local) SetVMMetadata(requestCtx context.Context, req *proto.SetVMMetadataRequest) (*empty.Empty, error) {
	client, err := s.shimFirecrackerClient(requestCtx, req.VMID)
	if err != nil {
		return nil, err
	}

	defer client.Close()

	resp, err := client.SetVMMetadata(requestCtx, req)
	if err != nil {
		err = errors.Wrap(err, "shim client failed to set vm info")
		s.logger.WithError(err).Error()
		return nil, err
	}

	return resp, nil
}

func (s *local) newShim(ns, vmID, containerdAddress string, shimSocket *net.UnixListener, fcSocket *net.UnixListener) (*exec.Cmd, error) {
	logger := s.logger.WithField("vmID", vmID)

	args := []string{
		"-namespace", ns,
		"-address", containerdAddress,
	}

	cmd := exec.Command(internal.ShimBinaryName, args...)

	shimDir, err := vm.ShimDir(ns, vmID)
	if err != nil {
		err = errors.Wrap(err, "failed to create shim dir")
		logger.WithError(err).Error()
		return nil, err
	}

	// note: The working dir of the shim has an effect on the length of the path
	// needed to specify various unix sockets that the shim uses to communicate
	// with the firecracker VMM and guest agent within. The length of that path
	// has a relatively low limit (usually 108 chars), so modifying the working
	// dir should be done with caution. See internal/vm/dir.go for the path
	// definitions.
	cmd.Dir = shimDir.RootPath()

	shimSocketFile, err := shimSocket.File()
	if err != nil {
		err = errors.Wrap(err, "failed to get shim socket fd")
		logger.WithError(err).Error()
		return nil, err
	}

	fcSocketFile, err := fcSocket.File()
	if err != nil {
		err = errors.Wrap(err, "failed to get shim fccontrol socket fd")
		logger.WithError(err).Error()
		return nil, err
	}

	cmd.ExtraFiles = append(cmd.ExtraFiles, shimSocketFile, fcSocketFile)
	fcSocketFDNum := 2 + len(cmd.ExtraFiles) // "2 +" because ExtraFiles come after stderr (fd #2)

	cmd.Env = append(os.Environ(),
		fmt.Sprintf("%s=%s", internal.VMIDEnvVarKey, vmID),
		fmt.Sprintf("%s=%s", internal.FCSocketFDEnvKey, strconv.Itoa(fcSocketFDNum))) // TODO remove after containerd is updated to expose ttrpc server to shim

	cmd.SysProcAttr = &syscall.SysProcAttr{
		Setpgid: true,
	}

	// shim stderr is just raw text, so pass it through our logrus formatter first
	cmd.Stderr = logger.WithField("shim_stream", "stderr").WriterLevel(logrus.ErrorLevel)
	// shim stdout on the other hand is already formatted by logrus, so pass that transparently through to containerd logs
	cmd.Stdout = logger.Logger.Out

	err = cmd.Start()
	if err != nil {
		err = errors.Wrap(err, "failed to start shim child process")
		logger.WithError(err).Error()
		return nil, err
	}

	// make sure to wait after start
	go func() {
		logger.Debug("waiting on shim process")
		waitErr := cmd.Wait()
		logger.WithError(waitErr).Debug("completed waiting on shim process")
	}()

	err = setShimOOMScore(cmd.Process.Pid)
	if err != nil {
		logger.WithError(err).Error()
		return nil, err
	}

	return cmd, nil
}

func isEADDRINUSE(err error) bool {
	return err != nil && strings.Contains(err.Error(), "address already in use")
}

func setShimOOMScore(shimPid int) error {
	containerdPid := os.Getpid()

	score, err := sys.GetOOMScoreAdj(containerdPid)
	if err != nil {
		return errors.Wrap(err, "failed to get OOM score for containerd")
	}

	shimScore := score + 1
	if err := sys.SetOOMScore(shimPid, shimScore); err != nil {
		return errors.Wrap(err, "failed to set OOM score on shim")
	}

	return nil
}
