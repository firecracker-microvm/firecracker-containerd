# Getting started with Firecracker and containerd

Thanks for your interest in using containerd and Firecracker together!  This
project is currently in a very early state, and things are not yet as
plug-and-play as we'd like.  This guide contains general instructions for
getting started, but is more descriptive than a concrete set of step-by-step
instructions. 

If you prefer to follow a script, you can use the
[quickstart guide](quickstart.md) instead.

## Prerequisites

You need to have the following things in order to use firecracker-containerd:

* A computer with a recent-enough version of Linux (4.14+), an Intel x86_64
  processor (AMD and Arm are on the roadmap, but not yet supported), and KVM
  enabled.  An i3.metal running Amazon Linux 2 is a good candidate.
  
  <details>
  
  <summary>Click here to see a bash script that will check if your system meets
  the basic requirements to run Firecracker.</summary>
  
  ```bash
  #!/bin/bash
  err=""; \
  [ "$(uname) $(uname -m)" = "Linux x86_64" ] \
    || err="ERROR: your system is not Linux x86_64."; \
  [ -r /dev/kvm ] && [ -w /dev/kvm ] \
    || err="$err\nERROR: /dev/kvm is innaccessible."; \
  (( $(uname -r | cut -d. -f1)*1000 + $(uname -r | cut -d. -f2) >= 4014 )) \
    || err="$err\nERROR: your kernel version ($(uname -r)) is too old."; \
  dmesg | grep -i "hypervisor detected" \
    && echo "WARNING: you are running in a virtual machine. Firecracker is not well tested under nested virtualization."; \
  [ -z "$err" ] && echo "Your system looks ready for Firecracker!" || echo -e "$err"
  ```
  
  </details>
