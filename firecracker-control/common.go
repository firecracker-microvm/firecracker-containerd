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
	"time"

	models "github.com/firecracker-microvm/firecracker-go-sdk/client/models"
	"github.com/pkg/errors"
)

const (
	localPluginID = "fc-control"
	grpcPluginID  = "fc-control-service"
)

var (
	// ErrVMNotFound means that a VM with the given ID not found
	ErrVMNotFound = errors.New("vm not found")
)

const (
	firecrackerStartTimeout = 5 * time.Second
	defaultCPUTemplate      = models.CPUTemplateT2
	defaultCPUCount         = 1
	defaultMemSizeMb        = 128
	defaultKernelArgs       = "console=ttyS0 noapic reboot=k panic=1 pci=off nomodules rw"
	defaultFilesPath        = "/var/lib/firecracker-containerd/runtime/"
	defaultKernelPath       = defaultFilesPath + "default-vmlinux.bin"
	defaultRootfsPath       = defaultFilesPath + "default-rootfs.img"
)
