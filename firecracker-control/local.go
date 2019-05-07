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
	"os"
	"path/filepath"
	"sync"

	"github.com/containerd/containerd/events"
	"github.com/containerd/containerd/log"
	"github.com/containerd/containerd/namespaces"
	"github.com/containerd/containerd/plugin"
	"github.com/firecracker-microvm/firecracker-go-sdk"
	"github.com/golang/protobuf/ptypes/empty"
	"github.com/pkg/errors"
	"github.com/sirupsen/logrus"

	"github.com/firecracker-microvm/firecracker-containerd/proto"
	fccontrol "github.com/firecracker-microvm/firecracker-containerd/proto/service/fccontrol/grpc"
)

var (
	_ fccontrol.FirecrackerServer = (*local)(nil)
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

const (
	startEventName = "/firecracker-vm/start"
	stopEventName  = "/firecracker-vm/stop"
)

// Wrap interface in order to properly generate mock with mockgen
type machine interface {
	firecracker.MachineIface
}

type publisher interface {
	events.Publisher
}

type instance struct {
	cfg     *firecracker.Config
	machine machine
}

type local struct {
	vm          map[string]*instance
	vmLock      sync.RWMutex
	rootPath    string
	publisher   publisher
	findVsockFn func(context.Context) (*os.File, uint32, error)
}

func newLocal(ic *plugin.InitContext) (*local, error) {
	if err := os.MkdirAll(ic.Root, 0750); err != nil && !os.IsExist(err) {
		return nil, errors.Wrapf(err, "failed to create root directory: %s", ic.Root)
	}

	return &local{
		vm:          make(map[string]*instance),
		rootPath:    ic.Root,
		publisher:   ic.Events,
		findVsockFn: findNextAvailableVsockCID,
	}, nil
}

// CreateVM creates new Firecracker VM instance
func (s *local) CreateVM(ctx context.Context, req *proto.CreateVMRequest) (*empty.Empty, error) {
	id := req.GetVMID()
	if id == "" {
		return nil, errors.New("invalid VM ID")
	}

	s.vmLock.Lock()

	// Make sure VM ID unique
	if _, ok := s.vm[id]; ok {
		s.vmLock.Unlock()
		return nil, errors.Errorf("VM with id %q already exists", id)
	}

	// Reserve this ID while we're creating an instance in order to avoid race conditions
	s.vm[id] = &instance{}
	s.vmLock.Unlock()

	instance, err := s.newInstance(ctx, id, req)

	s.vmLock.Lock()
	defer s.vmLock.Unlock()

	if err != nil {
		log.G(ctx).WithError(err).WithField("vm_id", id).Error("failed to create VM")
		delete(s.vm, id)

		return nil, err
	}

	s.vm[id] = instance
	return &empty.Empty{}, nil
}

func (s *local) newInstance(ctx context.Context, id string, req *proto.CreateVMRequest) (*instance, error) {
	logger := log.G(ctx).WithField("vm_id", id)

	vsockDescriptor, cid, err := s.findVsockFn(ctx)
	if err != nil {
		return nil, errors.Wrap(err, "failed to find available cid for vsock")
	}

	defer vsockDescriptor.Close()

	cfg, err := s.buildVMConfiguration(ctx, id, cid, req)
	if err != nil {
		return nil, err
	}

	logger.Info("creating new VM")

	machine, err := firecracker.NewMachine(ctx, *cfg, firecracker.WithLogger(logger))
	if err != nil {
		logger.WithError(err).Error("failed to create new machine instance")
		return nil, err
	}

	// Release vsock CID so Firecracker instance can reacquire it.
	vsockDescriptor.Close()

	if err := s.startMachine(ctx, id, machine); err != nil {
		logger.WithError(err).Error("failed to start VM instance")
		return nil, err
	}

	logger.Info("successfully started the VM")

	newInstance := &instance{
		cfg:     cfg,
		machine: machine,
	}

	return newInstance, nil
}

func (s *local) buildVMConfiguration(ctx context.Context, id string, cid uint32, req *proto.CreateVMRequest) (*firecracker.Config, error) {
	logger := log.G(ctx).WithFields(logrus.Fields{
		"vm_id":  id,
		"vm_cid": cid,
	})

	cfg := &firecracker.Config{
		SocketPath:   filepath.Join(s.rootPath, fmt.Sprintf("vm_%s.socket", id)),
		LogFifo:      filepath.Join(s.rootPath, fmt.Sprintf("vm_%s_log.fifo", id)),
		MetricsFifo:  filepath.Join(s.rootPath, fmt.Sprintf("vm_%s_metrics.fifo", id)),
		VsockDevices: []firecracker.VsockDevice{{Path: "root", CID: cid}},
		MachineCfg:   machineConfigurationFromProto(req.GetMachineCfg()),
	}

	logger.Debugf("using socket path: %s", cfg.SocketPath)

	// Kernel configuration

	if val := req.GetKernelArgs(); val != "" {
		cfg.KernelArgs = val
	} else {
		cfg.KernelArgs = defaultKernelArgs
	}

	if val := req.GetKernelImagePath(); val != "" {
		cfg.KernelImagePath = val
	} else {
		cfg.KernelImagePath = defaultKernelPath
	}

	// Drives configuration

	var driveBuilder firecracker.DrivesBuilder
	if root := req.GetRootDrive(); root != nil {
		driveBuilder = firecracker.NewDrivesBuilder(root.GetPathOnHost())
	} else {
		driveBuilder = firecracker.NewDrivesBuilder(defaultRootfsPath)
	}

	for _, drive := range req.GetAdditionalDrives() {
		driveBuilder = addDriveFromProto(driveBuilder, drive)
	}

	// TODO: Reserve fake drives here (https://github.com/firecracker-microvm/firecracker-containerd/pull/154)

	cfg.Drives = driveBuilder.Build()

	// Setup network interfaces

	for _, ni := range req.GetNetworkInterfaces() {
		cfg.NetworkInterfaces = append(cfg.NetworkInterfaces, networkConfigFromProto(ni))
	}

	return cfg, nil
}

// startMachine attempts to start an instance within timeout and runs a goroutine to monitor VM exit event
func (s *local) startMachine(ctx context.Context, id string, machine machine) error {
	logger := log.G(ctx).WithField("vm_id", id)

	logger.Info("starting VM instance")

	ctx, cancel := context.WithTimeout(ctx, firecrackerStartTimeout)
	defer cancel()

	ns, err := namespaces.NamespaceRequired(ctx)
	if err != nil {
		return err
	}

	logger.Debugf("using namespace: %s", ns)

	if err := machine.Start(ctx); err != nil {
		logger.WithError(err).Error("failed to start the VM")
		return err
	}

	if err := s.publisher.Publish(ctx, startEventName, &proto.VMStart{VMID: id}); err != nil {
		logger.WithError(err).Error("failed to publish start event")
		return err
	}

	go func() {
		ctx := namespaces.WithNamespace(context.Background(), ns)

		if err := machine.Wait(ctx); err != nil {
			logger.WithError(err).Error("failed to wait the VM")
		}

		if err := s.publisher.Publish(ctx, stopEventName, &proto.VMStop{VMID: id}); err != nil {
			logger.WithError(err).Error("failed to publish stop event")
		}

		logger.Debug("removing machine")

		s.vmLock.Lock()
		delete(s.vm, id)
		s.vmLock.Unlock()
	}()

	return nil
}

// StopVM stops running VM instance by VM ID
func (s *local) StopVM(ctx context.Context, req *proto.StopVMRequest) (*empty.Empty, error) {
	logger := log.G(ctx).WithField("vm_id", req.GetVMID())

	instance, err := s.getVM(req.GetVMID())
	if err != nil {
		return nil, err
	}

	logger.Infof("stopping the VM")

	if err := instance.machine.StopVMM(); err != nil {
		logger.WithError(err).Error("failed to stop VM")
		return nil, err
	}

	logger.Info("successfully stopped the VM")
	return &empty.Empty{}, nil
}

func (s *local) GetVMInfo(ctx context.Context, req *proto.GetVMInfoRequest) (*proto.GetVMInfoResponse, error) {
	id := req.GetVMID()
	instance, err := s.getVM(id)
	if err != nil {
		return nil, err
	}

	var cid uint32
	if len(instance.cfg.VsockDevices) > 0 {
		cid = instance.cfg.VsockDevices[0].CID
	}

	resp := &proto.GetVMInfoResponse{
		VMID:            id,
		ContextID:       cid,
		SocketPath:      instance.cfg.SocketPath,
		LogFifoPath:     instance.cfg.LogFifo,
		MetricsFifoPath: instance.cfg.MetricsFifo,
	}

	return resp, nil
}

// SetVMMetadata sets Firecracker instance metadata
func (s *local) SetVMMetadata(ctx context.Context, req *proto.SetVMMetadataRequest) (*empty.Empty, error) {
	var (
		id       = req.GetVMID()
		metadata = req.GetMetadata()
	)

	instance, err := s.getVM(id)
	if err != nil {
		return nil, err
	}

	log.G(ctx).WithField("vm_id", id).Info("updating VM metadata")
	if err := instance.machine.SetMetadata(ctx, metadata); err != nil {
		return nil, errors.Wrapf(err, "failed to set metadata for VM %q", id)
	}

	return &empty.Empty{}, nil
}

func (s *local) getVM(id string) (*instance, error) {
	s.vmLock.RLock()
	instance, ok := s.vm[id]
	s.vmLock.RUnlock()

	if !ok {
		return nil, ErrVMNotFound
	}

	return instance, nil
}
