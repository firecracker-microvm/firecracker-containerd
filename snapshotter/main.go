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
	"flag"
	"os"

	"github.com/sirupsen/logrus"

	"github.com/firecracker-microvm/firecracker-containerd/snapshotter/app"
	"github.com/firecracker-microvm/firecracker-containerd/snapshotter/config"
)

func main() {
	var filepath string
	flag.StringVar(&filepath, "config", "/etc/demux-snapshotter/config.toml", "Configuration filepath")

	if !flag.Parsed() {
		flag.Parse()
	}

	config, err := config.Load(filepath)
	if err != nil {
		logger := logrus.New().WithField("config", filepath)
		logger.WithError(err).Error("error reading config")
		return
	}

	logLevel, err := logrus.ParseLevel(config.Debug.LogLevel)
	if err != nil {
		logLevel = logrus.InfoLevel
		logrus.New().WithError(err).Warn("Fallback to default log level")
	}
	logrus.SetLevel(logLevel)

	if err := app.Run(config); err != nil {
		os.Exit(1)
	}
	os.Exit(0)
}
