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

package discovery

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"

	"github.com/containerd/containerd/log"
	"github.com/firecracker-microvm/firecracker-containerd/snapshotter/demux/cache"
)

// ServiceDiscovery discovers metrics services in the snapshotter cache
// and serves a []MetricsTarget on an HTTP server.
type ServiceDiscovery struct {
	// cache is the shared host-level cache of snapshotters also used by the demux snapshotter.
	cache cache.Cache
	// server is the HTTP server that returns a list of targets to pull metrics from and apply labels to those metrics.
	server *http.Server
}

// NewServiceDiscovery returns a ServiceDiscovery with configured HTTP server and provided cache.
func NewServiceDiscovery(host string, port int, c cache.Cache) *ServiceDiscovery {
	return &ServiceDiscovery{
		server: &http.Server{
			Addr: host + ":" + strconv.Itoa(port),
		},
		cache: c,
	}
}

// Serve starts the HTTP server for receiving service discovery requests.
func (sd *ServiceDiscovery) Serve() error {
	sd.server.Handler = http.HandlerFunc(sd.serviceDiscoveryHandler)
	err := sd.server.ListenAndServe()
	if err != http.ErrServerClosed {
		return err
	}

	return nil
}

// Shutdown shuts down the service discovery HTTP server.
func (sd *ServiceDiscovery) Shutdown(ctx context.Context) error {
	return sd.server.Shutdown(ctx)
}

// metricsTarget represents the underlying JSON structure
// that a Prometheus HTTP Service Discovery server is
// expected to return.
//
// https://prometheus.io/docs/prometheus/latest/http_sd/
type metricsTarget struct {
	Targets []string          `json:"targets"`
	Labels  map[string]string `json:"labels"`
}

// serviceDiscoveryHandler scans the cache for snapshotters and their proxy server information,
// and builds and returns a JSON response in the format of a Prometheus service discovery endpoint.
func (sd *ServiceDiscovery) serviceDiscoveryHandler(w http.ResponseWriter, req *http.Request) {
	ctx := req.Context()

	namespaces := sd.cache.List()
	services := []metricsTarget{}
	for _, ns := range namespaces {
		cachedSnapshotter, err := sd.cache.Get(ctx, ns, nil)
		if err != nil {
			log.G(ctx).Error("unable to retrieve from snapshotter cache: ", err)
			continue
		}

		// make target "localhost:{PORT}" -> the metrics proxy for given snapshotter
		targetString := fmt.Sprintf("localhost:%v", cachedSnapshotter.MetricsProxyPort())
		target := []string{targetString}

		// build list of discovered services
		mt := metricsTarget{Targets: target, Labels: cachedSnapshotter.MetricsProxyLabels()}
		services = append(services, mt)
	}

	w.Header().Set("Content-Type", "application/json")

	response, err := json.Marshal(services)
	if err != nil {
		log.G(ctx).Error("unable to marshal service discovery response: ", err)
		w.WriteHeader(500)
		return
	}

	w.Write(response)
}
