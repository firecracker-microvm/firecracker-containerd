// Copyright 2018 Amazon.com, Inc. or its affiliates. All Rights Reserved.
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

package snapshotter

import (
	"context"
	"flag"
	"net"
	"os"
	"os/signal"
	"syscall"

	snapshotsapi "github.com/containerd/containerd/api/services/snapshots/v1"
	"github.com/containerd/containerd/contrib/snapshotservice"
	"github.com/containerd/containerd/log"
	"github.com/containerd/containerd/snapshots"
	"github.com/sirupsen/logrus"
	"golang.org/x/sync/errgroup"
	"google.golang.org/grpc"
)

var (
	unixAddr string
	debug    bool
)

func init() {
	flag.StringVar(&unixAddr,
		"address",
		"./firecracker-snapshotter.sock",
		"RPC server unix address (default: ./firecracker-snapshotter.sock)")

	flag.BoolVar(&debug,
		"debug",
		false,
		"Debug mode")
}

// CreateFunc represents a callback to be used for creating concrete snapshotter implementation
type CreateFunc func(ctx context.Context) (snapshots.Snapshotter, error)

// Run runs snapshotter ttrpc server for containerd (somewhat similar to shim.Run).
// snapInit should create concrete snapshotter implementation such as naive or devmapper.
// There are two command line parameters available out of the box:
// - address: specifies unix address to run ttrpc server on
// - debug: turns on debug logging
// Any extra flags might me specified if additional configuration needed, flags.Parse will
// be called prior snapshot create callback (see naive example).
func Run(snapInit CreateFunc) {
	if !flag.Parsed() {
		flag.Parse()
	}

	if debug {
		logrus.SetLevel(logrus.DebugLevel)
	}

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, syscall.SIGINT, syscall.SIGTERM, syscall.SIGPIPE, syscall.SIGHUP, syscall.SIGQUIT)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	group, ctx := errgroup.WithContext(ctx)

	rpc := grpc.NewServer()

	snap, err := snapInit(ctx)
	if err != nil {
		log.G(ctx).WithError(err).Fatal("failed to create snapshotter")
	}

	// Convert the snapshotter interface to gRPC service and run server
	log.G(ctx).WithField("unix_addr", unixAddr).Info("running gRPC server")
	service := snapshotservice.FromSnapshotter(snap)
	snapshotsapi.RegisterSnapshotsServer(rpc, service)

	listener, err := net.Listen("unix", unixAddr)
	if err != nil {
		log.G(ctx).WithError(err).Fatalf("failed to listen socket at %s", unixAddr)
	}

	group.Go(func() error {
		return rpc.Serve(listener)
	})

	group.Go(func() error {
		defer func() {
			log.G(ctx).Info("stopping  server")
			rpc.Stop()

			if err := snap.Close(); err != nil {
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
		log.G(ctx).WithError(err).Warn("snapshotter error")
	}

	log.G(ctx).Info("done")
}
