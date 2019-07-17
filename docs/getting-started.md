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
* The Firecracker binary with the optional `vsock` feature enabled.  This
  feature requires building from source; instructions for doing so are in the
  [Firecracker getting started guide](https://github.com/firecracker-microvm/firecracker/blob/master/docs/getting-started.md#building-from-source)
* A root filesystem image (you can use the one
  [described here](https://github.com/firecracker-microvm/firecracker/blob/master/docs/getting-started.md#running-firecracker)
  as `hello-rootfs.ext4`). 
* A recent installation of [Docker CE](https://docker.com).
* Go 1.11 or later, which you can download from [here](https://golang.org/dl/).
* Rust 1.32 (and Cargo), which you can download from [here](https://rustup.rs/).

## Setup

### Build Firecracker with `vsock` support

Clone the repository to your computer in a directory of your choice:

```bash
git clone https://github.com/firecracker-microvm/firecracker.git
```
Change into the new directory, and build with Cargo.  Make sure to enable the
optional `vsock` feature using the `--features vsock` flag.

> Note: Firecracker normally builds a statically-linked binary with musl libc.
> On Amazon Linux 2, you must specify `--target x86_64-unknown-linux-gnu`
> because musl libc is not available.  Switching to this target changes the set
> of syscalls invoked by Firecracker.  If you intend to jail Firecracker using
> seccomp, you must adjust your seccomp profile for these changes.

```bash
git checkout v0.17.0 # latest released tag
cargo build --release --features vsock # --target x86_64-unknown-linux-gnu
```

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
git clone https://github.com/firecracker-microvm/firecracker-containerd
make all
```

If you clone repository inside the `$GOPATH` and golang version is 1.11.x, should explicitly set `GO111MODULE=on` before build.

```bash
GO111MODULE=on make all
```

Once you have built the runtime, be sure to place the following binaries on your
`$PATH`:
* `runtime/containerd-shim-aws-firecracker`
* `snapshotter/cmd/devmapper/devmapper_snapshotter`
* `snapshotter/cmd/naive/naive_snapshotter`
* `firecracker-control/cmd/containerd/firecracker-containerd`
* `firecracker-control/cmd/containerd/firecracker-ctr`

You can use the `make install` target to install the files to `/usr/local/bin`,
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
[proxy_plugins]
  [proxy_plugins.firecracker-naive]
    type = "snapshot"
    address = "/var/run/firecracker-containerd/naive-snapshotter.sock"

[debug]
  level = "debug"
```

Also note the `firecracker-ctr` binary installed alongside the `firecracker-containerd`
binary. `ctr` is containerd's standard cli client; `firecracker-ctr` is a build of `ctr`
from the same version of containerd as `firecracker-containerd`, which ensures the two
binaries are in sync with one another. While other builds of `ctr` may work with
`firecracker-containerd`, use of `firecracker-ctr` will ensure compatibility.

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
  "metrics_fifo": "fc-metrics.fifo",

}
```
</details>

## Usage

Start the containerd snapshotter

```bash
$ ./naive_snapshotter \
  -address /var/run/firecracker-containerd/naive-snapshotter.sock \
  -path /tmp/fc-snapshot
```

In another terminal, start containerd

```bash
$ sudo PATH=$PATH /usr/local/bin/firecracker-containerd \
  --config /etc/firecracker-containerd/config.toml
```

Pull an image

```bash
$ sudo firecracker-ctr --address /run/firecracker-containerd/containerd.sock images \
  pull --snapshotter firecracker-naive \
  docker.io/library/busybox:latest
```

And start a container!

```bash
$ sudo firecracker-ctr --address /run/firecracker-containerd/containerd.sock \
  run --snapshotter firecracker-naive --runtime aws.firecracker --tty \
  docker.io/library/busybox:latest busybox-test
```

Alternatively you can specify `--runtime` and `--snapshotter` just once when creating a new namespace using containerd's default labels:

```bash
$ sudo firecracker-ctr --address /run/firecracker-containerd/containerd.sock \
  namespaces create fc

$ sudo firecracker-ctr --address /run/firecracker-containerd/containerd.sock \
  namespaces label fc \
  containerd.io/defaults/runtime=aws.firecracker \
  containerd.io/defaults/snapshotter=firecracker-naive

$ sudo firecracker-ctr --address /run/firecracker-containerd/containerd.sock \
  -n fc \
  run --tty \
  docker.io/library/busybox:latest busybox-test
```
