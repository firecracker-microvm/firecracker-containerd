// Copyright 2018 Amazon.com, Inc. or its affiliates. All Rights Reserved.
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
	"io/ioutil"
	"path/filepath"

	"github.com/containerd/containerd/log"
	"github.com/containerd/containerd/namespaces"
	"github.com/containerd/containerd/runtime/v2/shim"
	shimapi "github.com/containerd/containerd/runtime/v2/task"
	"github.com/firecracker-microvm/firecracker-containerd/proto"
	"github.com/gogo/protobuf/types"
	"github.com/sirupsen/logrus"
)

const (
	defaultNamespace = "default"
	bundleMountPath  = "/container"
)

// TaskService represents inner shim wrapper over runc in order to:
// - Add default namespace to ctx as it's not passed by ttrpc over vsock
// - Add debug logging to simplify debugging
// - Make place for future extensions as needed
type TaskService struct {
	runc shim.Shim
}

func NewTaskService(runc shim.Shim) shim.Shim {
	return &TaskService{runc: runc}
}

func (ts *TaskService) Create(ctx context.Context, req *shimapi.CreateTaskRequest) (*shimapi.CreateTaskResponse, error) {
	log.G(ctx).WithFields(logrus.Fields{"id": req.ID, "bundle": req.Bundle}).Info("create")

	// Use mount path instead of bundle path inside the VM
	req.Bundle = bundleMountPath

	// Do not pass any mounts to runc, everything is already mounted for us
	req.Rootfs = nil

	// Passthrough runcOptions
	opts, err := unpackBundle(filepath.Join(bundleMountPath, "config.json"), req.Options)
	if err != nil {
		return nil, err
	}
	req.Options = opts

	ctx = namespaces.WithNamespace(ctx, defaultNamespace)
	resp, err := ts.runc.Create(ctx, req)
	if err != nil {
		log.G(ctx).WithError(err).Error("create failed")
		return nil, err
	}

	log.G(ctx).WithField("pid", resp.Pid).Debugf("create succeeded")
	return resp, nil
}

func unpackBundle(path string, bundle *types.Any) (*types.Any, error) {
	// get json bytes from task request
	extraData := &proto.ExtraData{}
	err := types.UnmarshalAny(bundle, extraData)
	if err != nil {
		return nil, err
	}
	// write bundle/config.json bytes
	err = ioutil.WriteFile(path, extraData.JsonSpec, 0644)
	if err != nil {
		return nil, err
	}
	return extraData.RuncOptions, nil
}

func (ts *TaskService) State(ctx context.Context, req *shimapi.StateRequest) (*shimapi.StateResponse, error) {
	log.G(ctx).WithFields(logrus.Fields{"id": req.ID, "exec_id": req.ExecID}).Debug("state")

	ctx = namespaces.WithNamespace(ctx, defaultNamespace)
	resp, err := ts.runc.State(ctx, req)
	if err != nil {
		log.G(ctx).WithError(err).Error("state failed")
		return nil, err
	}

	log.G(ctx).WithFields(logrus.Fields{
		"id":     resp.ID,
		"bundle": resp.Bundle,
		"pid":    resp.Pid,
		"status": resp.Status,
	}).Debug("state succeeded")
	return resp, nil
}

func (ts *TaskService) Start(ctx context.Context, req *shimapi.StartRequest) (*shimapi.StartResponse, error) {
	log.G(ctx).WithFields(logrus.Fields{"id": req.ID, "exec_id": req.ExecID}).Debug("start")

	ctx = namespaces.WithNamespace(ctx, defaultNamespace)
	resp, err := ts.runc.Start(ctx, req)
	if err != nil {
		log.G(ctx).WithError(err).Error("start failed")
		return nil, err
	}

	log.G(ctx).WithField("pid", resp.Pid).Debug("start succeeded")
	return resp, nil
}

func (ts *TaskService) Delete(ctx context.Context, req *shimapi.DeleteRequest) (*shimapi.DeleteResponse, error) {
	log.G(ctx).WithFields(logrus.Fields{"id": req.ID, "exec_id": req.ExecID}).Debug("delete")

	ctx = namespaces.WithNamespace(ctx, defaultNamespace)
	resp, err := ts.runc.Delete(ctx, req)
	if err != nil {
		log.G(ctx).WithError(err).Error("delete failed")
		return nil, err
	}

	log.G(ctx).WithFields(logrus.Fields{
		"pid":         resp.Pid,
		"exit_status": resp.ExitStatus,
	}).Debug("delete succeeded")
	return resp, nil
}

