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
	"fmt"
	"io"
	"net"
	"net/http"
	"strconv"
	"sync"

	"github.com/containerd/containerd/log"
)

const (
	// RequestPath is the path in the microVM hosting remote snapshotter metrics.
	RequestPath = "http://localhost/metrics"
	// MaxMetricsResponseSize is the limit in bytes for size of a metrics response from a remote snapshotter.
	MaxMetricsResponseSize = 32768 // 32 KB
)

// Monitor is used for port and lifecycle management of metrics proxies.
type Monitor struct {
	// ch is used for communicating when a metrics proxy and its port are no longer in use
	// so that the Monitor can mark the port as free.
	ch chan int
	// portsInUse is a set that tracks used and unused metrics proxy server ports.
	portsInUse map[int]bool
	// startPort is first port in the configurable range of ports to serve metrics proxies from.
	startPort int
	// endPort is last port in the configurable range of ports to serve metrics proxies from.
	endPort int
	// mu is the lock used for threadsafe reading and writing of the metrics proxy port map.
	mu *sync.Mutex
}

// NewMonitor returns a new metrics proxy monitor.
func NewMonitor(startPort, endPort int) (*Monitor, error) {
	if startPort <= 0 || endPort <= 0 || startPort > endPort {
		return nil, fmt.Errorf("invalid port range %d-%d", startPort, endPort)
	}
	return &Monitor{make(chan int), make(map[int]bool), startPort, endPort, &sync.Mutex{}}, nil
}

// Start receives messages from the proxy server channel indiciating a server has closed or terminated.
// It marks the metrics proxy server's port as available by removing it from the portsInUse set.
func (m *Monitor) Start() error {
	for freePort := range m.ch {
		m.mu.Lock()
		delete(m.portsInUse, freePort)
		m.mu.Unlock()
	}

	return nil
}

// Stop closes the metrics proxy monitoring channel.
func (m *Monitor) Stop() {
	close(m.ch)
}

// findOpenPort returns a port for use by a metrics proxy server.
func (m *Monitor) findOpenPort() (int, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	metricsProxyPort := -1
	for p := m.startPort; p <= m.endPort; p++ {
		if _, ok := m.portsInUse[p]; !ok {
			// Mark the port as in use in the set
			metricsProxyPort = p
			m.portsInUse[p] = true
			break
		}
	}

	if metricsProxyPort < 0 {
		return 0, fmt.Errorf("unable to find port in range %d-%d", m.startPort, m.endPort)
	}

	return metricsProxyPort, nil
}

// HTTPClient defines the interface for the client getting metrics.
type HTTPClient interface {
	Get(string) (*http.Response, error)
}

// Proxy represents a metrics proxy server for getting and serving remote snapshotter metrics.
type Proxy struct {
	// client is the HTTP client used to send metrics requests to a remote snapshotter.
	client HTTPClient
	// server is the proxy server on host for serving remote snapshotter metrics.
	server *http.Server
	// host is the nonroutable address listening for metrics requests.
	host string
	// Port is the port that the metrics proxy server listens on.
	Port int
	// Labels are labels used to apply to metrics.
	Labels map[string]string
	// monitor is the metrics proxy monitor
	monitor *Monitor
}

// NewProxy creates a new Proxy with initialized HTTP client and port.
// dialer is used as the DialContext underlying the metrics HTTP client's RoundTripper.
//
// Reference: https://pkg.go.dev/net/http#Transport
func NewProxy(host string, monitor *Monitor, labels map[string]string, dialer func(context.Context, string, string) (net.Conn, error)) (*Proxy, error) {
	port, err := monitor.findOpenPort()
	if err != nil {
		return nil, err
	}
	return &Proxy{
		client: &http.Client{
			Transport: &http.Transport{
				DialContext: dialer,
			},
		},
		server:  nil,
		host:    host,
		Port:    port,
		Labels:  labels,
		monitor: monitor,
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
		log.G(ctx).Errorf("metrics proxy server: %v\n", err)
		mp.monitor.ch <- mp.Port
		return err
	}

	mp.monitor.ch <- mp.Port
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
		log.G(ctx).Errorf("error reading response body: %v", err)
		w.WriteHeader(500)
		return
	}

	defer res.Body.Close()
	response := io.LimitReader(res.Body, MaxMetricsResponseSize)

	_, err = io.Copy(w, response)
	if err != nil {
		log.G(ctx).Errorf("error writing response body: %v", err)
		w.WriteHeader(500)
		return
	}
}
