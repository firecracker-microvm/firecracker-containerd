# containerd-firecracker-snapshotter

This component is a
[snapshotter](https://github.com/containerd/containerd/blob/master/design/snapshots.md)
[plugin](https://github.com/containerd/containerd/blob/master/PLUGINS.md) for
containerd that stores snapshots in flat, ext4-formatted filesystem images.
The snapshots created by this snapshotter are usable with the
containerd-firecracker-runtime to run microVM-backed containers with the
Firecracker VMM.

This snapshotter plugin is written for broad compatibility, and should run on
any Linux system capable of running Firecracker and containerd. However, it
sacrifices efficiency in order to achieve this compatibility. Each layer in a
container image is represented as a unique filesystem image. Each container is
given a complete private copy of it's root filesystem image upon creation. Thus,
container creation is expensive in terms IO and disk space.

We should consider writing a more efficient snapshotter plugin. Linux's
device-mapper subsystem would allow us to build something based on copy-on-write
snapshots that is performant and space-efficient, with the tradeoff being that
it would require some additional setup on the host. 

## Installation

To make containerd aware of this plugin, you need to register it in
containerd's configuration file.  This file is typically located at
`/etc/containerd/config.toml`.

Here's a sample entry that can be made in the configuration file:

```toml
[proxy_plugins]
  [proxy_plugins.firecracker-snapshotter]
    type = "snapshot"
    address = "/var/run/firecracker-snapshotter.sock"
```

The name of the plugin in this example is "firecracker-snapshotter".  The
`address` entry points to a socket file exposed by the snapshotter, which is
determined when you run it.

## Usage

```
./naive_snapshotter -address UNIX-DOMAIN-SOCKET -path ROOT -debug
```

To run the snapshotter, you must specify both a Unix domain socket and a root
directory where the snapshots will be stored.  For example, to run the
snapshotter with its domain socket at `/var/run/firecracker-snapshotter.sock`
and its storage at `/var/lib/firecracker-snapshotter`, you would run the
snapshotter plugin process as follows:

```
./naive_snapshotter -address /var/run/firecracker-snapshotter.sock -path /var/lib/firecracker-snapshotter
```

Now you can use snapshotter with containerd:

```
CONTAINERD_SNAPSHOTTER=firecracker-snapshotter ctr images pull docker.io/library/alpine:latest
```
