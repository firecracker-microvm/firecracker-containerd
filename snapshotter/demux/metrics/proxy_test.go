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

package metrics

import (
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/pkg/errors"
)

func okPortRange(startPort, endPort int) error {
	m, err := NewMonitor(startPort, endPort)
	errMsg := errors.New("valid port range should not result in error")
	if err != nil {
		return errMsg
	}
	p, err := m.findOpenPort()
	if err != nil && p == 0 {
		return errMsg
	}

	return nil
}

func invalidPortRange(startPort, endPort int) error {
	_, err := NewMonitor(startPort, endPort)
	if err == nil {
		return errors.New("invalid port range should result in error")
	}

	return nil
}

func noPortsAvailable(startPort, endPort int) error {
	m, err := NewMonitor(startPort, endPort)
	if err != nil {
		return errors.New("initialization of monitor should not result in error")
	}
	m.portsInUse[startPort] = true
	m.portsInUse[endPort] = true
	p, err := m.findOpenPort()
	if err == nil && p != 0 {
		return errors.New("no port should be available")
	}

	return nil
}

func TestFindOpenPort(t *testing.T) {
	tests := []struct {
		name      string
		startPort int
		endPort   int
		run       func(int, int) error
	}{
		{"Valid Port Range", 9000, 9999, okPortRange},
		{"Invalid Port Range", 9999, 9000, invalidPortRange},
		{"No Ports Available", 9999, 9999, noPortsAvailable},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if err := test.run(test.startPort, test.endPort); err != nil {
				t.Fatal(test.name + ": " + err.Error())
			}
		})
	}
}

const metricsResponse = `
		# A histogram, which has a pretty complex representation in the text format:
		# HELP http_request_duration_seconds A histogram of the request duration.
		# TYPE http_request_duration_seconds histogram
		http_request_duration_seconds_bucket{le="0.05"} 24054
		http_request_duration_seconds_bucket{le="0.1"} 33444
		http_request_duration_seconds_bucket{le="0.2"} 100392
		http_request_duration_seconds_bucket{le="0.5"} 129389
		http_request_duration_seconds_bucket{le="1"} 133988
		http_request_duration_seconds_bucket{le="+Inf"} 144320
		http_request_duration_seconds_sum 53423
		http_request_duration_seconds_count 144320
		`

func happyPath() error {
	reader := mockReader{response: []byte(metricsResponse), err: io.EOF}
	client := mockClient{getError: nil, getResponse: http.Response{Body: &reader}}
	uut := Proxy{client: &client}

	rr := httptest.NewRecorder()
	req, err := http.NewRequest("GET", RequestPath, nil)
	if err != nil {
		return errors.New("expected no error from metrics request")
	}
	uut.metrics(rr, req)
	if status := rr.Code; status != http.StatusOK {
		return fmt.Errorf("metrics handler returned wrong status code: got %v want %v",
			status, http.StatusOK)
	}

	if rr.Body.String() != metricsResponse {
		return fmt.Errorf("metrics handler returned unexpected body: got %v want %v",
			rr.Body.String(), metricsResponse)
	}

	return nil
}

func exceedBufferSize() error {
	b := make([]byte, 1<<16)
	largeResponse := string(b)

	reader := mockReader{response: []byte(largeResponse), err: io.EOF}
	client := mockClient{getError: nil, getResponse: http.Response{Body: &reader}}
	uut := Proxy{client: &client}

	rr := httptest.NewRecorder()
	req, err := http.NewRequest("GET", RequestPath, nil)
	if err != nil {
		return errors.New("expected no error from metrics request")
	}
	uut.metrics(rr, req)
	if status := rr.Code; status != http.StatusOK {
		return fmt.Errorf("metrics handler returned wrong status code: got %v want %v",
			status, http.StatusOK)
	}

	// Check that body does not match since it should be truncated.
	if rr.Body.String() == largeResponse {
		// Do not output large body to keep testing logs clean.
		return fmt.Errorf("metrics handler returned unexpected body")
	}

	responseLen := len(rr.Body.String())
	if len(rr.Body.String()) > MaxMetricsResponseSize {
		return fmt.Errorf("metrics handler body too large: got %v want %v", responseLen, MaxMetricsResponseSize)

	}

	return nil
}

func returnErrorOnRequest() error {
	reader := mockReader{response: []byte(""), err: nil}
	client := mockClient{getError: http.ErrHandlerTimeout, getResponse: http.Response{Body: &reader}}
	uut := Proxy{client: &client}

	rr := httptest.NewRecorder()
	req, err := http.NewRequest("GET", RequestPath, nil)
	if err != nil {
		return errors.New("expected no error from metrics request")
	}
	uut.metrics(rr, req)

	if status := rr.Code; status != http.StatusInternalServerError {
		return fmt.Errorf("metrics handler returned wrong status code: got %v want %v",
			status, http.StatusInternalServerError)
	}

	return nil
}

func returnErrorOnResponse() error {
	reader := mockReader{response: []byte(""), err: io.ErrClosedPipe}
	client := mockClient{getError: nil, getResponse: http.Response{Body: &reader}}
	uut := Proxy{client: &client}

	rr := httptest.NewRecorder()
	req, err := http.NewRequest("GET", RequestPath, nil)
	if err != nil {
		return errors.New("expected no error from metrics request")
	}
	uut.metrics(rr, req)

	if status := rr.Code; status != http.StatusInternalServerError {
		return fmt.Errorf("metrics handler returned wrong status code: got %v want %v",
			status, http.StatusInternalServerError)
	}

	return nil
}

func TestMetricsGet(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		run  func() error
	}{
		{"HappyPath", happyPath},
		{"ExceedBufferSize", exceedBufferSize},
		{"ReturnErrorOnRequest", returnErrorOnRequest},
		{"ReturnErrorOnResponse", returnErrorOnResponse},
	}

	for _, test := range tests {
		if err := test.run(); err != nil {
			t.Fatalf(test.name+" test failed: %s", err)
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
