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
	"fmt"
	"io/ioutil"
	"net/http"
	"testing"

	"github.com/docker/docker-credential-helpers/credentials"
	"github.com/stretchr/testify/assert"
)

const (
	validDockerCredentials = `
		{
			"public.ecr.aws": {
				"username": "123456789012",
				"password": "ecr_token"
			},
			"docker.io": {
				"username": "user",
				"password": "pass"
			}
		}`

	dockerCredentials = `
		{
			"public.ecr.aws": {
				"username": "123456789012",
				"password": "ecr_token"
			},
			"docker.io": {
				"username": "user",
				"password": "pass"
			},
			"nouser.io": {
				"password": "pass"
			},
			"nopass.io": {
				"username": "user"
			},
			"notamap.io": [],
			"notastring.io": {
				"username": []
			}
		}`
)

func validMetadataResponse() (*http.Response, error) {
	return &http.Response{
		StatusCode: http.StatusOK,
		Body:       ioutil.NopCloser(bytes.NewBufferString(validDockerCredentials)),
	}, nil
}

func metadataResponse() (*http.Response, error) {
	return &http.Response{
		StatusCode: http.StatusOK,
		Body:       ioutil.NopCloser(bytes.NewBufferString(dockerCredentials)),
	}, nil
}

var validHttpClient = newHttpClient(v1TokenResponse, validMetadataResponse)
var httpClient = newHttpClient(v1TokenResponse, metadataResponse)

func TestGet(t *testing.T) {
	client, err := NewClient(httpClient)
	assert.NoError(t, err)
	helper := Helper{client}
	type testcase struct {
		name             string
		url              string
		expectedUsername string
		expectedPassword string
		expectedError    error
	}

	for _, test := range []testcase{
		{
			name:             "public.ecr.aws",
			url:              "public.ecr.aws",
			expectedUsername: "123456789012",
			expectedPassword: "ecr_token",
			expectedError:    nil,
		},
		{
			name:             "docker.io",
			url:              "docker.io",
			expectedUsername: "user",
			expectedPassword: "pass",
			expectedError:    nil,
		},
		{
			name:             "non-existant creds",
			url:              "ghcr.io",
			expectedUsername: "",
			expectedPassword: "",
			expectedError:    fmt.Errorf("no key for ghcr.io"),
		},
		{
			name:             "no username",
			url:              "nouser.io",
			expectedUsername: "",
			expectedPassword: "",
			expectedError:    fmt.Errorf("no key for username"),
		},
		{
			name:             "no password",
			url:              "nopass.io",
			expectedUsername: "",
			expectedPassword: "",
			expectedError:    fmt.Errorf("no key for password"),
		},
		{
			name:             "url credentials is not a map",
			url:              "notamap.io",
			expectedUsername: "",
			expectedPassword: "",
			expectedError:    fmt.Errorf("[] is not a map"),
		},
		{
			name:             "username is not a string",
			url:              "notastring.io",
			expectedUsername: "",
			expectedPassword: "",
			expectedError:    fmt.Errorf("[] is not a string"),
		},
	} {

		username, password, err := helper.Get(test.url)
		assert.Equal(t, test.expectedUsername, username)
		assert.Equal(t, test.expectedPassword, password)
		assert.Equal(t, test.expectedError, err)
	}
}

func TestList(t *testing.T) {
	client, err := NewClient(validHttpClient)
	assert.NoError(t, err)
	helper := Helper{client}
	res, err := helper.List()
	assert.NoError(t, err)
	assert.Equal(t, map[string]string{"public.ecr.aws": "123456789012", "docker.io": "user"}, res)
}

func TestAdd(t *testing.T) {
	client, err := NewClient(httpClient)
	assert.NoError(t, err)
	helper := Helper{client}
	err = helper.Add(&credentials.Credentials{"public.ecr.aws", "123456789012", "ecr_token"})
	assert.Equal(t, errNotImplemented, err)
}
func TestDelete(t *testing.T) {
	client, err := NewClient(httpClient)
	assert.NoError(t, err)
	helper := Helper{client}
	err = helper.Delete("public.ecr.aws")
	assert.Equal(t, errNotImplemented, err)
}
