package main

import (
	"context"
	"github.com/awslabs/containerd-firecracker/snapshotter"
	"google.golang.org/grpc"
	"net"
	"os"

	snapshotsapi "github.com/containerd/containerd/api/services/snapshots/v1"
	"github.com/containerd/containerd/contrib/snapshotservice"
	"github.com/containerd/containerd/log"
)

func main() {
	if len(os.Args) < 3 {
		log.L.Fatalf("invalid args: usage %s <unix addr> <root>", os.Args[0])
	}

	var unixAddr, rootPath = os.Args[1], os.Args[2]

	ctx := context.Background()
	rpc := grpc.NewServer()

	snap, err := snapshotter.NewSnapshotter(ctx, rootPath)
	if err != nil {
		log.G(ctx).WithError(err).Fatal("failed to create snapshotter")
	}

	// Convert the snapshotter interface to gRPC service and run server
	log.G(ctx).WithField("unix_addr", unixAddr).Info("running gRPC server")
	service := snapshotservice.FromSnapshotter(snap)
	snapshotsapi.RegisterSnapshotsServer(rpc, service)

	listener, err := net.Listen("unix", unixAddr)
	if err != nil {
		log.G(ctx).WithError(err).Fatalf("failed to listen socket at %s", os.Args[1])
	}

	defer listener.Close()

	if err := rpc.Serve(listener); err != nil {
		log.G(ctx).WithError(err).Fatal("failed to run gRPC server")
	}
}
