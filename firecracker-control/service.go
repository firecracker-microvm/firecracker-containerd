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

package service

import (
	"context"
	"fmt"

	"github.com/containerd/containerd/log"
	"github.com/containerd/containerd/plugin"
	"github.com/containerd/ttrpc"
	"github.com/golang/protobuf/ptypes/empty"

	"github.com/firecracker-microvm/firecracker-containerd/proto"
	fccontrol "github.com/firecracker-microvm/firecracker-containerd/proto/service/fccontrol/ttrpc"
)

func init() {
	plugin.Register(&plugin.Registration{
		Type: plugin.GRPCPlugin,
		ID:   grpcPluginID,
		Requires: []plugin.Type{
			plugin.ServicePlugin,
		},
		InitFn: func(ic *plugin.InitContext) (interface{}, error) {
			log.G(ic.Context).Debugf("initializing %s plugin", grpcPluginID)

			list, err := ic.GetByType(plugin.ServicePlugin)
			if err != nil {
				return nil, err
			}

			item, ok := list[localPluginID]
			if !ok {
				return nil, fmt.Errorf("service %q not found", localPluginID)
			}

			instance, err := item.Instance()
			if err != nil {
				return nil, err
			}

			return &service{local: instance.(fccontrol.FirecrackerService)}, nil
		},
	})
}

type service struct {
	local fccontrol.FirecrackerService
}

var _ fccontrol.FirecrackerService = (*service)(nil)

func (s *service) RegisterTTRPC(server *ttrpc.Server) error {
	fccontrol.RegisterFirecrackerService(server, s)
	return nil
}

func (s *service) CreateVM(ctx context.Context, req *proto.CreateVMRequest) (*proto.CreateVMResponse, error) {
	log.G(ctx).Debugf("create VM request: %+v", req)
	return s.local.CreateVM(ctx, req)
}

func (s *service) StopVM(ctx context.Context, req *proto.StopVMRequest) (*empty.Empty, error) {
	log.G(ctx).Debugf("stop VM: %+v", req)
	return s.local.StopVM(ctx, req)
}

func (s *service) GetVMInfo(ctx context.Context, req *proto.GetVMInfoRequest) (*proto.GetVMInfoResponse, error) {
	log.G(ctx).Debugf("get VM info: %+v", req)
	return s.local.GetVMInfo(ctx, req)
}

func (s *service) SetVMMetadata(ctx context.Context, req *proto.SetVMMetadataRequest) (*empty.Empty, error) {
	log.G(ctx).Debug("Setting vm metadata")
	return s.local.SetVMMetadata(ctx, req)
}

func (s *service) UpdateVMMetadata(ctx context.Context, req *proto.UpdateVMMetadataRequest) (*empty.Empty, error) {
	log.G(ctx).Debug("Updating vm metadata")
	return s.local.UpdateVMMetadata(ctx, req)
}

func (s *service) GetVMMetadata(ctx context.Context, req *proto.GetVMMetadataRequest) (*proto.GetVMMetadataResponse, error) {
	log.G(ctx).Debug("Getting vm metadata")
	return s.local.GetVMMetadata(ctx, req)
}

func (s *service) PauseVM(ctx context.Context, req *proto.PauseVMRequest) (*empty.Empty, error) {
	log.G(ctx).Debugf("pause VM request: %+v", req)
	return s.local.PauseVM(ctx, req)
}

func (s *service) ResumeVM(ctx context.Context, req *proto.ResumeVMRequest) (*empty.Empty, error) {
	log.G(ctx).Debugf("resume VM request: %+v", req)
	return s.local.ResumeVM(ctx, req)
}

func (s *service) LoadSnapshot(ctx context.Context, req *proto.LoadSnapshotRequest) (*empty.Empty, error) {
	log.G(ctx).Debugf("load snapshot request: %+v", req)
	return s.local.LoadSnapshot(ctx, req)
}

func (s *service) CreateSnapshot(ctx context.Context, req *proto.CreateSnapshotRequest) (*empty.Empty, error) {
	log.G(ctx).Debugf("create snapshot request: %+v", req)
	return s.local.CreateSnapshot(ctx, req)
}

func (s *service) Offload(ctx context.Context, req *proto.OffloadRequest) (*empty.Empty, error) {
	log.G(ctx).Debugf("offload request: %+v", req)
	return s.local.Offload(ctx, req)
}