func (ts *TaskService) Pids(ctx context.Context, req *shimapi.PidsRequest) (*shimapi.PidsResponse, error) {
	log.G(ctx).WithField("id", req.ID).Debug("pids")

	ctx = namespaces.WithNamespace(ctx, defaultNamespace)
	resp, err := ts.runc.Pids(ctx, req)
	if err != nil {
		log.G(ctx).WithError(err).Error("pids failed")
		return nil, err
	}

	log.G(ctx).Debug("pids succeeded")
	return resp, nil
}

func (ts *TaskService) Pause(ctx context.Context, req *shimapi.PauseRequest) (*types.Empty, error) {
	log.G(ctx).WithField("id", req.ID).Debug("pause")

	ctx = namespaces.WithNamespace(ctx, defaultNamespace)
	resp, err := ts.runc.Pause(ctx, req)
	if err != nil {
		log.G(ctx).WithError(err).Error("pause failed")
		return nil, err
	}

	log.G(ctx).Debug("pause succeeded")
	return resp, nil
}

func (ts *TaskService) Resume(ctx context.Context, req *shimapi.ResumeRequest) (*types.Empty, error) {
	log.G(ctx).WithField("id", req.ID).Debug("resume")

	ctx = namespaces.WithNamespace(ctx, defaultNamespace)
	resp, err := ts.runc.Resume(ctx, req)
	if err != nil {
		log.G(ctx).WithError(err).Debug("resume failed")
		return nil, err
	}

	log.G(ctx).Debug("resume succeeded")
	return resp, nil
}

func (ts *TaskService) Checkpoint(ctx context.Context, req *shimapi.CheckpointTaskRequest) (*types.Empty, error) {
	log.G(ctx).WithFields(logrus.Fields{"id": req.ID, "path": req.Path}).Info("checkpoint")

	ctx = namespaces.WithNamespace(ctx, defaultNamespace)
	resp, err := ts.runc.Checkpoint(ctx, req)
	if err != nil {
		log.G(ctx).WithError(err).Error("checkout failed")
		return nil, err
	}

	log.G(ctx).Debug("checkpoint succeeded")
	return resp, nil
}

func (ts *TaskService) Kill(ctx context.Context, req *shimapi.KillRequest) (*types.Empty, error) {
	log.G(ctx).WithFields(logrus.Fields{"id": req.ID, "exec_id": req.ExecID}).Debug("kill")

	ctx = namespaces.WithNamespace(ctx, defaultNamespace)
	resp, err := ts.runc.Kill(ctx, req)
	if err != nil {
		log.G(ctx).WithError(err).Error("kill failed")
		return nil, err
	}

	log.G(ctx).Debug("kill succeeded")
	return resp, nil
}

func (ts *TaskService) Exec(ctx context.Context, req *shimapi.ExecProcessRequest) (*types.Empty, error) {
	log.G(ctx).WithFields(logrus.Fields{"id": req.ID, "exec_id": req.ExecID}).Debug("exec")

	ctx = namespaces.WithNamespace(ctx, defaultNamespace)
	resp, err := ts.runc.Exec(ctx, req)
	if err != nil {
		log.G(ctx).WithError(err).Error("exec failed")
		return nil, err
	}

	log.G(ctx).Debug("exec succeeded")
	return resp, nil
}

func (ts *TaskService) ResizePty(ctx context.Context, req *shimapi.ResizePtyRequest) (*types.Empty, error) {
	log.G(ctx).WithFields(logrus.Fields{"id": req.ID, "exec_id": req.ExecID}).Debug("resize_pty")

	ctx = namespaces.WithNamespace(ctx, defaultNamespace)
	resp, err := ts.runc.ResizePty(ctx, req)
	if err != nil {
		log.G(ctx).WithError(err).Error("resize_pty failed")
		return nil, err
	}

	log.G(ctx).Debug("resize_pty succeeded")
	return resp, nil
}

func (ts *TaskService) CloseIO(ctx context.Context, req *shimapi.CloseIORequest) (*types.Empty, error) {
	log.G(ctx).WithFields(logrus.Fields{"id": req.ID, "exec_id": req.ExecID}).Debug("close_io")

	ctx = namespaces.WithNamespace(ctx, defaultNamespace)
	resp, err := ts.runc.CloseIO(ctx, req)
	if err != nil {
		log.G(ctx).WithError(err).Error("close io failed")
		return nil, err
	}

	log.G(ctx).Debug("close io succeeded")
	return resp, nil
}

