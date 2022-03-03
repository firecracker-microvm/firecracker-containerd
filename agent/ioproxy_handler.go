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

	"github.com/containerd/containerd/log"
	"github.com/containerd/containerd/runtime/v2/task"
	"github.com/firecracker-microvm/firecracker-containerd/internal/vm"
	ioproxy "github.com/firecracker-microvm/firecracker-containerd/proto/service/ioproxy/ttrpc"
	"github.com/gogo/protobuf/types"
)

// ioProxyHandler implements IOProxyService that exposes the state of
// IOProxy instances.
type ioProxyHandler struct {
	runcService task.TaskService
	taskManager vm.TaskManager
}

var _ ioproxy.IOProxyService = &ioProxyHandler{}

// State returns whether the given exec's IOProxy is still open or not.
func (ps *ioProxyHandler) State(_ context.Context, req *ioproxy.StateRequest) (*ioproxy.StateResponse, error) {
	open, err := ps.taskManager.IsProxyOpen(req.ID, req.ExecID)
	if err != nil {
		return nil, err
	}
	return &ioproxy.StateResponse{IsOpen: open}, nil
}

// Attach a new IOProxy to the given exec.
func (ps *ioProxyHandler) Attach(ctx context.Context, req *ioproxy.AttachRequest) (*types.Empty, error) {
	state, err := ps.runcService.State(ctx, &task.StateRequest{ID: req.ID, ExecID: req.ExecID})
	if err != nil {
		return nil, err
	}

	logger := log.G(ctx).WithField("TaskID", req.ID).WithField("ExecID", req.ExecID)

	var proxy vm.IOProxy
	if vm.IsAgentOnlyIO(state.Stdout, logger) {
		proxy = vm.NewNullIOProxy()
	} else {
		proxy = vm.NewIOConnectorProxy(
			vm.InputPair(req.StdinPort, state.Stdin),
			vm.OutputPair(state.Stdout, req.StdoutPort),
			vm.OutputPair(state.Stderr, req.StderrPort),
		)
	}

	err = ps.taskManager.AttachIO(ctx, req.ID, req.ExecID, proxy)
	if err != nil {
		return nil, err
	}

	return &types.Empty{}, nil
}
