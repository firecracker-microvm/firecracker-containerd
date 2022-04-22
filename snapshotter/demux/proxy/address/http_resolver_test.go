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

package address

import (
	"errors"
	"fmt"
	"io"
	"net/http"
	"testing"
)

func returnErrorOnHTTPClientError() error {
	client := mockClient{}
	client.getError = errors.New("error on HTTP GET")
	uut := HTTPResolver{url: "localhost:10001", client: &client}

	if _, err := uut.Get("namespace"); err == nil {
		return errors.New("expected error on HTTP client GET")
	}

	return nil
}

func returnErrorOnResponseReadError() error {
	reader := mockReader{response: nil, err: errors.New("mock error on read")}
	client := mockClient{getError: nil, getResponse: http.Response{Body: &reader}}
	uut := HTTPResolver{url: "localhost:10001", client: &client}

	if _, err := uut.Get("namespace"); err == nil {
		return errors.New("expected error on body read")
	}

	return nil
}

func returnErrorOnJSONParserError() error {
	reader := mockReader{response: []byte(`{"network": "unix"`), err: io.EOF}
	client := mockClient{getError: nil, getResponse: http.Response{Body: &reader}}
	uut := HTTPResolver{url: "localhost:10001", client: &client}

	if _, err := uut.Get("namespace"); err == nil {
		return errors.New("expected error on JSON parsing")
	}

	return nil
}

func happyPath() error {
	reader := mockReader{response: []byte(
		`{
			"network": "unix", 
			"address": "/path/to/snapshotter.vsock", 
			"snapshotter_port": "10000", 
			"metrics_port": "10001",
			"labels": {
				"test1": "label1",
				"test2": "label2"
				}
			}`), err: io.EOF}
	client := mockClient{getError: nil, getResponse: http.Response{Body: &reader}}
	uut := HTTPResolver{url: "localhost:10001", client: &client}

	actual, err := uut.Get("namespace")
	if err != nil {
		fmt.Println(err)
		return errors.New("expected no error from HTTP resolver")
	}

	if actual.Network != "unix" {
		return fmt.Errorf("Expected network 'unix' but actual %s", actual.Address)
	}
	if actual.Address != "/path/to/snapshotter.vsock" {
		return fmt.Errorf("Expected address '/path/to/snapshotter.vsock' but actual %s", actual.Address)
	}
	if actual.SnapshotterPort != "10000" {
		return fmt.Errorf("Expected metrics port '10000' but actual %s", actual.MetricsPort)
	}
	if actual.MetricsPort != "10001" {
		return fmt.Errorf("Expected metrics port '10001' but actual %s", actual.MetricsPort)
	}
	if actual.Labels["test1"] != "label1" {
		return fmt.Errorf("Expected metrics label key='test1' value='label1' but actual %s", actual.Labels["test1"])
	}
	if actual.Labels["test2"] != "label2" {
		return fmt.Errorf("Expected metrics label key='test2' value='label2' but actual %s", actual.Labels["test1"])
	}
	return nil
}

func TestHttpResolverGet(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		run  func() error
	}{
		{"HttpClientError", returnErrorOnHTTPClientError},
		{"ResponseReaderError", returnErrorOnResponseReadError},
		{"JsonParserError", returnErrorOnJSONParserError},
		{"HappyPath", happyPath},
	}

	for _, test := range tests {
		if err := test.run(); err != nil {
			t.Fatalf(test.name+" test failed: %s", err)
		}
	}
}

func TestRequestUrlFormat(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		url       string
		namespace string
		expected  string
	}{
		{"NS-1", "http://127.0.0.1:10001", "ns-1", "http://127.0.0.1:10001/address?namespace=ns-1"},
		{"NS-2", "http://localhost:10001", "ns-2", "http://localhost:10001/address?namespace=ns-2"},
	}

	for _, test := range tests {
		if actual := requestURL(test.url, test.namespace); actual != test.expected {
			t.Fatalf("%s failed: expected %s actual %s", test.name, test.expected, actual)
		}
	}
}

type mockClient struct {
	getResponse http.Response
	getError    error
}

func (c *mockClient) Get(url string) (*http.Response, error) {
	if c.getError != nil {
		return nil, c.getError
	}
	return &c.getResponse, nil
}

type mockReader struct {
	response []byte
	err      error
}

func (r *mockReader) Read(p []byte) (int, error) {
	return copy(p, r.response), r.err
}

func (r *mockReader) Close() error {
	return nil
}
