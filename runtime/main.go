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
	"os"

	"github.com/containerd/containerd/runtime/v2/shim"
	"github.com/containerd/log"
	"github.com/sirupsen/logrus"
)

const shimID = "aws.firecracker"

var revision string

func init() {
	logrus.SetFormatter(&logrus.TextFormatter{
		TimestampFormat: log.RFC3339NanoFixed,
		FullTimestamp:   true,
	})

	logrus.SetOutput(os.Stdout)
}

func main() {
	shim.Run(shimID, NewService, func(cfg *shim.Config) {
		cfg.NoSetupLogger = true

		// Just let child processes get reparented to init
		// (or the nearest subreaper). Enabling reaping
		// creates races with `os.Exec` commands that expect
		// to be able to wait on their child processes.
		cfg.NoSubreaper = true
		cfg.NoReaper = true
	})
}
