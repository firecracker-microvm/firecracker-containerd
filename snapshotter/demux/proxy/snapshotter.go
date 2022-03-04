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
	"github.com/sirupsen/logrus"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

// SnapshotterDialer defines an interface for establishing a network connection.
type SnapshotterDialer = func(context.Context, *logrus.Entry, string, uint32)

// NewProxySnapshotter creates a proxy snapshotter using gRPC over vsock connection.
func NewProxySnapshotter(ctx context.Context, address string, dialer func(context.Context, string) (net.Conn, error)) (snapshots.Snapshotter, error) {
	opts := []grpc.DialOption{
		grpc.WithContextDialer(dialer),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithBlock(),
	}
	gRPCConn, err := grpc.DialContext(ctx, address, opts...)
	if err != nil {
		return nil, err
	}
	return proxy.NewSnapshotter(snapshotsapi.NewSnapshotsClient(gRPCConn), address), nil
}
