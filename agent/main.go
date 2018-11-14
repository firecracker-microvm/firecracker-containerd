package main

import (
	"context"
	"flag"
	"os"
	"os/signal"
	"syscall"

	"github.com/containerd/containerd/log"
	"github.com/containerd/containerd/runtime/v2/runc"
	"github.com/containerd/containerd/runtime/v2/shim"
	shimapi "github.com/containerd/containerd/runtime/v2/task"
	"github.com/containerd/ttrpc"
	"github.com/mdlayher/vsock"
	"github.com/sirupsen/logrus"
	"golang.org/x/sync/errgroup"
	"golang.org/x/sys/unix"
)

const defaultPort = 10789

func main() {
	var (
		id    string
		port  int
		debug bool
	)

	flag.StringVar(&id, "id", "", "Shim task id")
	flag.IntVar(&port, "port", defaultPort, "Vsock port to listen to")
	flag.BoolVar(&debug, "debug", false, "Turn on debug mode")
	flag.Parse()

	if debug {
		logrus.SetLevel(logrus.DebugLevel)
	}

	signals := make(chan os.Signal, 32)
	signal.Notify(signals, syscall.SIGINT, syscall.SIGTERM, unix.SIGCHLD)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	group, ctx := errgroup.WithContext(ctx)

	// Create a runc task service that can be used via GRPC.
	// This can be wrapped to add missing functionality (like
	// running multiple containers inside one Firecracker VM)

	log.G(ctx).WithField("id", id).Info("creating runc shim")

	runcTaskService, err := runc.New(ctx, id, nil)
	if err != nil {
		log.G(ctx).WithError(err).Error("failed to create runc shim")
		os.Exit(1)
	}

	taskService := NewTaskService(runcTaskService)

	server, err := ttrpc.NewServer()
	if err != nil {
		log.G(ctx).WithError(err).Error("failed to create ttrpc server")
		os.Exit(1)
	}

	shimapi.RegisterTaskService(server, taskService)

	// Run ttrpc over vsock

	log.G(ctx).WithField("port", port).Info("listening to vsock")

	listener, err := vsock.Listen(uint32(port))
	if err != nil {
		log.G(ctx).WithError(err).Errorf("failed to listen to vsock on port %d", port)
		os.Exit(1)
	}

	group.Go(func() error {
		// TODO: this doesn't exit after server.Close when using vsock (listener.Accept remains blocked), need ugly workaround.
		// Github issue to track: https://github.com/mdlayher/vsock/issues/19
		return server.Serve(ctx, listener)
	})

	group.Go(func() error {
		defer func() {
			log.G(ctx).Info("stopping ttrpc server")

			if err := server.Close(); err != nil {
				log.G(ctx).WithError(err).Errorf("failed to close ttrpc server")
			}
		}()

		for {
			select {
			case s := <-signals:
				switch s {
				case unix.SIGCHLD:
					if err := shim.Reap(); err != nil {
						log.G(ctx).WithError(err).Error("reap error")
					}
				case syscall.SIGINT, syscall.SIGTERM:
					cancel()
					return nil
				}
			case <-ctx.Done():
				return ctx.Err()
			}
		}
	})

	if err := group.Wait(); err != nil {
		log.G(ctx).WithError(err).Warn("shim error")
	}

	log.G(ctx).Info("done")
}
