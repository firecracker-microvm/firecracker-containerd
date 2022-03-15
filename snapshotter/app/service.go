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

package app

import (
	"context"
	"fmt"
	"net"
	"os"
	"os/signal"
	"strconv"
	"syscall"

	snapshotsapi "github.com/containerd/containerd/api/services/snapshots/v1"
	"github.com/containerd/containerd/contrib/snapshotservice"
	"github.com/containerd/containerd/log"
	"github.com/containerd/containerd/snapshots"
	"github.com/firecracker-microvm/firecracker-go-sdk/vsock"
	"github.com/sirupsen/logrus"
	"golang.org/x/sync/errgroup"
	"google.golang.org/grpc"

	"github.com/firecracker-microvm/firecracker-containerd/snapshotter/config"
	"github.com/firecracker-microvm/firecracker-containerd/snapshotter/demux"
	"github.com/firecracker-microvm/firecracker-containerd/snapshotter/demux/cache"
	"github.com/firecracker-microvm/firecracker-containerd/snapshotter/demux/proxy"
	proxyaddress "github.com/firecracker-microvm/firecracker-containerd/snapshotter/demux/proxy/address"
)

// Run the demultiplexing snapshotter service.
//
// The snapshotter server will be running on the
// network address and port specified in listener config.
func Run(config config.Config) error {
	stop := make(chan os.Signal, 1)
	signal.Notify(stop, syscall.SIGINT, syscall.SIGTERM, syscall.SIGPIPE, syscall.SIGHUP, syscall.SIGQUIT)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	group, ctx := errgroup.WithContext(ctx)

	snapshotter, err := initSnapshotter(ctx, config)
	if err != nil {
		log.G(ctx).WithFields(
			logrus.Fields{"resolver": config.Snapshotter.Proxy.Address.Resolver.Type},
		).WithError(err).Fatal("failed creating socket resolver")
		return err
	}

	grpcServer := grpc.NewServer()
	service := snapshotservice.FromSnapshotter(snapshotter)
	snapshotsapi.RegisterSnapshotsServer(grpcServer, service)

	listenerConfig := config.Snapshotter.Listener
	listener, err := net.Listen(listenerConfig.Network, listenerConfig.Address)
	if err != nil {
		log.G(ctx).WithFields(
			logrus.Fields{
				"network": listenerConfig.Network,
				"address": listenerConfig.Address,
			},
		).WithError(err).Fatal("failed creating listener")
		return err
	}

	group.Go(func() error {
		return grpcServer.Serve(listener)
	})

	group.Go(func() error {
		defer func() {
			log.G(ctx).Info("stopping server")
			grpcServer.Stop()

			if err := snapshotter.Close(); err != nil {
				log.G(ctx).WithError(err).Error("failed to close snapshotter")
			}
		}()

		for {
			select {
			case <-stop:
				cancel()
				return nil
			case <-ctx.Done():
				return ctx.Err()
			}
		}
	})

	if err := group.Wait(); err != nil {
		log.G(ctx).WithError(err).Error("demux snapshotter error")
		return err
	}

	log.G(ctx).Info("done")
	return nil
}

func initResolver(config config.Config) (proxyaddress.Resolver, error) {
	resolverConfig := config.Snapshotter.Proxy.Address.Resolver
	switch resolverConfig.Type {
	case "http":
		return proxyaddress.NewHTTPResolver(resolverConfig.Address), nil
	default:
		return nil, fmt.Errorf("invalid resolver type: %s", resolverConfig.Type)
	}
}

const base10 = 10
const bits32 = 32

func initSnapshotter(ctx context.Context, config config.Config) (snapshots.Snapshotter, error) {
	resolver, err := initResolver(config)
	if err != nil {
		return nil, err
	}

	newProxySnapshotterFunc := func(ctx context.Context, namespace string) (snapshots.Snapshotter, error) {
		r := resolver
		response, err := r.Get(namespace)
		if err != nil {
			return nil, err
		}
		host, portstr, err := net.SplitHostPort(response.Address)
		if err != nil {
			return nil, err
		}
		port, err := strconv.ParseUint(portstr, base10, bits32)
		if err != nil {
			return nil, err
		}
		snapshotterDialer := func(ctx context.Context, namespace string) (net.Conn, error) {
			return vsock.DialContext(ctx, host, uint32(port), vsock.WithLogger(log.G(ctx)))
		}
		return proxy.NewProxySnapshotter(ctx, host, snapshotterDialer)
	}

	return demux.NewSnapshotter(cache.NewSnapshotterCache(), newProxySnapshotterFunc), nil
}
