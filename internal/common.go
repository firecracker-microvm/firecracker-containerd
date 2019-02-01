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

package internal

const (
	// StdinPort represents vsock port to be used for stdin
	StdinPort = 11000
	// StdoutPort represents vsock port to be used for stdout
	StdoutPort = 11001
	// StderrPort represents vsock port to be used for stderr
	StderrPort = 11002
	// DefaultBufferSize represents buffer size in bytes to used for IO between runtime and agent
	DefaultBufferSize = 1024
)
