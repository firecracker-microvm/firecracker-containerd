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
	"flag"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/containerd/containerd/events/exchange"
	"github.com/containerd/containerd/log"
	"github.com/containerd/containerd/namespaces"
	taskAPI "github.com/containerd/containerd/runtime/v2/task"
	"github.com/containerd/containerd/sys/reaper"
	"github.com/containerd/ttrpc"
	"github.com/opencontainers/runc/libcontainer/system"
	"github.com/sirupsen/logrus"
	"golang.org/x/sync/errgroup"
	"golang.org/x/sys/unix"

	"github.com/firecracker-microvm/firecracker-containerd/eventbridge"
	"github.com/firecracker-microvm/firecracker-containerd/internal/event"
	"github.com/firecracker-microvm/firecracker-containerd/internal/vm"

	drivemount "github.com/firecracker-microvm/firecracker-containerd/proto/service/drivemount/ttrpc"
)

const (
	defaultPort      = 10789
	defaultNamespace = namespaces.Default

	// per prctl(2), we must provide a non-zero arg when calling prctl with
	// PR_SET_CHILD_SUBREAPER in order to enable subreaping (0 disables it)
	enableSubreaper = 1
)

var (
	revision string
)

func main() {
	var (
		port    int
		debug   bool
		version bool
	)

	flag.IntVar(&port, "port", defaultPort, "Vsock port to listen to")
	flag.BoolVar(&debug, "debug", false, "Turn on debug mode")
	flag.BoolVar(&version, "version", false, "Show the version")
	flag.Parse()

	if debug {
		logrus.SetLevel(logrus.DebugLevel)
	}

	if version {
		showVersion()
		return
	}

	signals := make(chan os.Signal, 32)
	signal.Notify(signals, syscall.SIGINT, syscall.SIGTERM, unix.SIGCHLD)

	shimCtx, shimCancel := context.WithCancel(namespaces.WithNamespace(context.Background(), defaultNamespace))
	group, shimCtx := errgroup.WithContext(shimCtx)

	// Ensure this process is a subreaper or else containers created via runc will
	// not be its children.
	if err := system.SetSubreaper(enableSubreaper); err != nil {
		log.G(shimCtx).WithError(err).Fatal("failed to set shim as subreaper")
	}

	// Create a runc task service that can be used via GRPC.
	// This can be wrapped to add missing functionality (like
	// running multiple containers inside one Firecracker VM)

	log.G(shimCtx).Info("creating task service")

	server, err := ttrpc.NewServer()
	if err != nil {
		log.G(shimCtx).WithError(err).Fatal("failed to create ttrpc server")
	}

	eventExchange := &event.ExchangeCloser{Exchange: exchange.NewExchange()}
	eventbridge.RegisterGetterService(server, eventbridge.NewGetterService(shimCtx, eventExchange))

	taskService, err := NewTaskService(shimCtx, shimCancel, eventExchange)
	if err != nil {
		log.G(shimCtx).WithError(err).Fatal("failed to create task service")
	}
	taskAPI.RegisterTaskService(server, taskService)

	dh, err := newDriveHandler(blockPath, drivePath)
	if err != nil {
		log.G(shimCtx).WithError(err).Fatal("failed to create drive handler")
	}
	drivemount.RegisterDriveMounterService(server, dh)

	// Run ttrpc over vsock

	vsockLogger := log.G(shimCtx).WithField("port", port)
	listener, err := vm.VSockListener(shimCtx, vsockLogger, uint32(port))
	if err != nil {
		log.G(shimCtx).WithError(err).Fatalf("failed to listen to vsock on port %d", port)
	}

	group.Go(func() error {
		err := server.Serve(shimCtx, listener)
		if err == ttrpc.ErrServerClosed {
			// Calling server.Shutdown() from another goroutine will cause ErrServerClosed, which is fine.
			return nil
		}
		return err
	})

	group.Go(func() error {
		defer func() {
			log.G(shimCtx).Info("stopping ttrpc server")
			// use context.Background() instead of the canceled shimCtx, which should allow shutdown to
			// flush any pending responses before actually stopping the server. We're choosing to not
			// use any timeouts here, deferring instead to the the higher-level runtime shim's shutdown
			// timeouts. If we get blocked here, the runtime shim will eventually force the VM to shutdown
			// after its timeouts.
			if err := server.Shutdown(context.Background()); err != nil {
				log.G(shimCtx).WithError(err).Errorf("failed to close ttrpc server")
			}
		}()

		for {
			select {
			case s := <-signals:
				switch s {
				case unix.SIGCHLD:
					if err := reaper.Reap(); err != nil {
						log.G(shimCtx).WithError(err).Error("reap error")
					}
				case syscall.SIGINT, syscall.SIGTERM:
					shimCancel()
					return nil
				}
			case <-shimCtx.Done():
				return shimCtx.Err()
			}
		}
	})

	err = group.Wait()
	log.G(shimCtx).Info("shutting down agent")

	if err != nil && err != context.Canceled {
		log.G(shimCtx).WithError(err).Error("shim error")
		panic(err)
	}
}

func showVersion() {
	// Once https://github.com/golang/go/issues/29814 is resolved,
	// we can use runtime/debug.BuildInfo instead of calling git(1) from Makefile
	fmt.Printf("containerd Firecracker agent (git commit: %s)\n", revision)
}
