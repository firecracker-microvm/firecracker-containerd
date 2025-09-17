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

// Package integtest provides integration testing utilities.
package integtest

import (
	"bytes"
	"context"

	"github.com/containerd/containerd"
	"github.com/containerd/containerd/cio"
)

// CommandResult encapsulates the stdout, stderr, and exit code returned
// from a task.
type CommandResult struct {
	Stdout   string
	Stderr   string
	ExitCode uint32
}

// RunTask is a utility function for running a task and returning the result.
func RunTask(ctx context.Context, c containerd.Container) (*CommandResult, error) {
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

		return &CommandResult{
			Stdout:   stdout.String(),
			Stderr:   stderr.String(),
			ExitCode: exitStatus.ExitCode(),
		}, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}
