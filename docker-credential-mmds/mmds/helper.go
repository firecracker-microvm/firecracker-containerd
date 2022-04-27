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
	"errors"
	"fmt"
	"net/http"

	"github.com/docker/docker-credential-helpers/credentials"
)

// Helper implements the docker credential helper interface
// with backing from firecracker's MMDS.
// It expects MMDS to contain metadata under the docker-credentials key in the following format:
// "docker-credentials": {
//   "public.ecr.aws": {
//     "username": "user",
//     "password": "pass"
//   },
//   "docker.io": {
//     "username": "user2",
//     "password": "pass2"
//   }
// }
type Helper struct {
	client *Client
}

var _ credentials.Helper = (*Helper)(nil)

func NewHelper() (*Helper, error) {
	mmdsClient, err := NewClient(http.DefaultClient)
	if err != nil {
		return nil, err
	}
	return &Helper{
		mmdsClient,
	}, nil
}

var errNotImplemented error = errors.New("not implemented")

func (h *Helper) Add(c *credentials.Credentials) error {
	return errNotImplemented
}

func (h *Helper) Delete(serverURL string) error {
	return errNotImplemented
}

func (h *Helper) Get(serverUrl string) (string, string, error) {
	allCredentials, err := h.getCredentialMetadata()
	if err != nil {
		return "", "", err
	}
	credentials, err := getMap(allCredentials, serverUrl)
	if err != nil {
		return "", "", err
	}
	username, err := getString(credentials, "username")
	if err != nil {
		return "", "", err
	}
	password, err := getString(credentials, "password")
	if err != nil {
		return "", "", err
	}

	return username, password, nil
}

func (h *Helper) List() (map[string]string, error) {
	allCredentials, err := h.getCredentialMetadata()
	if err != nil {
		return nil, err
	}
	result := make(map[string]string)
	for serverUrl := range allCredentials {
		credentials, err := getMap(allCredentials, serverUrl)
		if err != nil {
			return nil, err
		}
		username, err := getString(credentials, "username")
		if err != nil {
			return nil, err
		}
		result[serverUrl] = username
	}
	return result, nil
}

func (h *Helper) getCredentialMetadata() (map[string]interface{}, error) {
	return h.client.GetMetadata("docker-credentials")
}

func getMap(m map[string]interface{}, key string) (map[string]interface{}, error) {
	val, ok := m[key]
	if !ok {
		return nil, fmt.Errorf("no key for %s", key)
	}
	valMap, ok := val.(map[string]interface{})
	if !ok {
		return nil, fmt.Errorf("%s is not a map", val)
	}
	return valMap, nil
}

func getString(m map[string]interface{}, key string) (string, error) {
	val, ok := m[key]
	if !ok {
		return "", fmt.Errorf("no key for %s", key)
	}
	valMap, ok := val.(string)
	if !ok {
		return "", fmt.Errorf("%s is not a string", val)
	}
	return valMap, nil
}
