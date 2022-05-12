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
	"bytes"
	"context"
	"strings"

	"github.com/containerd/containerd"
	"github.com/containerd/containerd/cio"
	"github.com/pkg/errors"

	"github.com/firecracker-microvm/firecracker-containerd/firecracker-control/client"
	"github.com/firecracker-microvm/firecracker-containerd/internal"
	"github.com/firecracker-microvm/firecracker-containerd/internal/integtest"
)

func init() {
	flag, err := internal.SupportCPUTemplate()
	if err != nil {
		panic(err)
	}

	if flag {
		integtest.DefaultRuntimeConfig.CPUTemplate = "T2"
	}
}

// devmapper is the only snapshotter we can use with Firecracker
const defaultSnapshotterName = "devmapper"

var testNameToVMIDReplacer = strings.NewReplacer("/", "-", "_", "-")

func testNameToVMID(s string) string {
	return testNameToVMIDReplacer.Replace(s)
}

type commandResult struct {
	stdout   string
	stderr   string
	exitCode uint32
}

func runTask(ctx context.Context, c containerd.Container) (*commandResult, error) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer

	task, err := c.NewTask(ctx, cio.NewCreator(cio.WithStreams(nil, &stdout, &stderr)))
	if err != nil {
		return nil, err
	}

	exitCh, err := task.Wait(ctx)
	if err != nil {
		return nil, err
	}

	err = task.Start(ctx)
	if err != nil {
		return nil, err
	}

	select {
	case exitStatus := <-exitCh:
		if err := exitStatus.Error(); err != nil {
			return nil, err
		}

		_, err := task.Delete(ctx)
		if err != nil {
			return nil, err
		}

		return &commandResult{
			stdout:   stdout.String(),
			stderr:   stderr.String(),
			exitCode: exitStatus.ExitCode(),
		}, nil
	case <-ctx.Done():
		return nil, errors.New("context cancelled")
	}
}

func newFCControlClient(socket string) (*client.Client, error) {
	return client.New(socket + ".ttrpc")
}
