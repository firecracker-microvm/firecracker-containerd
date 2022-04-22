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

package config

import (
	"io"
	"os"

	"github.com/pelletier/go-toml"
)

// Config contains metadata necessary to forward snapshotter requests
// to the destined remote snapshotter.
//
// The default location for the configuration file is '/etc/demux-snapshotter/config.toml'
type Config struct {
	Snapshotter snapshotter `toml:"snapshotter"`

	Debug debug `toml:"debug"`
}

type snapshotter struct {
	Listener listener `toml:"listener"`
	Proxy    proxy    `toml:"proxy"`
	Metrics  metrics  `toml:"metrics"`
}

type listener struct {
	Network string `toml:"network" default:"unix"`
	Address string `toml:"address" default:"/var/lib/demux-snapshotter/snapshotter.sock"`
}

type proxy struct {
	Address address `toml:"address"`
}

type address struct {
	Resolver resolver `toml:"resolver"`
}

type resolver struct {
	Type    string `toml:"type"`
	Address string `toml:"address"`
}

type debug struct {
	LogLevel string `toml:"logLevel" default:"info"`
}

type metrics struct {
	Enable               bool   `toml:"enable" default:"false"`
	Host                 string `toml:"host"`
	PortRange            string `toml:"port_range"`
	ServiceDiscoveryPort int    `toml:"service_discovery_port"`
}

// Load parses application configuration from a specified file path.
func Load(filePath string) (Config, error) {
	file, err := os.Open(filePath)
	if err != nil {
		return Config{}, err
	}
	defer file.Close()

	fileInfo, err := file.Stat()
	if err != nil {
		return Config{}, err
	}

	return load(file, fileInfo.Size())
}

func load(reader io.Reader, readSize int64) (Config, error) {
	buffer := make([]byte, readSize)
	readBytes, err := reader.Read(buffer)
	if int64(readBytes) != readSize || err != nil {
		return Config{}, err
	}

	config := Config{}
	if err = toml.Unmarshal(buffer, &config); err != nil {
		return Config{}, err
	}
	return config, nil
}