func (ts *TaskService) Update(ctx context.Context, req *shimapi.UpdateTaskRequest) (*types.Empty, error) {
	log.G(ctx).WithField("id", req.ID).Debug("update")

	ctx = namespaces.WithNamespace(ctx, defaultNamespace)
	resp, err := ts.runc.Update(ctx, req)
	if err != nil {
		log.G(ctx).WithError(err).Error("update failed")
		return nil, err
	}

	log.G(ctx).Debug("update succeeded")
	return resp, nil
}

func (ts *TaskService) Wait(ctx context.Context, req *shimapi.WaitRequest) (*shimapi.WaitResponse, error) {
	log.G(ctx).WithFields(logrus.Fields{"id": req.ID, "exec_id": req.ExecID}).Debug("wait")

	ctx = namespaces.WithNamespace(ctx, defaultNamespace)
	resp, err := ts.runc.Wait(ctx, req)
	if err != nil {
		log.G(ctx).WithError(err).Error("wait failed")
		return nil, err
	}

	log.G(ctx).WithField("exit_status", resp.ExitStatus).Debug("wait succeeded")
	return resp, nil
}

func (ts *TaskService) Cleanup(ctx context.Context) (*shimapi.DeleteResponse, error) {
	log.G(ctx).Debug("cleanup")

	ctx = namespaces.WithNamespace(ctx, defaultNamespace)
	resp, err := ts.runc.Cleanup(ctx)
	if err != nil {
		log.G(ctx).WithError(err).Error("cleanup failed")
		return nil, err
	}

	log.G(ctx).WithFields(logrus.Fields{
		"pid":         resp.Pid,
		"exit_status": resp.ExitStatus,
	}).Error("cleanup succeeded")
	return resp, nil
}

func (ts *TaskService) Stats(ctx context.Context, req *shimapi.StatsRequest) (*shimapi.StatsResponse, error) {
	log.G(ctx).WithField("id", req.ID).Debug("stats")

	ctx = namespaces.WithNamespace(ctx, defaultNamespace)
	resp, err := ts.runc.Stats(ctx, req)
	if err != nil {
		log.G(ctx).WithError(err).Error("stats failed")
		return nil, err
	}

	log.G(ctx).Debug("stats succeeded")
	return resp, nil
}

func (ts *TaskService) Connect(ctx context.Context, req *shimapi.ConnectRequest) (*shimapi.ConnectResponse, error) {
	log.G(ctx).WithField("id", req.ID).Debug("connect")

	ctx = namespaces.WithNamespace(ctx, defaultNamespace)
	resp, err := ts.runc.Connect(ctx, req)
	if err != nil {
		log.G(ctx).WithError(err).Error("connect failed")
		return nil, err
	}

	log.G(ctx).WithFields(logrus.Fields{
		"shim_pid": resp.ShimPid,
		"task_pid": resp.TaskPid,
		"version":  resp.Version,
	}).Error("connect succeeded")
	return resp, nil
}

func (ts *TaskService) Shutdown(ctx context.Context, req *shimapi.ShutdownRequest) (*types.Empty, error) {
	log.G(ctx).WithFields(logrus.Fields{"id": req.ID, "now": req.Now}).Debug("shutdown")

	ctx = namespaces.WithNamespace(ctx, defaultNamespace)
	resp, err := ts.runc.Shutdown(ctx, req)
	if err != nil {
		log.G(ctx).WithError(err).Error("shutdown failed")
		return nil, err
	}

	log.G(ctx).Debug("shutdown succeeded")
	return resp, nil
}

func (ts *TaskService) StartShim(ctx context.Context, id, containerdBinary, containerdAddress string) (string, error) {
	log.G(ctx).WithFields(logrus.Fields{"id": id, "bin": containerdAddress, "addr": containerdAddress}).Debug("start_shim")

	ctx = namespaces.WithNamespace(ctx, defaultNamespace)
	resp, err := ts.runc.StartShim(ctx, id, containerdBinary, containerdAddress)
	if err != nil {
		log.G(ctx).WithError(err).Error("start shim failed")
		return "", err
	}

	log.G(ctx).Debugf("start shim succeeded: %s", resp)
	return resp, err
}
