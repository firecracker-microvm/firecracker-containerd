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
	"errors"

	"github.com/containerd/containerd/log"
	"github.com/containerd/containerd/plugin"
	"github.com/golang/protobuf/ptypes/empty"
	"google.golang.org/grpc"

	"github.com/firecracker-microvm/firecracker-containerd/proto"
)

func init() {
	plugin.Register(&plugin.Registration{
		Type: plugin.GRPCPlugin,
		ID:   "fc-control",
		InitFn: func(ic *plugin.InitContext) (interface{}, error) {
			log.G(ic.Context).Debug("initializing fc-control plugin")
			return &service{}, nil
		},
	})
}

type service struct {
}

var _ proto.FirecrackerServer = (*service)(nil)

func (s *service) Register(server *grpc.Server) error {
	proto.RegisterFirecrackerServer(server, s)
	return nil
}

func (s *service) CreateVM(ctx context.Context, req *proto.CreateVMRequest) (*proto.CreateVMResponse, error) {
	log.G(ctx).Debug("create VM request: %+v", req)
	return nil, errors.New("not implemented")
}

func (s *service) StopVM(ctx context.Context, req *proto.StopVMRequest) (*empty.Empty, error) {
	log.G(ctx).Debug("stop VM: %+v", req)
	return nil, errors.New("not implemented")
}

func (s *service) GetVMAddress(ctx context.Context, req *proto.GetVMAddressRequest) (*proto.GetVMAddressResponse, error) {
	log.G(ctx).Debug("get VM address: %+v", req)
	return nil, errors.New("not implemented")
}
