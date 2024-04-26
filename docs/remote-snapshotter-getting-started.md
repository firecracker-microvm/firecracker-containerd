# Getting started with remote snapshotters in firecracker-containerd

This guide will walk you through setting up firecracker-containerd to work with remote snapshotters. By the end of the guide, you should have a working firecracker-containerd setup that allows you to lazily load container images inside network-connected Firecracker microVMs.

We support the [stargz-snapshotter](https://github.com/containerd/stargz-snapshotter) as a remote snapshotter in firecracker-containerd. The stargz-snapshotter uses a specially formatted OCI container image in a form called estargz. The estargz format adds metadata to the image which allows the stargz-snapshotter to randomly access individual files in the image over the network rather than at the layer level. This enables the stargz-snapshotter to start containers before the image is fully downloaded and lazily load additional parts of the container at run time. For more information, please read the stargz-snapshotter docs including the doc on the [estargz image format](https://github.com/containerd/stargz-snapshotter/blob/v0.11.4/docs/estargz.md).

# Setup

## Prerequisites

You will need the following to use firecracker-containerd with support for remote snapshotters:

* A computer with a recent-enough version of Linux (4.14+), and KVM enabled

* A recent installation of Docker CE (for building the Firecracker root filesystem)

* git

* Go 1.21 or later

* This repository cloned onto your local machine

For more details about these requirements please see the base [getting started guide](https://github.com/firecracker-microvm/firecracker-containerd/blob/57126558036c4a34a02967865467b2161ca9227e/docs/getting-started.md)


## Build firecracker-containerd

The following commands will build firecracker-containerd (including the necessary shim and agent) as well as Firecracker. It will install these binaries to `/usr/local/bin`. You do not have to install any of the components in this guide, however all of the example configuration assumes the components have been installed and will need modification if you choose to skip this step.

```
make
make install
make firecracker
make install-firecracker
```

## Build a Linux kernel with FUSE support

We generally recommend that users download our AWS-provided Linux kernel that is known to work with Firecracker. Unfortunately this kernel does not support FUSE which is needed for remote snapshotters. We do provide make rules to build a working kernel with FUSE support:

```
make kernel
make install-kernel
```

By default, this will build a Linux 4.14 kernel. If you would like to use 5.10, use:
```
KERNEL_VERSION=5.10 make kernel
KERNEL_VERSION=5.10 make install-kernel
```

[Note: Firecracker only officially supports 4.14 and 5.10](https://github.com/firecracker-microvm/firecracker/blob/v1.1.0/docs/kernel-policy.md). 


## Build a Firecracker rootfs with a remote snapshotter

The firecracker-containerd repository includes an image builder component that constructs a Debian-based root filesystem for the VM. The default installs the necessary firecracker-containerd agent as well as sets up an overlay to allow re-using the read only image across multiple VMs. We also supply a variant that provides these same features and additionally bundles the stargz-snapshotter.

```
make image-stargz
make install-stargz-rootfs
```

## Setup the demo network

While networking support is optional for firecracker-containerd in general, it is required for remote snapshotter support because the remote snapshotter will lazily load container images from inside the VM. The easiest way to set this up is to use the provided `demo-network` which will install CNI configuration to create a bridge, a veth pair connected to the bridge, and an adapter to connect the tap to Firecracker. 

```
make demo-network
```

## Configure Firecracker

If you followed the getting started or quickstart guide to setup firecracker-containerd, then Firecracker will be configured to use the default rootfs and kernel without FUSE support. In order for firecracker-containerd to work with remote snapshotters you should update `/etc/containerd/firecracker-runtime.json` to set:

* `kernel_image_path` to point to your kernel built with FUSE support
* `root_drive` to points to your rootfs with a remote snapshotter
* `default_network_interfaces` to set up your CNI network. This network interface must be allowed to access MMDS in order to configure credentials for the remote snapshotter

An example of this type of configuration is as follows
```
{
  "firecracker_binary_path": "/usr/local/bin/firecracker",
  "kernel_image_path": "/var/lib/firecracker-containerd/runtime/default-vmlinux.bin",
  "kernel_args": "console=ttyS0 pnp.debug=1 noapic reboot=k panic=1 pci=off nomodules ro systemd.unified_cgroup_hierarchy=0 systemd.journald.forward_to_console systemd.unit=firecracker.target init=sbin/overlay-init",
  "root_drive": "/var/lib/firecracker-containerd/runtime/rootfs-stargz.img",
  "log_fifo": "fc-logs.fifo",
  "log_levels": ["debug"],
  "metrics_fifo": "fc-metrics.fifo",
  "default_network_interfaces": [
  {
    "AllowMMDS": true,
    "CNIConfig": {
      "NetworkName": "fcnet",
      "InterfaceName": "veth0"
    }
  }
]
}
```

## Configure containerd

Firecracker-containerd relies on a `demux-snapshotter` running on the host to proxy snapshotter requests to the appropriate remote snapshotter running in the appropriate VM. `/etc/firecracker-containerd/config.toml` needs to be updated to tell `firecracker-containerd` about this `demux-snapshotter`. 

The following configuration will create a snapshotter called `proxy` which will be redirected over gRPC to the default endpoint for the `demux-snapshotter`

```
[proxy_plugins]
  [proxy_plugins.proxy]
    type = "snapshot"
    address = "/var/lib/demux-snapshotter/snapshotter.sock"
```

## Configure demux-snapshotter

To configure the demux-snapshotter, add the following config to `/etc/demux-snapshotter/config.toml`

```
[snapshotter.listener]
  type = "unix"
  address = "/var/lib/demux-snapshotter/snapshotter.sock"

[snapshotter.proxy.address.resolver]
  type = "http"
  address = "http://127.0.0.1:10001"

[snapshotter.metrics]
  enable = false

[debug]
  logLevel = "debug"
```

Create the demux snapshotter's runtime directory.
```
mkdir -p /var/lib/demux-snapshotter
```

## Start all of the host-daemons

Remote snapshotters require 3 host-side daemons: 
* `firecracker-containerd` - For running VMs/containers
* `demux-snapshotter` - For demultiplexing snapshotter requests from firecracker-containerd to the appropriate remote-snapshotter in the appropriate VM
* `http-address-resolver` - For mapping containerd namespace to VM vsock address (see [limitations](#limitations))

Run each in a separate shell:

```
firecracker-containerd --config /etc/firecracker-containerd/config.toml
```
```
snapshotter/demux-snapshotter
```
```
snapshotter/http-address-resolver
```

## Pull a remote image

Pulling a remote image requires special configuration which is currently not implemented by any CLI tool. We do have an example client implementation which can be built or extended for testing. Please see the example here: https://github.com/firecracker-microvm/firecracker-containerd/tree/main/examples/cmd/remote-snapshotter

## Limitations

### Containerd namespace must be 1-1 with a VM
  Portions of containerd's snapshotter API receive only containerd namespace and snapshotter key. In order to route those requests to the correct remote snapshotter running inside a VM, we route based on namespace. This means that we must have a unique mapping between a containerd namespace and a VM in order for requests to be fulfilled.

### OCI Images must be converted to estargz
  estargz images are compatible with OCI images, but OCI images must be converted to estargz to be usable with the lazy loading properties of the stargz-snapshotter. For information on how to convert and image, please see the [stargz-snapshotter's documentation](https://github.com/containerd/stargz-snapshotter/blob/v0.11.4/docs/ctr-remote.md)
  
### There is no fallback to ahead-of-time pull
  The stargz-snapshotter can normally fall back to an embedded overlay snapshotter with ahead-of-time pull if the image is not estargz. This does not work with firecracker-containerd because in this scenario, the client is responsible for unpacking the container image into the mount points provided by the snapshotter, however the client runs on the host and the mount points are isolated inside the VM.
