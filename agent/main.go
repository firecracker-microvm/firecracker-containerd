// Copyright 2018-2019 Amazon.com, Inc. or its affiliates. All Rights Reserved.
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

package main

import (
	"context"
	"errors"
	"flag"
	"os"
	"os/signal"
	"syscall"

	"github.com/containerd/containerd/events/exchange"
	"github.com/containerd/containerd/log"
	"github.com/containerd/containerd/namespaces"
	"github.com/containerd/containerd/runtime/v2/runc"
	"github.com/containerd/containerd/runtime/v2/shim"
	shimapi "github.com/containerd/containerd/runtime/v2/task"
	"github.com/containerd/containerd/sys"
	"github.com/containerd/ttrpc"
	"github.com/mdlayher/vsock"
	"github.com/sirupsen/logrus"
	"golang.org/x/sync/errgroup"
	"golang.org/x/sys/unix"

	"github.com/firecracker-microvm/firecracker-containerd/eventbridge"
)

const (
	defaultPort      = 10789
	defaultNamespace = namespaces.Default

	// per prctl(2), we must provide a non-zero arg when calling prctl with
	// PR_SET_CHILD_SUBREAPER in order to enable subreaping (0 disables it)
	enableSubreaper = 1
)

//nolint:gocyclo
func main() {
	var (
		id    string
		port  int
		debug bool
	)

	flag.StringVar(&id, "id", "", "ContainerID (required)")
	flag.IntVar(&port, "port", defaultPort, "Vsock port to listen to")
	flag.BoolVar(&debug, "debug", false, "Turn on debug mode")
	flag.Parse()

	if debug {
		logrus.SetLevel(logrus.DebugLevel)
	}

	signals := make(chan os.Signal, 32)
	signal.Notify(signals, syscall.SIGINT, syscall.SIGTERM, unix.SIGCHLD)

	ctx, cancel := context.WithCancel(namespaces.WithNamespace(context.Background(), defaultNamespace))
	defer cancel()

	group, ctx := errgroup.WithContext(ctx)

	// verify arguments, id is required
	if id == "" {
		log.G(ctx).WithError(errors.New("invalid argument")).Fatal("id not set")
	}

	// Ensure this process is a subreaper or else containers created via runc will
	// not be its children.
	if err := sys.SetSubreaper(enableSubreaper); err != nil {
		log.G(ctx).WithError(err).Fatal("failed to set shim as subreaper")
	}

	// Create a runc task service that can be used via GRPC.
	// This can be wrapped to add missing functionality (like
	// running multiple containers inside one Firecracker VM)

	log.G(ctx).WithField("id", id).Info("creating runc shim")

	eventExchange := exchange.NewExchange()
	runcTaskService, err := runc.New(ctx, id, eventExchange)
	if err != nil {
		log.G(ctx).WithError(err).Fatal("failed to create runc shim")
	}

	taskService := NewTaskService(runcTaskService, cancel)

	server, err := ttrpc.NewServer()
	if err != nil {
		log.G(ctx).WithError(err).Fatal("failed to create ttrpc server")
	}

	shimapi.RegisterTaskService(server, taskService)
	eventbridge.RegisterGetterService(server, eventbridge.NewGetterService(ctx, eventExchange))

	// Run ttrpc over vsock

	log.G(ctx).WithField("port", port).Info("listening to vsock")
	listener, err := vsock.Listen(uint32(port))
	if err != nil {
		log.G(ctx).WithError(err).Fatalf("failed to listen to vsock on port %d", port)
	}

	group.Go(func() error {
		return server.Serve(ctx, listener)
	})

	group.Go(func() error {
		defer func() {
			log.G(ctx).Info("stopping ttrpc server")
			if err := server.Shutdown(ctx); err != nil {
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

	log.G(ctx).Info("shutting down agent")
	if _, err := runcTaskService.Shutdown(ctx, &shimapi.ShutdownRequest{ID: id, Now: true}); err != nil {
		log.G(ctx).WithError(err).Error("runc shutdown error")
	}
}
