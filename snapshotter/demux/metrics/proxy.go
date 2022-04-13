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
	"context"
	"io"
	"net"
	"net/http"
	"strconv"

	"github.com/containerd/containerd/log"
)

const (
	// RequestPath is the path in the microVM hosting remote snapshotter metrics.
	RequestPath = "http://localhost/metrics"
	// MaxMetricsResponseSize is the limit in bytes for size of a metrics response from a remote snapshotter.
	MaxMetricsResponseSize = 32768 // 32 KB
)

// Proxy represents a metrics proxy server for getting and serving remote snapshotter metrics.
type Proxy struct {
	// client is the HTTP client used to send metrics requests to a remote snapshotter.
	client *http.Client
	// server is the proxy server on host for serving remote snapshotter metrics.
	server *http.Server
	// host is the nonroutable address listening for metrics requests.
	host string
	// Port is the port that the metrics proxy server listens on.
	Port int
	// Labels are labels used to apply to metrics.
	Labels map[string]string
}

// NewProxy creates a new Proxy with initialized HTTP client and port.
// dialer is used as the DialContext underlying the metrics HTTP client's RoundTripper.
//
// Reference: https://pkg.go.dev/net/http#Transport
func NewProxy(host string, port int, labels map[string]string, dialer func(context.Context, string, string) (net.Conn, error)) (*Proxy, error) {
	return &Proxy{
		client: &http.Client{
			Transport: &http.Transport{
				DialContext: dialer,
			},
		},
		server: nil,
		host:   host,
		Port:   port,
		Labels: labels,
	}, nil
}

// Serve starts the metrics proxy server for a single in-VM snapshotter.
func (mp *Proxy) Serve(ctx context.Context) error {
	mux := http.NewServeMux()
	mux.HandleFunc("/metrics", mp.metrics)

	mp.server = &http.Server{
		Addr:    mp.host + ":" + strconv.Itoa(mp.Port),
		Handler: mux,
	}

	err := mp.server.ListenAndServe()
	if err != http.ErrServerClosed {
		return err
	}

	return nil
}

// Shutdown shuts down the remote snapshotter metrics proxy server.
func (mp *Proxy) Shutdown(ctx context.Context) error {
	return mp.server.Shutdown(ctx)
}

// metrics is an HTTP handler that returns metrics for a given VM/snapshotter.
func (mp *Proxy) metrics(w http.ResponseWriter, req *http.Request) {
	ctx := req.Context()

	// Pull metrics and copy to HTTP response.
	res, err := mp.client.Get(RequestPath)
	if err != nil {
		log.G(ctx).Errorf("error reading response body: %v\n", err)
		w.WriteHeader(500)
		return
	}

	defer res.Body.Close()
	response := io.LimitReader(res.Body, MaxMetricsResponseSize)

	io.Copy(w, response)
}
