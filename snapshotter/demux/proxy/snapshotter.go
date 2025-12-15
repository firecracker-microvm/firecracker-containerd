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

// Package proxy implements the proxy snapshotter used by the demux
// snapshotter to route requests to remote snapshotter backends.
package proxy

import (
	"context"
	"net"
	"time"

	snapshotsapi "github.com/containerd/containerd/api/services/snapshots/v1"
	"github.com/containerd/containerd/snapshots"
	"github.com/containerd/containerd/snapshots/proxy"
	"github.com/firecracker-microvm/firecracker-containerd/snapshotter/demux/metrics"
	"github.com/hashicorp/go-multierror"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

// Dialer captures commonly grouped dial functionality and configuration.
type Dialer struct {
	// Dial is a function used to establish a network connection to a remote snapshotter.
	Dial func(context.Context, string) (net.Conn, error)

	// Timeout is the time required to establish a connection to a remote snapshotter.
	Timeout time.Duration
}

// RemoteSnapshotter embeds a snapshots.Snapshotter and its metrics proxy.
type RemoteSnapshotter struct {
	snapshots.Snapshotter
	metricsProxy *metrics.Proxy
}

// Close closes the remote snapshotter's snapshotter and shuts down its metrics proxy server.
func (rs *RemoteSnapshotter) Close() error {
	var compiledErr error
	if err := rs.Snapshotter.Close(); err != nil {
		compiledErr = multierror.Append(compiledErr, err)
	}
	if rs.metricsProxy != nil {
		if err := rs.metricsProxy.Shutdown(context.Background()); err != nil {
			compiledErr = multierror.Append(compiledErr, err)
		}
	}

	return compiledErr
}

// NewRemoteSnapshotter creates a proxy snapshotter using gRPC over vsock connection.
func NewRemoteSnapshotter(ctx context.Context, address string,
	dialer func(context.Context, string) (net.Conn, error), metricsProxy *metrics.Proxy) (*RemoteSnapshotter, error) {

	opts := []grpc.DialOption{
		grpc.WithContextDialer(dialer),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	}
	gRPCConn, err := grpc.NewClient(address, opts...)
	if err != nil {
		return nil, err
	}

	// Treat metrics proxy errors as operational errors, logged by the server itself.
	if metricsProxy != nil {
		go metricsProxy.Serve(ctx)
	}

	return &RemoteSnapshotter{proxy.NewSnapshotter(snapshotsapi.NewSnapshotsClient(gRPCConn), address), metricsProxy}, nil
}

// Cleanup implements the Cleaner interface for snapshotters.
// This enables asynchronous resource cleanup by remote snapshotters.
//
// See https://github.com/containerd/containerd/blob/v1.6.4/snapshots/snapshotter.go
func (rs *RemoteSnapshotter) Cleanup(ctx context.Context) error {
	return rs.Snapshotter.(snapshots.Cleaner).Cleanup(ctx)
}

// MetricsProxyPort returns the metrics proxy port for a remote snapshotter.
func (rs *RemoteSnapshotter) MetricsProxyPort() int {
	return rs.metricsProxy.Port

}

// MetricsProxyLabels returns the metrics labels for a remote snapshotter.
func (rs *RemoteSnapshotter) MetricsProxyLabels() map[string]string {
	return rs.metricsProxy.Labels
}
