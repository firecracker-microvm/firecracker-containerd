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
	"encoding/json"
	"fmt"
	"io"
	"io/ioutil"
	"net/http"
)

// HTTPClient defines the interface for the client used
// for HTTP communications in the resolver.
type HTTPClient interface {
	Get(string) (*http.Response, error)
}

// ResponseReader defines the reading function interface.
type ResponseReader func(io.Reader) ([]byte, error)

// HTTPResolver implements a proxy address resolver via HTTP.
type HTTPResolver struct {
	url    string
	client HTTPClient
}

// NewHTTPResolver creates a new instance of HttpResolver with specified the URL.
func NewHTTPResolver(url string) HTTPResolver {
	return HTTPResolver{url: url, client: http.DefaultClient}
}

func requestURL(url string, namespace string) string {
	return fmt.Sprintf("%s/address?namespace=%s", url, namespace)
}

// Get queries the proxy network type and address for the specified namespace.
func (h HTTPResolver) Get(namespace string) (Response, error) {
	httpResponse, err := h.client.Get(requestURL(h.url, namespace))
	if err != nil {
		return Response{}, err
	}
	defer httpResponse.Body.Close()

	body, err := ioutil.ReadAll(httpResponse.Body)
	if err != nil {
		return Response{}, err
	}

	var response Response
	err = json.Unmarshal(body, &response)
	if err != nil {
		return Response{}, err
	}

	return response, nil
}
