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
package mmds

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"io/ioutil"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
)

const token = "mmds_token"

type roundTripFunc func(*http.Request) (*http.Response, error)
type mockRoundTripper struct {
	f roundTripFunc
}

func (m *mockRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	return m.f(req)
}

func errorResponse() (*http.Response, error) {
	return nil, errors.New("this should not be called from tests")
}
func v1TokenResponse() (*http.Response, error) {
	return &http.Response{
		StatusCode: http.StatusMethodNotAllowed,
	}, nil
}
func v2TokenResponse() (*http.Response, error) {
	return &http.Response{
		StatusCode: http.StatusOK,
		Body:       ioutil.NopCloser(bytes.NewBufferString(token)),
	}, nil
}

func newHttpClient(tokenResponse, metadataResponse func() (*http.Response, error)) *http.Client {
	return &http.Client{
		Transport: &mockRoundTripper{
			f: func(r *http.Request) (*http.Response, error) {
				switch r.URL.Path {
				case getTokenPath:
					return tokenResponse()
				case "/docker-credentials":
					return metadataResponse()
				default:
					return nil, fmt.Errorf("unexpected url path: %s", r.URL.Path)
				}
			},
		},
	}
}

func TestNewMMDSV1Client(t *testing.T) {
	httpClient := newHttpClient(v1TokenResponse, errorResponse)
	client, err := NewClient(httpClient)
	assert.NoError(t, err)
	assert.Equal(t, client.version, v1)
}

func TestNewMMDSV2Client(t *testing.T) {
	httpClient := newHttpClient(v2TokenResponse, errorResponse)
	client, err := NewClient(httpClient)
	assert.NoError(t, err)
	assert.Equal(t, client.token, token)
	assert.Equal(t, client.version, v2)
}

func TestGetMetadata(t *testing.T) {
	metadata := `{"public.ecr.aws":{"username":"123456789012","password":"ecr_token"}}`
	expectedMetadata := map[string]interface{}{
		"public.ecr.aws": map[string]interface{}{
			"username": "123456789012",
			"password": "ecr_token",
		},
	}
	type testcase struct {
		name             string
		metadataResponse *http.Response
		expected         map[string]interface{}
		expectedError    error
	}
	for _, test := range []testcase{
		{
			name: "successful metadata",
			metadataResponse: &http.Response{
				StatusCode: http.StatusOK,
				Body:       io.NopCloser(bytes.NewBufferString(metadata)),
			},
			expected:      expectedMetadata,
			expectedError: nil,
		},
		{
			name: "unauthorized",
			metadataResponse: &http.Response{
				StatusCode: http.StatusUnauthorized,
			},
			expected:      nil,
			expectedError: errUnauthorized,
		},
		{
			name: "server error",
			metadataResponse: &http.Response{
				StatusCode: http.StatusInternalServerError,
				Body:       io.NopCloser(bytes.NewBufferString("Internal server error")),
			},
			expected:      nil,
			expectedError: errors.New("unexpected http response: 500 - (err <nil>) Internal server error"),
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			httpClient := newHttpClient(v1TokenResponse, func() (*http.Response, error) { return test.metadataResponse, nil })
			client, err := NewClient(httpClient)
			assert.NoError(t, err)
			assert.Equal(t, client.version, v1)
			actual, err := client.GetMetadata("docker-credentials")
			if test.expectedError != nil {
				assert.Equal(t, test.expectedError, err)
			} else {
				assert.Equal(t, test.expected, actual)
			}
		})
	}
}
