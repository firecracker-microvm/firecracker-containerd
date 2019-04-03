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

	"github.com/containerd/containerd/log"
	"github.com/containerd/containerd/plugin"
	"github.com/golang/protobuf/ptypes/empty"
	"google.golang.org/grpc"

	"github.com/firecracker-microvm/firecracker-containerd/proto"
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

			return &service{local: instance.(proto.FirecrackerServer)}, nil
		},
	})
}

type service struct {
	local proto.FirecrackerServer
}

var _ proto.FirecrackerServer = (*service)(nil)

func (s *service) Register(server *grpc.Server) error {
	proto.RegisterFirecrackerServer(server, s)
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

func (s *service) GetVMAddress(ctx context.Context, req *proto.GetVMAddressRequest) (*proto.GetVMAddressResponse, error) {
	log.G(ctx).Debugf("get VM address: %+v", req)
	return s.local.GetVMAddress(ctx, req)
}
