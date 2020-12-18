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

package io

import (
	"fmt"
	"math"
	"sync"
	"time"

	"github.com/firecracker-microvm/firecracker-containerd/internal/vm"
	"github.com/firecracker-microvm/firecracker-containerd/proto"
	"github.com/sirupsen/logrus"

	"github.com/containerd/containerd/cio"
)

const (
	// TaskExecID is a special exec ID that is pointing its task itself.
	// While the constant is defined here, the convention is coming from containerd.
	TaskExecID = ""

	minVsockIOPort             = uint32(11000)
	defaultVSockConnectTimeout = 5 * time.Second
)

// Manager manages FIFO files on a host to translate the FIFO paths on responses.
type Manager struct {
	vsockIOPortCount uint32
	vsockPortMu      sync.Mutex

	// fifos have stdio FIFOs containerd passed to the shim. The key is [taskID][execID].
	fifos   map[string]map[string]cio.Config
	fifosMu sync.Mutex

	logger *logrus.Entry
}

// NewManager returns a new Manager.
func NewManager(logger *logrus.Entry) *Manager {
	return &Manager{
		logger: logger,
		fifos:  make(map[string]map[string]cio.Config),
	}
}

// ReservePorts reserves stdio vsock ports and return a new ExtraData.
func (m *Manager) ReservePorts(extra proto.ExtraData) proto.ExtraData {
	result := extra

	result.StdinPort = m.nextVSockPort()
	result.StdoutPort = m.nextVSockPort()
	result.StderrPort = m.nextVSockPort()

	return result
}

func (m *Manager) nextVSockPort() uint32 {
	m.vsockPortMu.Lock()
	defer m.vsockPortMu.Unlock()

	port := minVsockIOPort + m.vsockIOPortCount
	if port == math.MaxUint32 {
		// given we use 3 ports per container, there would need to
		// be about 1431652098 containers spawned in this VM for
		// this to actually happen in practice.
		panic("overflow of vsock ports")
	}

	m.vsockIOPortCount++
	return port
}

// Reserve adds on-host FIFO files regarding the given exec.
func (m *Manager) Reserve(taskID, execID string, config cio.Config) error {
	m.fifosMu.Lock()
	defer m.fifosMu.Unlock()

	_, exists := m.fifos[taskID]
	if !exists {
		m.fifos[taskID] = make(map[string]cio.Config)
	}

	value, exists := m.fifos[taskID][execID]
	if exists {
		return fmt.Errorf("failed to add FIFO files for task %q (exec=%q). There was %+v already", taskID, execID, value)
	}
	m.fifos[taskID][execID] = config
	return nil
}

// Release removes the entry regarding the given exec.
// Note that containerd is responsible for removing actual FIFO files.
func (m *Manager) Release(taskID, execID string) error {
	m.fifosMu.Lock()
	defer m.fifosMu.Unlock()

	_, exists := m.fifos[taskID][execID]
	if !exists {
		return fmt.Errorf("task %q (exec=%q) doesn't have corresponding FIFOs to delete", taskID, execID)
	}
	delete(m.fifos[taskID], execID)

	if execID == TaskExecID {
		delete(m.fifos, taskID)
	}
	return nil
}

// Find returns FIFO paths regarding the given exec.
func (m *Manager) Find(taskID, execID string) (cio.Config, error) {
	m.fifosMu.Lock()
	defer m.fifosMu.Unlock()

	fifos, ok := m.fifos[taskID]
	if !ok {
		return cio.Config{}, fmt.Errorf("failed to find task %q", taskID)
	}

	config, ok := fifos[execID]
	if !ok {
		return cio.Config{}, fmt.Errorf("failed to find exec %q from task %q", execID, taskID)
	}

	return config, nil
}

// NewProxy creates a new IOProxy.
func (m *Manager) NewProxy(relVSockPath string, config cio.Config, extraData proto.ExtraData) vm.IOProxy {
	if vm.IsAgentOnlyIO(config.Stdout, m.logger) {
		return vm.NewNullIOProxy()
	}

	var stdin, stdout, stderr *vm.IOConnectorPair
	if config.Stdin != "" {
		stdin = &vm.IOConnectorPair{
			ReadConnector:  vm.ReadFIFOConnector(config.Stdin),
			WriteConnector: vm.VSockDialConnector(defaultVSockConnectTimeout, relVSockPath, extraData.StdinPort),
		}
	}

	if config.Stdout != "" {
		stdout = &vm.IOConnectorPair{
			ReadConnector:  vm.VSockDialConnector(defaultVSockConnectTimeout, relVSockPath, extraData.StdoutPort),
			WriteConnector: vm.WriteFIFOConnector(config.Stdout),
		}
	}

	if config.Stderr != "" {
		stderr = &vm.IOConnectorPair{
			ReadConnector:  vm.VSockDialConnector(defaultVSockConnectTimeout, relVSockPath, extraData.StderrPort),
			WriteConnector: vm.WriteFIFOConnector(config.Stderr),
		}
	}
	return vm.NewIOConnectorProxy(stdin, stdout, stderr)
}
