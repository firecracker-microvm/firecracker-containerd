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

package client

import (
	"fmt"

	"github.com/containerd/containerd/pkg/ttrpcutil"

	fccontrol "github.com/firecracker-microvm/firecracker-containerd/proto/service/fccontrol/ttrpc"
)

// Client is a helper client for containerd's firecracker-control plugin
type Client struct {
	fccontrol.FirecrackerService

	ttrpcClient *ttrpcutil.Client
}

// New creates a new firecracker-control service client
func New(ttrpcAddress string) (*Client, error) {
	ttrpcClient, err := ttrpcutil.NewClient(ttrpcAddress)
	if err != nil {
		return nil, fmt.Errorf("failed to create ttrpc client: %w", err)
	}

	client, err := ttrpcClient.Client()
	if err != nil {
		return nil, err
	}
	fcClient := fccontrol.NewFirecrackerClient(client)

	return &Client{
		FirecrackerService: fcClient,
		ttrpcClient:        ttrpcClient,
	}, nil
}

// Close closes the underlying TTRPC client
func (c *Client) Close() error {
	return c.ttrpcClient.Close()
}
