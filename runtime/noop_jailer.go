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
	"errors"
	"os"
	"syscall"

	"github.com/firecracker-microvm/firecracker-go-sdk"
	"github.com/sirupsen/logrus"

	"github.com/firecracker-microvm/firecracker-containerd/config"
	"github.com/firecracker-microvm/firecracker-containerd/internal/vm"
)

// noopJailer is a jailer that returns only successful responses and performs
// no operations during calls
type noopJailer struct {
	logger  *logrus.Entry
	shimDir vm.Dir
	ctx     context.Context
	pid     int
}

func newNoopJailer(ctx context.Context, logger *logrus.Entry, shimDir vm.Dir) *noopJailer {
	return &noopJailer{
		logger:  logger,
		shimDir: shimDir,
		ctx:     ctx,
		pid:     0,
	}
}

func (j *noopJailer) BuildJailedMachine(cfg *config.Config, _ *firecracker.Config, vmID string) ([]firecracker.Opt, error) {
	if len(cfg.FirecrackerBinaryPath) == 0 {
		return []firecracker.Opt{}, nil
	}

	relSocketPath, err := j.shimDir.FirecrackerSockRelPath()
	if err != nil {
		return nil, err
	}

	cmd := firecracker.VMCommandBuilder{}.
		WithBin(cfg.FirecrackerBinaryPath).
		WithSocketPath(relSocketPath).
		WithArgs([]string{"--id", vmID}).
		Build(j.ctx)

	if cfg.DebugHelper.LogFirecrackerOutput() {
		cmd.Stdout = j.logger.WithField("vmm_stream", "stdout").WriterLevel(logrus.DebugLevel)
		cmd.Stderr = j.logger.WithField("vmm_stream", "stderr").WriterLevel(logrus.DebugLevel)
	}

	pidHandler := firecracker.Handler{
		Name: "firecracker-containerd-jail-pid-handler",
		Fn: func(_ context.Context, m *firecracker.Machine) error {
			pid, err := m.PID()
			if err != nil {
				return err
			}
			j.pid = pid
			return nil
		},
	}

	j.logger.Debug("noop operation for BuildJailedMachine")
	return []firecracker.Opt{
		firecracker.WithProcessRunner(cmd),
		func(m *firecracker.Machine) {
			m.Handlers.FcInit = m.Handlers.FcInit.Append(pidHandler)
		},
	}, nil
}

func (j *noopJailer) JailPath() vm.Dir {
	j.logger.Debug("noop operation returning shim dir for JailPath")
	return j.shimDir
}

func (j *noopJailer) ExposeFileToJail(_ string) error {
	j.logger.Debug("noop operation for ExposeFileToJail")
	return nil
}

func (j *noopJailer) StubDrivesOptions() []FileOpt {
	j.logger.Debug("noop operation for StubDrivesOptions")
	return []FileOpt{}
}

func (j *noopJailer) Stop(force bool) error {
	if j.pid == 0 {
		return errors.New("the machine has not been started")
	}

	signal := syscall.SIGTERM
	if force {
		signal = syscall.SIGKILL
	}
	j.logger.Debugf("sending signal %d to %d", signal, j.pid)
	p, err := os.FindProcess(j.pid)
	if err != nil {
		return err
	}

	err = p.Signal(signal)
	if err == nil || err.Error() == "os: process already finished" {
		return nil
	}
	return err
}

func (j *noopJailer) Close() error {
	return os.RemoveAll(j.shimDir.RootPath())
}
