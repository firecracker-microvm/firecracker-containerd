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

// Response to the proxy network address resolver.
type Response struct {
	// Network type used in net.Dial.
	//
	// Reference: https://pkg.go.dev/net#Dial
	Network string `json:"network"`

	// Network address used in net.Dial.
	Address string `json:"address"`

	// SnapshotterPort is the port used in vsock.DialContext for sending snapshotter API requests to the remote snapshotter.
	SnapshotterPort string `json:"snapshotter_port"`

	// MetricsPort is the port used in vsock.DialContext for sending metrics requests to the remote snapshotter.
	MetricsPort string `json:"metrics_port"`

	// Labels is a map used for applying labels to metrics.
	Labels map[string]string `json:"labels"`
}

// Resolver for the proxy network address.
type Resolver interface {
	// Fetch the network address used to forward snapshotter
	// requests by namespace lookup from a remote service call.
	Get(namespace string) (Response, error)
}
