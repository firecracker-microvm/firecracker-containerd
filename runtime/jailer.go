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

	"github.com/firecracker-microvm/firecracker-go-sdk"
	"github.com/sirupsen/logrus"

	"github.com/firecracker-microvm/firecracker-containerd/internal/vm"
	"github.com/firecracker-microvm/firecracker-containerd/proto"
)

const (
	kernelImageFileName   = "kernel-image"
	jailerHandlerName     = "firecracker-containerd-jail-handler"
	jailerFifoHandlerName = "firecracker-containerd-jail-fifo-handler"
	rootfsFolder          = "rootfs"

	// TODO evenetually we can get rid of this when we add usernamespaces to
	// jailing.
	jailerUID = 300000
	jailerGID = 300000
)

var (
	runcConfigPath = "/etc/containerd/firecracker-runc-config.json"
)

// jailer will allow modification and provide options to the the Firecracker VM
// to allow for jailing. In addition, this will allow for given files to be exposed
// to the jailed filesystem.
type jailer interface {
	// BuildJailedMachine will modify the firecracker.Config and provide
	// firecracker.Opt to be passed into firecracker.NewMachine which will allow
	// for the VM to be jailed.
	BuildJailedMachine(cfg *Config, machineCfg *firecracker.Config, vmID string) ([]firecracker.Opt, error)
	// ExposeDeviceToJail will expose the given device provided by the snapshotter
	// to the jailed filesystem
	ExposeDeviceToJail(path string) error
	// JailPath is used to return the directory we are supposed to be working in.
	JailPath() vm.Dir
	// StubDrivesOptions will return a set of options used to create a new stub
	// drive handler.
	StubDrivesOptions() []stubDrivesOpt
}

// newJailer is used to construct a jailer from the CreateVM request. If no
// request or jailer config was provided, then the noopJailer will be returned.
func newJailer(
	ctx context.Context,
	logger *logrus.Entry,
	ociBundlePath string,
	service *service,
	request *proto.CreateVMRequest,
) (jailer, error) {
	if request == nil || request.JailerConfig == nil {
		l := logger.WithField("jailer", "noop")
		return newNoopJailer(ctx, l, service.shimDir), nil
	}

	l := logger.WithField("jailer", "runc")
	return newRuncJailer(ctx, l, ociBundlePath, service.config.JailerConfig.RuncBinaryPath, jailerUID, jailerGID)
}