* git
* A root filesystem image (you can use the one
  [described here](https://github.com/firecracker-microvm/firecracker/blob/master/docs/getting-started.md#running-firecracker)
  as `hello-rootfs.ext4`). 
* A recent installation of [Docker CE](https://docker.com).
* Go 1.11 or later, which you can download from [here](https://golang.org/dl/).

## Setup

### Download appropriate kernel

You can use the following kernel:

```bash
curl -fsSL -o hello-vmlinux.bin https://s3.amazonaws.com/spec.ccfc.min/img/hello/kernel/hello-vmlinux.bin
```

### Install Docker

You can install Docker CE from the [upstream
binaries](https://docs.docker.com/install/) or from your Linux distribution.

### Clone this repository and build the components

Clone this repository to your computer in a directory of your choice.
We recommend choosing a directory outside of `$GOPATH` or `~/go`.

```bash
git clone --recurse-submodules https://github.com/firecracker-microvm/firecracker-containerd
make all
```

If you clone repository inside the `$GOPATH` and golang version is 1.11.x, should explicitly set `GO111MODULE=on` before build.

```bash
GO111MODULE=on make all
```

Once you have built the runtime, be sure to place the following binaries on your
`$PATH`:
* `runtime/containerd-shim-aws-firecracker`
* `firecracker-control/cmd/containerd/firecracker-containerd`
* `firecracker-control/cmd/containerd/firecracker-ctr`

You can use the `make install` target to install the files to `/usr/local/bin`,
or specify a different `INSTALLROOT` if you prefer another location.

### Build Firecracker

From the repository cloned in the previous step, run
```bash
make firecracker
```

Once you have built firecracker, be sure to place the following binaries on your
`$PATH`:
* `_submodules/firecracker/target/x86_64-unknown-linux-musl/release/firecracker`
* `_submodules/firecracker/target/x86_64-unknown-linux-musl/release/jailer`

You can use the `make install-firecracker` target to install the files to `/usr/local/bin`,
or specify a different `INSTALLROOT` if you prefer another location.

### Build a root filesystem

The firecracker-containerd repository includes an image builder component that
constructs a Debian-based root filesystem for the VM.  This root filesystem
bundles the necessary firecracker-containerd components and is configured to be
safely shared among multiple VMs by overlaying a read-write filesystem layer on
top of a read-only image.

The image builder uses Docker, and expects you to be a member of the `docker`
group (or otherwise have access to the Docker API socket).

You can build an image like this:

```bash
make image
sudo mkdir -p /var/lib/firecracker-containerd/runtime
sudo cp tools/image-builder/rootfs.img /var/lib/firecracker-containerd/runtime/default-rootfs.img
```

### Configure `firecracker-containerd` binary

The `firecracker-containerd` binary is a `containerd` binary that includes an
additional plugin.  Configure it with a separate config file and have it use a
separate location for on-disk state.  Make sure to include configuration for the
snapshotter you intend to use.

We recommend a configuration like the following:

```toml
disabled_plugins = ["cri"]
root = "/var/lib/firecracker-containerd/containerd"
state = "/run/firecracker-containerd"
[grpc]
  address = "/run/firecracker-containerd/containerd.sock"
[plugins]
  [plugins.devmapper]
    pool_name = "fc-dev-thinpool"
    base_image_size = "10GB"
    root_path = "/var/lib/firecracker-containerd/snapshotter/devmapper"

[debug]
  level = "debug"
```

Also note the `firecracker-ctr` binary installed alongside the `firecracker-containerd`
binary. `ctr` is containerd's standard cli client; `firecracker-ctr` is a build of `ctr`
from the same version of containerd as `firecracker-containerd`, which ensures the two
binaries are in sync with one another. While other builds of `ctr` may work with
`firecracker-containerd`, use of `firecracker-ctr` will ensure compatibility.

### Prepare and configure snapshotter

The devmapper snapshotter requires a thinpool to exist.
Below is a script to create a thinpool device.

`Note: The configuration with loopback devices is slow and not intended for use in production.`

<details>
<summary>Script to setup thinpool with dmsetup.</summary>

```bash
#!/bin/bash

# Sets up a devicemapper thin pool with loop devices in
# /var/lib/firecracker-containerd/snapshotter/devmapper

set -ex

DIR=/var/lib/firecracker-containerd/snapshotter/devmapper
POOL=fc-dev-thinpool

if [[ ! -f "${DIR}/data" ]]; then
touch "${DIR}/data"
truncate -s 100G "${DIR}/data"
fi

if [[ ! -f "${DIR}/metadata" ]]; then
touch "${DIR}/metadata"
truncate -s 2G "${DIR}/metadata"
fi

DATADEV="$(losetup --output NAME --noheadings --associated ${DIR}/data)"
if [[ -z "${DATADEV}" ]]; then
DATADEV="$(losetup --find --show ${DIR}/data)"
fi

METADEV="$(losetup --output NAME --noheadings --associated ${DIR}/metadata)"
if [[ -z "${METADEV}" ]]; then
METADEV="$(losetup --find --show ${DIR}/metadata)"
fi

SECTORSIZE=512
DATASIZE="$(blockdev --getsize64 -q ${DATADEV})"
LENGTH_SECTORS=$(bc <<< "${DATASIZE}/${SECTORSIZE}")
DATA_BLOCK_SIZE=128 # see https://www.kernel.org/doc/Documentation/device-mapper/thin-provisioning.txt
LOW_WATER_MARK=32768 # picked arbitrarily
THINP_TABLE="0 ${LENGTH_SECTORS} thin-pool ${METADEV} ${DATADEV} ${DATA_BLOCK_SIZE} ${LOW_WATER_MARK} 1 skip_block_zeroing"
echo "${THINP_TABLE}"

if ! $(dmsetup reload "${POOL}" --table "${THINP_TABLE}"); then
dmsetup create "${POOL}" --table "${THINP_TABLE}"
fi
```
</details>

### Configure containerd runtime plugin

The runtime expects a JSON-formatted configuration file to be located either in
`/etc/containerd/firecracker-runtime.json` or in a location defined by the
`FIRECRACKER_CONTAINERD_RUNTIME_CONFIG_PATH` environment variable.  The
configuration file has the following fields:

* `firecracker_binary_path` (optional) - A path to locate the `firecracker`
  executable.  If left undefined, the runtime looks for an executable named
  `firecracker` located in its working directory.  A fully-qualified path to the
  `firecracker` binary is recommended, as the working directory typically
  changes every execution when run by containerd.
* `kernel_image_path` (optional) - A path where the kernel image file is
  located.  A fully-qualified path is recommended.  If left undefined, the
  runtime looks for a file named
  `/var/lib/firecracker-containerd/runtime/default-vmlinux.bin`.
* `kernel_args` (optional) - Arguments for the kernel command line.  If left
  undefined, the runtime specifies "console=ttyS0 noapic reboot=k panic=1
  pci=off nomodules rw".
* `root_drive` (optional) - A path where the root drive image file is located. A
  fully-qualified path is recommended.  If left undefined, the runtime looks for
  a file named `/var/lib/firecracker-containerd/runtime/default-rootfs.img`.
* `cpu_count` (optional) - The number of vCPUs to make available to a microVM.
  If left undefined, the default is 1.
* `cpu_template` (required) - The Firecracker CPU emulation template.  Supported
  values are "C3" and "T2".
* `additional_drives` (unused)
* `log_fifo` (optional) - Named pipe where Firecracker logs should be delivered.
* `log_level` (optional) - Log level for the Firecracker logs
* `metrics_fifo` (optional) - Named pipe where Firecracker metrics should be
  delivered.
* `ht_enabled` (unused) - Reserved for future use.
* `debug` (optional) - Enable debug-level logging from the runtime.
* `default_network_interfaces` (optional) - a list of network interfaces to configure
  a VM with if no list of network interfaces is provided with a CreateVM call. Defaults
  to an empty list. The structure of the items in the list is the same as the Go API
  FirecrackerNetworkInterface defined [in protobuf here](../proto/types.proto).
* `shim_base_dir` - (optional) Set the path to which Firecracker will run the
  shim from. Defaults to /var/lib/firecracker-containerd/shim-base

<details>
<summary>A reasonable example configuration</summary>

```json
{
  "firecracker_binary_path": "/usr/local/bin/firecracker",
  "kernel_image_path": "/var/lib/firecracker-containerd/runtime/hello-vmlinux.bin",
  "kernel_args": "console=ttyS0 noapic reboot=k panic=1 pci=off nomodules ro systemd.journald.forward_to_console systemd.unit=firecracker.target init=/sbin/overlay-init",
  "root_drive": "/var/lib/firecracker-containerd/runtime/default-rootfs.img",
  "cpu_template": "T2",
  "log_fifo": "fc-logs.fifo",
  "log_level": "Debug",
  "metrics_fifo": "fc-metrics.fifo"
}
```
</details>

## Usage

Ensure that /var/lib/firecracker-containerd exists as the default shim base
directory used is /var/lib/firecracker-containerd/shim-base
```bash
mkdir -p /var/lib/firecracker-containerd
```

Start containerd

```bash
$ sudo PATH=$PATH /usr/local/bin/firecracker-containerd \
  --config /etc/firecracker-containerd/config.toml
```

Pull an image

```bash
$ sudo firecracker-ctr --address /run/firecracker-containerd/containerd.sock images \
  pull --snapshotter devmapper \
  docker.io/library/busybox:latest
```

And start a container!

```bash
$ sudo firecracker-ctr --address /run/firecracker-containerd/containerd.sock \
  run \
  --snapshotter devmapper \
  --runtime aws.firecracker \
  --rm --tty --net-host \
  docker.io/library/busybox:latest busybox-test
```

Alternatively you can specify `--runtime` and `--snapshotter` just once when 
creating a new namespace using containerd's default labels:

```bash
$ sudo firecracker-ctr --address /run/firecracker-containerd/containerd.sock \
  namespaces create fc

$ sudo firecracker-ctr --address /run/firecracker-containerd/containerd.sock \
  namespaces label fc \
  containerd.io/defaults/runtime=aws.firecracker \
  containerd.io/defaults/snapshotter=devmapper

$ sudo firecracker-ctr --address /run/firecracker-containerd/containerd.sock \
  -n fc \
  run --rm --tty --net-host \
  docker.io/library/busybox:latest busybox-test
```

## Networking support 
Firecracker-containerd supports the same networking options as provided by the 
Firecracker Go SDK, [documented here](https://github.com/firecracker-microvm/firecracker-go-sdk#network-configuration).
This includes support for configuring VM network interfaces both with
pre-created tap devices and with tap devices created automatically by 
[CNI](https://github.com/containernetworking/cni) plugins.

### CNI Setup

CNI-configured networks offer the quickest way to get VMs up and running with
connectivity between MicroVMs and to external networks. Setting one up requires
a few extra steps in addition to the above Setup steps.

Production deployments should be sure to choose a network configuration suitale
to the specifics of the environment and workloads being hosting, with particular
attention being given to network isolation between tasks.

To install the required CNI dependencies, run the following make target from the 
previously cloned firecracker-containerd repository:

```bash
$ sudo make demo-network
```

You can check the Makefile to see exactly what is installed and where, but for a
quick summary:
* [`bridge` CNI plugin](https://github.com/containernetworking/plugins/tree/master/plugins/main/bridge)
  - Creates a [veth](http://man7.org/linux/man-pages/man4/veth.4.html) pair with
  one end in a private network namespace and the other end attached to a bridge
  device in the host's network namespace.
* [`ptp` CNI plugin](https://github.com/containernetworking/plugins/tree/master/plugins/main/ptp)
  - Creates a [veth](http://man7.org/linux/man-pages/man4/veth.4.html) pair with
  one end in a private network namespace and the other end in the host's network
  namespace.
* [`host-local` CNI
  plugin](https://github.com/containernetworking/plugins/tree/master/plugins/ipam/host-local)
  - Manages IP allocations of network devices present on the local machine by
  vending them from a statically defined subnet.
* [`firewall` CNI
  plugin](https://github.com/containernetworking/plugins/tree/master/plugins/meta/firewall)
  - Sets up firewall rules on the host that allows traffic to/from VMs via the host
    network.
* [`tc-redirect-tap` CNI
  plugin](https://github.com/firecracker-microvm/firecracker-go-sdk/tree/master/cni)
  - A CNI plugin that adapts other CNI plugins to be usable by Firecracker VMs.
  [See this doc for more details](networking.md). It is used here to adapt veth
  devices created by the `ptp` plugin to tap devices provided to VMs.
* [`fcnet.conflist`](../tools/demo/fcnet.conflist) - A sample CNI configuration
  file that defines a `fcnet` network created via the `ptp`, `host-local` and
  `tc-redirect-tap` plugins
  - Note that, by default, the nameserver configuration within your host's
    `/etc/resolv.conf` will be parsed and provided to VMs as their nameserver
	configuration. This can cause problems if your host is using a systemd
	resolver or other resolver that operates on localhost (which results in the
	VM using its own localhost as the nameserver instead of your host's). This
	situation may require manual tweaking of the default CNI configuration, such
	as specifying [static DNS configuration as part of the `ptp` plugin](
	https://github.com/containernetworking/plugins/tree/master/plugins/main/ptp#network-configuration-reference).

After those dependencies are installed, an update to the firecracker-containerd
configuration file is required for VMs to use the `fcnet` CNI-configuration as
their default way of generating network interfaces. Just include the following
`default_network_interfaces` key in your runtime configuration file (by default
at `/etc/containerd/firecracker-runtime.json`):
```json
"default_network_interfaces": [
  {
    "CNIConfig": {
      "NetworkName": "fcnet",
      "InterfaceName": "veth0"
    }
  }
]
```

After that, start up a container (as described in the above Usage section) and
try pinging any IP available on your host. If your host has internet access,
you should also be able to access the internet from the container too.
