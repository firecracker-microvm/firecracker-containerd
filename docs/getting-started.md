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
* An uncompressed ELF-format kernel image (you can use the one
  [described here](https://github.com/firecracker-microvm/firecracker/blob/master/docs/getting-started.md#running-firecracker)
  as `hello-vmlinux.bin`).
* A root filesystem image (you can use the one
  [described here](https://github.com/firecracker-microvm/firecracker/blob/master/docs/getting-started.md#running-firecracker)
  as `hello-rootfs.ext4`). 
* A recent installation of
  [containerd](https://github.com/containerd/containerd/releases).  We suggest
  [containerd v1.2.1](https://github.com/containerd/containerd/releases/tag/v1.2.1).
* Go 1.11 or later, which you can download from [here](https://golang.org/dl/).
* Rust (and Cargo), which you can download from [here](https://rustup.rs/).

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
git checkout v0.12.0 # latest released tag
cargo build --release --features vsock # --target x86_64-unknown-linux-gnu
```

### Download appropriate filesystem images

You can use the following images for the kernel and a default user-space:

```bash
curl -fsSL -o hello-vmlinux.bin https://s3.amazonaws.com/spec.ccfc.min/img/hello/kernel/hello-vmlinux.bin
curl -fsSL -o hello-rootfs.ext4 https://s3.amazonaws.com/spec.ccfc.min/img/hello/fsfiles/hello-rootfs.ext4
```

We will be modifying the `hello-rootfs.ext4` file in a subsequent step.

### Install containerd

You can install containerd from the
[upstream binaries](https://github.com/containerd/containerd/releases/tag/v1.2.1),
[build it yourself](https://github.com/containerd/containerd/blob/master/BUILDING.md),
or use the containerd binary included with recent releases of Docker (like
18.09).  We do not recommend using older versions of containerd.

The [quickstart guide](quickstart.md) has example commands for building and
installing containerd.

### Install runc

You can install runc from the
[upstream binaries](https://github.com/opencontainers/runc/releases/tag/v1.0.0-rc6),
[build it yourself](https://github.com/containerd/containerd/blob/v1.2.1/RUNC.md),
or use the runc binary included with recent releases of Docker (like 18.06).  We
do not recommend using older versions of containerd.

Depending on whether or not the root filesystem of the microVM contains an
installation of glibc, you may need to have a statically-linked build of runc.

The [quickstart guide](quickstart.md) has example commands for building and
installing runc, with static linking.


### Clone this repository and build the components

Clone this repository to your computer in a directory of your choice.
We recommend choosing a directory outside of `$GOPATH` or `~/go`.

```bash
git clone https://github.com/firecracker-microvm/firecracker-containerd
make STATIC_AGENT=true
```

If you clone repository inside the `$GOPATH` and golang version is 1.11.x, should explicitly set `GO111MODULE=on` before build.

```bash
GO111MODULE=on make STATIC_AGENT=true
```

Once you have built the runtime, be sure to place the
`containerd-shim-aws-firecracker` binary on your `$PATH`.

### Inject the agent, runc shim, and runc into the rootfs image

firecracker-containerd needs two components to be embedded in the root
filesystem image.  They are:

* `runc` - used to run containers
* firecracker-containerd's `agent` - this is a process that communicates with
  the outer firecracker-containerd `runtime` process over a `vsock` and proxies
  commands to `containerd-shim-runc-v1`

In order to inject these components in the root filesystem image above, we'll
need to do a few steps.  These instructions assume that you're using the image
described above; they are specific to the Alpine Linux user user-space and its
init system.

1. Increase the size of the root filesystem image.  The image that is supplied
   by Firecracker (`hello-rootfs.ext4`) is an ext4 filesystem that is sized
   exactly to the files already in it; no free space.  We can resize this
   filesystem (while unmounted) by running
   `e2fsck -f hello-rootfs.ext4 && resize2fs hello-rootfs.ext4`.
2. Construct a `fc-agent.start` file so that the agent starts on boot:
   <details>
   <summary>fc-agent.start</summary>
   
   ```bash
   #!/bin/sh
   mkdir -p /container/rootfs
   exec > /container/agent-debug.log # Debug logs from the agent
   exec 2>&1
   touch /container/runtime
   mount -t auto -o rw /dev/vdb /container/rootfs
   cd /container
   /usr/local/bin/agent -id 1 -debug &
   ```
   </details>
3. Mount the filesystem to a location like `/tmp/mnt`
   <details>
   <summary>Mounting at /tmp/mnt</summary>
   
   ```bash
   sudo mkdir /tmp/mnt
   sudo mount hello-rootfs.ext4 /tmp/mnt
   ```
   </details>
4. Copy in the binaries, copy the `fc-agent.start` file, and set up OpenRC to
   launch the necessary components
   <details>
   <summary>Copy in the binaries to /tmp/mnt</summary>
   
   ```bash
   sudo cp $(which runc) firecracker-containerd/agent/agent /tmp/mnt/usr/local/bin
   sudo cp fc-agent.start /tmp/mnt/etc/local.d
   sudo chmod +x /tmp/mnt/etc/local.d/fc-agent.start
   sudo ln -s /etc/init.d/local /tmp/mnt/etc/runlevels/default/local
   sudo ln -s /etc/init.d/cgroups /tmp/mnt/etc/runlevels/default/cgroups
   sudo umount /tmp/mnt
   ```
   </details>

### Configure containerd snapshotter

Add the snapshotter plugin to your `/etc/containerd/config.toml`
```toml
[proxy_plugins]
  [proxy_plugins.firecracker-naive]
    type = "snapshot"
    address = "/var/run/firecracker-containerd/naive-snapshotter.sock"
```

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
* `cpu_count` (required) - The number of vCPUs to make available to a microVM.
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
  "kernel_args": "console=ttyS0 noapic reboot=k panic=1 pci=off nomodules rw",
  "root_drive": "/var/lib/firecracker-containerd/runtime/hello-rootfs.ext4",
  "cpu_count": 1,
  "cpu_template": "T2",
  "log_fifo": "/tmp/fc-logs.fifo",
  "log_level": "Debug",
  "metrics_fifo": "/tmp/fc-metrics.fifo"
}
```
</details>

## Usage

Start the containerd snapshotter

```bash
$ ./naive_snapshotter -address /var/run/firecracker-containerd/naive-snapshotter.sock -path /tmp/fc-snapshot
```

In another terminal, start containerd

```bash
sudo PATH=$PATH /usr/local/bin/containerd
```

Pull an image

```bash
$ sudo ctr images pull --snapshotter firecracker-naive docker.io/library/busybox:latest
```

And start a container!

```bash
$ sudo ctr run --snapshotter firecracker-naive --runtime aws.firecracker --tty docker.io/library/busybox:latest busybox-test
```
