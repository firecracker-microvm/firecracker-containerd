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
	"strings"
	"syscall"
	"time"

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
	"github.com/firecracker-microvm/firecracker-containerd/snapshotter/demux/metrics"
	"github.com/firecracker-microvm/firecracker-containerd/snapshotter/demux/metrics/discovery"
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

	cache := cache.NewSnapshotterCache()

	var (
		monitor          *metrics.Monitor
		serviceDiscovery *discovery.ServiceDiscovery
	)
	if config.Snapshotter.Metrics.Enable {
		sdHost := config.Snapshotter.Metrics.Host
		sdPort := config.Snapshotter.Metrics.ServiceDiscoveryPort
		serviceDiscovery = discovery.NewServiceDiscovery(sdHost, sdPort, cache)
		var err error
		monitor, err = initMetricsProxyMonitor(config.Snapshotter.Metrics.PortRange)
		if err != nil {
			log.G(ctx).WithError(err).Fatal("failed creating metrics proxy monitor")
			return err
		}
		group.Go(func() error {
			return serviceDiscovery.Serve()
		})
		group.Go(func() error {
			return monitor.Start()
		})
	}

	snapshotter, err := initSnapshotter(ctx, config, cache, monitor)
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
				// cancelling context will cause shutdown to fail; shutdown before cancel
				if config.Snapshotter.Metrics.Enable {
					if err := serviceDiscovery.Shutdown(ctx); err != nil {
						log.G(ctx).WithError(err).Error("failed to shutdown service discovery server")
					}
					// Senders to this channel would panic if it is closed. However snapshotter.Close() will
					// shutdown all metrics proxies and ensure there are no more senders over the channel.
					monitor.Stop()
				}
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

type dialerTimeouts struct {
	DialTimeout       time.Duration
	RetryTimeout      time.Duration
	RetryInterval     time.Duration
	ConnectMsgTimeout time.Duration
	AckMsgTimeout     time.Duration
}

func parseDialerTimeouts(config config.Config) (dialerTimeouts, error) {
	dialTimeout, err := time.ParseDuration(config.Snapshotter.Dialer.DialTimeout)
	if err != nil {
		return dialerTimeouts{}, fmt.Errorf("Failed parsing dialer dial timeout: %w", err)
	}
	retryTimeout, err := time.ParseDuration(config.Snapshotter.Dialer.RetryTimeout)
	if err != nil {
		return dialerTimeouts{}, fmt.Errorf("Failed parsing dialer retry timeout: %w", err)
	}
	retryInterval, err := time.ParseDuration(config.Snapshotter.Dialer.RetryInterval)
	if err != nil {
		return dialerTimeouts{}, fmt.Errorf("Failed parsing dialer retry interval: %w", err)
	}
	connectMsgTimeout, err := time.ParseDuration(config.Snapshotter.Dialer.ConnectMsgTimeout)
	if err != nil {
		return dialerTimeouts{}, fmt.Errorf("Failed parsing dialer connect msg timeout: %w", err)
	}
	ackMsgTimeout, err := time.ParseDuration(config.Snapshotter.Dialer.AckMsgTimeout)
	if err != nil {
		return dialerTimeouts{}, fmt.Errorf("Failed parsing dialer ack msg timeout: %w", err)
	}
	return dialerTimeouts{
		DialTimeout:       dialTimeout,
		RetryTimeout:      retryTimeout,
		RetryInterval:     retryInterval,
		ConnectMsgTimeout: connectMsgTimeout,
		AckMsgTimeout:     ackMsgTimeout,
	}, nil
}

const base10 = 10
const bits32 = 32

func initSnapshotter(ctx context.Context, config config.Config, cache cache.Cache, monitor *metrics.Monitor) (snapshots.Snapshotter, error) {
	resolver, err := initResolver(config)
	if err != nil {
		return nil, err
	}

	dialerTimeouts, err := parseDialerTimeouts(config)
	if err != nil {
		return nil, err
	}

	newRemoteSnapshotterFunc := func(ctx context.Context, namespace string) (*proxy.RemoteSnapshotter, error) {
		r := resolver
		response, err := r.Get(namespace)
		if err != nil {
			return nil, err
		}
		host := response.Address
		port, err := strconv.ParseUint(response.SnapshotterPort, base10, bits32)
		if err != nil {
			return nil, err
		}

		snapshotterDialer := func(ctx context.Context, namespace string) (net.Conn, error) {
			return vsock.DialContext(ctx, host, uint32(port),
				vsock.WithLogger(log.G(ctx)),
				vsock.WithDialTimeout(dialerTimeouts.DialTimeout),
				vsock.WithRetryTimeout(dialerTimeouts.RetryTimeout),
				vsock.WithRetryInterval(dialerTimeouts.RetryInterval),
				vsock.WithConnectionMsgTimeout(dialerTimeouts.ConnectMsgTimeout),
				vsock.WithAckMsgTimeout(dialerTimeouts.AckMsgTimeout),
			)
		}

		var metricsProxy *metrics.Proxy
		if config.Snapshotter.Metrics.Enable {
			metricsProxy, err = initMetricsProxy(config, monitor, host, response.MetricsPort, response.Labels)
			if err != nil {
				return nil, err
			}
		}

		return proxy.NewRemoteSnapshotter(ctx, host, snapshotterDialer, metricsProxy)
	}

	return demux.NewSnapshotter(cache, newRemoteSnapshotterFunc), nil
}

func initMetricsProxyMonitor(portRange string) (*metrics.Monitor, error) {
	ports := strings.Split(portRange, "-")
	portRangeError := fmt.Errorf("invalid port range %s", portRange)
	if len(ports) < 2 {
		return nil, portRangeError
	}
	lower, err := strconv.Atoi(ports[0])
	if err != nil {
		return nil, portRangeError
	}
	upper, err := strconv.Atoi(ports[1])
	if err != nil {
		return nil, portRangeError
	}

	return metrics.NewMonitor(lower, upper)
}

func initMetricsProxy(config config.Config, monitor *metrics.Monitor, host, port string, labels map[string]string) (*metrics.Proxy, error) {
	metricsPort, err := strconv.ParseUint(port, base10, bits32)
	if err != nil {
		return nil, err
	}

	metricsDialer := func(ctx context.Context, _, _ string) (net.Conn, error) {
		return vsock.DialContext(ctx, host, uint32(metricsPort), vsock.WithLogger(log.G(ctx)))
	}

	metricsHost := config.Snapshotter.Metrics.Host

	return metrics.NewProxy(metricsHost, monitor, labels, metricsDialer)
}
