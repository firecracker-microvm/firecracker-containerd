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
	"encoding/json"
	"errors"
	"fmt"
	"io/ioutil"
	"net/http"
	"net/url"
	"strings"
)

const (
	mmdsAddress    = "169.254.169.254"
	getTokenPath   = "/latest/api/token/"
	tokenTtlHeader = "X-metadata-token-ttl-seconds"
	tokenHeader    = "X-metadata-token"
	tokenTtl       = "21600"
)

var errUnauthorized = errors.New("Unauthorized")

type version int

const (
	v1 version = 1
	v2
)

// Client is a guest-side MMDS client
type Client struct {
	httpClient *http.Client
	version    version
	token      string
}

// NewClient creates a new MMDS client
// The MMDS client will attempt to get an MMDS v2 token,
// however if MMDS v2 is not enabled in the guest, it will
// fall back to MMDS v1 without session tokens.
func NewClient(httpClient *http.Client) (*Client, error) {
	client := Client{
		httpClient: httpClient,
		version:    v2,
		token:      "",
	}
	err := client.refreshToken()
	if err != nil {
		return nil, err
	}
	return &client, nil
}

func (c *Client) refreshToken() error {
	resp, err := c.httpClient.Do(&http.Request{
		Method: http.MethodPut,
		URL: &url.URL{
			Scheme: "http",
			Host:   mmdsAddress,
			Path:   getTokenPath,
		},
		Header: http.Header{
			tokenTtlHeader: []string{tokenTtl},
		},
	})
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusMethodNotAllowed {
		c.version = v1
		return nil
	}
	token, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return err
	}
	c.token = string(token)
	return nil
}

// GetMetadata retrieves JSON metadata from MMDS and converts it to a map
func (c *Client) GetMetadata(path string) (map[string]interface{}, error) {
	b, err := c.getMetadata(path)
	if err != nil {
		return nil, err
	}
	var result map[string]interface{}
	err = json.Unmarshal(b, &result)
	return result, err
}

func (c *Client) getMetadata(path string) ([]byte, error) {
	headers := http.Header{
		"Accept": []string{"application/json"},
	}
	if c.version == v2 {
		headers[tokenHeader] = []string{c.token}
	}
	if !strings.HasPrefix(path, "/") {
		path = "/" + path
	}
	resp, err := c.httpClient.Do(&http.Request{
		Method: http.MethodGet,
		URL: &url.URL{
			Scheme: "http",
			Host:   mmdsAddress,
			Path:   path,
		},
		Header: headers,
	})
	if err != nil {
		return []byte{}, err
	}
	defer resp.Body.Close()
	return readBody(resp)
}

func readBody(resp *http.Response) ([]byte, error) {
	switch status := resp.StatusCode; {
	case status < 300:
		return ioutil.ReadAll(resp.Body)
	case status == http.StatusUnauthorized:
		return []byte{}, errUnauthorized
	default:
		body, err := ioutil.ReadAll(resp.Body)
		return []byte{}, fmt.Errorf("unexpected http response: %d - (err %v) %s", status, err, string(body))
	}
}
