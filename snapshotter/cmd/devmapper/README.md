# devmapper snapshotter for containerd and Firecracker

This component is a
[snapshotter](https://github.com/containerd/containerd/blob/master/design/snapshots.md)
[plugin](https://github.com/containerd/containerd/blob/master/PLUGINS.md) for
containerd that stores snapshots in ext4-formatted filesystem images in a
devicemapper thin pool.  The snapshots created by this snapshotter are usable
with the containerd-firecracker-runtime to run microVM-backed containers with
the Firecracker VMM.

## Installation

To make containerd aware of this plugin, you need to register it in
containerd's configuration file.  This file is typically located at
`/etc/containerd/config.toml`.

Here's a sample entry that can be made in the configuration file:

```toml
[proxy_plugins]
  [proxy_plugins.firecracker-dm-snapshotter]
    type = "snapshot"
    address = "/var/run/firecracker-dm-snapshotter.sock"
```

The name of the plugin in this example is "firecracker-dm-snapshotter".  The
`address` entry points to a socket file exposed by the snapshotter, which is
determined when you run it.

## Usage

```
./devmapper_snapshotter -address UNIX-DOMAIN-SOCKET -config CONFIG -debug
```

To run the snapshotter, you must specify the path to a Unix domain socket and a
path to a JSON configuration file.  The config file must contain the following
fields:

* `RootPath` - a directory where the metadata will be available
* `PoolName` - a name to use for the devicemapper thin pool
* `DataDevice` - path to the data volume that should be used by the thin pool
* `MetadataDevice` - path to the metadata volume that should be used by the thin
  pool
* `DataBlockSize` - the size of allocation chunks in data file, between 128
  sectors (64KB) and and 2097152 sectors (1GB) and a multiple of 128 sectors
  (64KB)
* `BaseImageSize` - defines how much space to allocate when creating the base
  device

For example, to run the snapshotter with its domain socket at
`/var/run/firecracker-dm-snapshotter.sock` and its configuration file at
`/etc/firecracker-dm-snapshotter/config.json` you would run the snapshotter
plugin process as follows:

```
./devmapper_snapshotter -address /var/run/firecracker-dm-snapshotter.sock -config /etc/firecracker-dm-snapshotter/config.json
```

Now you can use snapshotter with containerd:

```
CONTAINERD_SNAPSHOTTER=firecracker-dm-snapshotter ctr images pull docker.io/library/alpine:latest
```
