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
	"errors"
	"fmt"
	"testing"
)

type fakeReader struct {
	buffer    []byte
	bytesRead int
	err       error
}

func (fr fakeReader) Read(p []byte) (int, error) {
	copy(p, fr.buffer)
	return fr.bytesRead, fr.err
}

func errorOnRead() error {
	reader := fakeReader{
		buffer:    nil,
		bytesRead: 0,
		err:       errors.New("mock read error"),
	}
	if _, err := load(reader, 10); err == nil {
		return errors.New("expected error on read")
	}
	return nil
}

func errorOnParse() error {
	fileContents := []byte(`[snapshotter`)
	reader := fakeReader{
		buffer:    fileContents,
		bytesRead: len(fileContents),
		err:       nil,
	}
	if _, err := load(reader, int64(len(fileContents))); err == nil {
		return errors.New("expected error on parse")
	}
	return nil
}

func defaultConfig() error {
	expected := Config{
		Snapshotter: snapshotter{
			Listener: listener{
				Network: "unix",
				Address: "/var/lib/demux-snapshotter/snapshotter.sock",
			},
			Metrics: metrics{
				Enable: false,
			},
		},
		Debug: debug{
			LogLevel: "info",
		},
	}
	return parseConfig([]byte(``), expected)
}

func parseExampleConfig() error {
	fileContents := []byte(`
	[snapshotter]
	  [snapshotter.listener]
	    network = "unix"
	    address = "/var/lib/demux-snapshotter/non-default-snapshotter.vsock"
	  [snapshotter.proxy.address.resolver]
	    type = "http"
	    address = "localhost:10001"
	  [snapshotter.metrics]
        enable = true
		port_range = "9000-9999"
		host = "0.0.0.0"
		service_discovery_port = 8080
	[debug]
	  logLevel = "debug"
	`)
	expected := Config{
		Snapshotter: snapshotter{
			Listener: listener{
				Network: "unix",
				Address: "/var/lib/demux-snapshotter/non-default-snapshotter.vsock",
			},
			Proxy: proxy{
				Address: address{
					Resolver: resolver{
						Type:    "http",
						Address: "localhost:10001",
					},
				},
			},
			Metrics: metrics{
				Enable:               true,
				PortRange:            "9000-9999",
				Host:                 "0.0.0.0",
				ServiceDiscoveryPort: 8080,
			},
		},
		Debug: debug{
			LogLevel: "debug",
		},
	}
	return parseConfig(fileContents, expected)
}

func parseConfig(input []byte, expected Config) error {
	reader := fakeReader{
		buffer:    input,
		bytesRead: len(input),
		err:       nil,
	}
	actual, err := load(reader, int64(len(input)))
	if err != nil {
		return errors.New("expected no error on parse")
	}
	if actual != expected {
		return fmt.Errorf("expected %v actual %v", expected, actual)
	}
	return nil
}

func TestLoad(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		run  func() error
	}{
		{"ReadError", errorOnRead},
		{"ParseError", errorOnParse},
		{"DefaultConfig", defaultConfig},
		{"ParseFullConfig", parseExampleConfig},
	}

	for _, test := range tests {
		if err := test.run(); err != nil {
			t.Fatalf("%s: %s", test.name, err.Error())
		}
	}
}
