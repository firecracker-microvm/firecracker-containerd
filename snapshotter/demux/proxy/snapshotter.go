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

package proxy

import (
	"context"
	"net"

	snapshotsapi "github.com/containerd/containerd/api/services/snapshots/v1"
	"github.com/containerd/containerd/snapshots"
	"github.com/containerd/containerd/snapshots/proxy"
	"github.com/firecracker-microvm/firecracker-containerd/snapshotter/demux/metrics"
	"github.com/hashicorp/go-multierror"
	"github.com/sirupsen/logrus"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

// SnapshotterDialer defines an interface for establishing a network connection.
type SnapshotterDialer = func(context.Context, *logrus.Entry, string, uint32)

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

// NewProxySnapshotter creates a proxy snapshotter using gRPC over vsock connection.
func NewProxySnapshotter(ctx context.Context, address string,
	dialer func(context.Context, string) (net.Conn, error), metricsProxy *metrics.Proxy) (*RemoteSnapshotter, error) {

	opts := []grpc.DialOption{
		grpc.WithContextDialer(dialer),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithBlock(),
	}
	gRPCConn, err := grpc.DialContext(ctx, address, opts...)
	if err != nil {
		return nil, err
	}

	// TODO (ginglis13)
	// we should be monitoring this goroutine's status.
	// https://github.com/firecracker-microvm/firecracker-containerd/issues/607
	if metricsProxy != nil {
		go metricsProxy.Serve(ctx)
	}

	return &RemoteSnapshotter{proxy.NewSnapshotter(snapshotsapi.NewSnapshotsClient(gRPCConn), address), metricsProxy}, nil
}

// MetricsProxyPort returns the metrics proxy port for a remote snapshotter.
func (rs *RemoteSnapshotter) MetricsProxyPort() int {
	return rs.metricsProxy.Port

}

// MetricsProxyLabels returns the metrics labels for a remote snapshotter.
func (rs *RemoteSnapshotter) MetricsProxyLabels() map[string]string {
	return rs.metricsProxy.Labels
}
