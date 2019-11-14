# Create Firecracker VM images for use with firecracker-containerd #

## Debian root image ##

### Overview ###

The image builder component of firecracker-containerd will build a
microVM image including the necessary components to support container
management inside the microVM. In particular, the
firecracker-containerd runtime agent and runc binary will be installed
in the image.

The image is generated as a read-only squashfs image. A read/write
overlay layer is supported via the /sbin/overlay-init program, which
should be used as init (e.g. by passing `init=/sbin/overlay-init` as a
kernel boot parameter). By default, overlay-init allocates a tmpfs
filesystem for use as the upper layer, but a block device can be
provided via the `overlay_root` kernel parameter,
e.g. `overlay_root=vdc`. This device should already contain a
(possibly empty) ext4 filesystem. By using a block device, it is
possible to preserve the filesystem state beyond the termination of
the VM, and potentially re-use it for subsequent VM execution.

### Generation ###

There are two alternatives for providing the build environment. You
can perform the image build in Docker, in which case the only
build-time dependency is that you can launch Docker container directly
(i.e. without `sudo`, etc). To build an image in this configuration,
run from [the root of the firecracker-containerd package](../../Makefile):

`$ make image`

Alternatively, to build outside a container, you'll need:

* To pre-build the `agent` binary, set appropriate permissions and place it
  under the rootfs builder directory.
  * For example, run `make agent` from the root of the firecracker-containerd
    package and copy `agent/agent` to 
	`tools/image-builder/files_ephemeral/usr/local/bin/agent`.
* To pre-build the `runc` binary, set appropriate permissions and place it
  under the rootfs builder directory.
  * For example, run `make -C _submodules/runc static` from the root of the 
    firecracker-containerd package and copy `_submodules/runc/runc` to 
	`tools/image-builder/files_ephemeral/usr/local/bin/runc`
* [`debootstrap`](https://salsa.debian.org/installer-team/debootstrap)
  (Install via the package of the same name on Debian and Ubuntu)
* `mksquashfs`, available in the
   [`squashfs-tools`](https://packages.debian.org/stretch/squashfs-tools)
   package on Debian and Ubuntu.
* To run the image build process as root.

Then execute `make rootfs.img` from this directory (`tools/image-builder`)

### Usage ###

The generated root filesystem contains all the components necessary
for use with firecracker-containerd, including the runc and agent
binaries.

You can tell the firecracker-containerd runtime component where to
find the root filesystem image by setting the `root_drive` value in
`/etc/containerd/firecracker-runtime.json` to the complete path to the
generated image file.

In order to start the agent at VM startup, systemd should be
instructed to boot to the `firecracker.target` via the kernel
command line.

In order to use the root filesystem as a reusable "lower layer" for an
overlay-based based filesystem, `init=/sbin/overlay-init` should be
the final parameter passed on the kernel command line.

A complete command line, settable via the `kernel_args` setting in `/etc/containerd/firecracker-runtime.json`, is:

    ro console=ttyS0 noapic reboot=k panic=1 pci=off nomodules systemd.journald.forward_to_console systemd.unit=firecracker.target init=/sbin/overlay-init

### Security ###

In order to ensure sufficient entropy is consistently available within 
the VM, the rootfs is configured to start the 
[`haveged`](https://manpages.debian.org/buster/haveged/haveged.8.en.html)
daemon during boot. [More information on its method of operation and other
details can be found in its FAQ](https://issihosts.com/haveged/faq.html).
Users of the image created by this utility are encouraged to evaluate 
`haveged` against their security requirements before running any
cryptographically-sensitive workloads inside their microVMs and containers.
