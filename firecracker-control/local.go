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
	"errors"

	"github.com/containerd/containerd/log"
	"github.com/containerd/containerd/plugin"
	"github.com/golang/protobuf/ptypes/empty"
	"golang.org/x/net/context"

	"github.com/firecracker-microvm/firecracker-containerd/proto"
)

const (
	localPluginID = "fc-control"
	grpcPluginID  = "fc-control-service"
)

var (
	_ proto.FirecrackerServer = (*local)(nil)
)

func init() {
	plugin.Register(&plugin.Registration{
		Type: plugin.ServicePlugin,
		ID:   localPluginID,
		InitFn: func(ic *plugin.InitContext) (interface{}, error) {
			log.G(ic.Context).Debugf("initializing %s plugin", localPluginID)
			return &local{}, nil
		},
	})
}

type local struct{}

func (s *local) CreateVM(context.Context, *proto.CreateVMRequest) (*proto.CreateVMResponse, error) {
	return nil, errors.New("not implemented")
}

func (s *local) StopVM(context.Context, *proto.StopVMRequest) (*empty.Empty, error) {
	return nil, errors.New("not implemented")
}

func (s *local) GetVMAddress(context.Context, *proto.GetVMAddressRequest) (*proto.GetVMAddressResponse, error) {
	return nil, errors.New("not implemented")
}
