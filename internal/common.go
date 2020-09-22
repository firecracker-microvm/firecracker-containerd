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

package internal

import (
	"bytes"
	"fmt"
	"io"
)

const (
	// StdinPort represents vsock port to be used for stdin
	StdinPort = 11000
	// StdoutPort represents vsock port to be used for stdout
	StdoutPort = 11001
	// StderrPort represents vsock port to be used for stderr
	StderrPort = 11002
	// DefaultBufferSize represents buffer size in bytes to used for IO between runtime and agent
	DefaultBufferSize = 1024

	// FirecrackerSockName is the name of the Firecracker VMM API socket
	FirecrackerSockName = "firecracker.sock"
	// FirecrackerVSockName is the name of the Firecracker VSock unix path used for communication
	// between the runtime and the agent
	FirecrackerVSockName = "firecracker.vsock"
	// FirecrackerLogFifoName is the name of the Firecracker VMM log FIFO
	FirecrackerLogFifoName = "fc-logs.fifo"
	// FirecrackerMetricsFifoName is the name of the Firecracker VMM metrics FIFO
	FirecrackerMetricsFifoName = "fc-metrics.fifo"

	// TODO these strings are hardcoded throughout the containerd codebase, it may
	// be worth sending them a PR to define them as constants accessible to shim
	// implementations like our own

	// ShimAddrFileName is the name of the file in which a shim's API address can be found, used by
	// containerd to reconnect after it restarts
	ShimAddrFileName = "address"

	// ShimLogFifoName is the name of the FIFO created by containerd for a shim to write its logs to
	ShimLogFifoName = "log"

	// LogPathNameStart is the name of the FIFO created by containerd for a shim to write its logs to
	LogPathNameStart = "log_start"

	// LogPathNameLoad is the name of the FIFO created by containerd for a shim to write its logs to
	LogPathNameLoad = "log_load"

	// OCIConfigName is the name of the OCI bundle's config field
	OCIConfigName = "config.json"

	// BundleRootfsName is the name of the bundle's directory for holding the container's rootfs
	BundleRootfsName = "rootfs"

	// VMIDEnvVarKey is the environment variable key used to provide a VMID to a shim process
	VMIDEnvVarKey = "FIRECRACKER_VM_ID"

	// FCSocketFDEnvKey is the environment variable key used to provide the FD of the fccontrol listening socket to a shim
	FCSocketFDEnvKey = "FCCONTROL_SOCKET_FD"

	// ShimBinaryName is the name of the runtime shim binary
	ShimBinaryName = "containerd-shim-aws-firecracker"
)

// MagicStubBytes used to determine whether or not a drive is a stub drive
var MagicStubBytes = []byte{214, 244, 216, 245, 215, 177, 177, 177}

// IsStubDrive will check to see if the io.Reader follows our stub drive
// format. In the event of an error, this will return false. This will only
// return true when the first n bytes read matches that of the magic stub
// bytes and where n is the length of magic bytes.
func IsStubDrive(r io.Reader) bool {
	buf := make([]byte, len(MagicStubBytes))
	lr := io.LimitReader(r, int64(len(MagicStubBytes)))
	n, err := lr.Read(buf)
	if err != nil {
		return false
	}

	if n != len(MagicStubBytes) {
		return false
	}

	return bytes.Equal(buf[:n], MagicStubBytes)
}

// ParseStubContent will parse the contents of an io.Reader and return the
// given id that was encoded
func ParseStubContent(r io.Reader) (string, error) {
	magicBytesReader := io.LimitReader(r, int64(len(MagicStubBytes)))
	magicBytes := make([]byte, len(MagicStubBytes))
	_, err := magicBytesReader.Read(magicBytes)
	if err != nil {
		return "", err
	}

	sizeReader := io.LimitReader(r, 1)
	sizeByte := make([]byte, 1)
	_, err = sizeReader.Read(sizeByte)
	if err != nil {
		return "", err
	}

	idReader := io.LimitReader(r, int64(sizeByte[0]))
	idBytes := make([]byte, sizeByte[0])
	_, err = idReader.Read(idBytes)
	if err != nil {
		return "", err
	}

	return string(idBytes), nil
}

// GenerateStubContent will generate stub content using the magic stub bytes
// and a length encoded string
func GenerateStubContent(id string) (string, error) {
	length := len(id)
	if length > 0xFF {
		return "", fmt.Errorf("Length of drive id, %d, is too long and is limited to %d bytes", length, 0xFF)
	}

	return fmt.Sprintf("%s%c%s", MagicStubBytes, byte(length), id), nil
}
