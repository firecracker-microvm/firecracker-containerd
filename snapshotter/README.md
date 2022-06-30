# (De)multiplexing Snapshotter

This package enables the use of in-VM snapshotters.

There are a couple of components within this package that enable containerd
snapshot requests to be proxied to an in-VM snapshotter:

 * The snapshotter plugin that runs as a service to receive snapshotter
   requests from containerd via GRPC. See [containerd's plugin guide](https://github.com/containerd/containerd/blob/v1.6.6/docs/PLUGINS.md)
   for more information on building external snapshotter plugins. This plugin enables
   containerd snapshotter requests to be proxied over a GRPC connection on top of [firecracker's
   vsock](https://github.com/firecracker-microvm/firecracker/blob/v1.1.0/docs/vsock.md).
 * The address resolver agent which implements the namespace lookup API for querying firecracker's
   vsock pathing for communication to the microVM.

## Snapshotter Plugin

The snapshotter plugin is a snapshots service for which we can configure containerd to proxy snapshot requests.

```
[proxy_plugins]
  [proxy_plugins.proxy]
    type = "snapshot"
    address = "/var/lib/demux-snapshotter/snapshotter.sock"
```

The containerd configuration above will match the demux snapshotter listener configuration.

```toml
[snapshotter.listener]
  type = "unix"
  address = "/var/lib/demux-snapshotter/snapshotter.sock"
```

## Address Resolver Agent

The address resolver agent is a service for querying the network address for a remote snapshotter keyed off the namespace
of the container being built.

The example agent provided here does a simple lookup of the Firecracker microVM using
a firecracker-control client where the `VMID` matches the namespace of the container.

### HTTP Address Resolver Agent

An address resolver agent must implement the following HTTP endpoint to be able to handle requests from the demux snapshotter.

Example Request:
```
GET /address?namespace="cbfad871-0862-4dd6-ae7a-52e9b1c16ede"
```

Example Response:
```json
{
  "network": "unix",
  "address": "/var/lib/firecracker-containerd/shim-base/default#cbfad871-0862-4dd6-ae7a-52e9b1c16ede/firecracker.vsock",
  "snapshotter_port": "10000",
  "metrics_port": "10001",
  "labels": {
    "namespace": "cbfad871-0862-4dd6-ae7a-52e9b1c16ede"
  }
}
```

## Building

`demux-snapshotter` and `http-address-resolver` can be built with

```
make
```

## Testing

To run unit-tests, run:

```
make test
```

To run integration tests, run:

```
make integ-test
```

## Configuration

Configuration filepath can be specified at runtime using command line argument `--config`.

By default, if no configuration filepath is specified then `/etc/demux-snapshotter/config.toml` will be used.

### Listener

Application configuration to denote the interface the service will receive requests.

```toml
[snapshotter.listener]
  type = "unix"
  address = "/var/lib/demux-snapshotter/snapshotter.sock"
```

### Proxy Address Resolver

Application configuration to denote the interface to use for resolving proxy addressing
for remote snapshotters.

```toml
[snapshotter.proxy.address.resolver]
  type = "http"
  address = "http://127.0.0.1:10001"
```

### Metrics

Application configuration to enable remote snapshotter metrics collection via a demux snapshotter endpoint.

```toml
[snapshotter.metrics]
  enable = true
  port_range = "9000-9999"

  [snapshotter.metrics.service_discovery]
    enable = true
    port = 8080
```

Service discovery enables Prometheus service discovery at `http://localhost:{port}` which returns proxy
endpoints for remote snapshotter Prometheus endpoints.
