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

The image currently expects `vdb` to be the block device containing
the container root filesystem, and this device is mounted on
`/container/rootfs`.

If the `vsock_srv` program from the
[clownix](https://github.com/clownix/cloonix_vsock) github repository
is present in `ephemeral_files/bin/` when the image is built, it will
be embedded in the image and run at boot, allowing easy shell access
to the VM. This can be convenient for debugging, but it is not
embedded by default for security and licensing reasons.

### Generation ###

There are two alternatives for providing the build environment. You
can perform the image build in Docker, in which case the only
build-time dependency is that you can launch Docker container directly
(i.e. without `sudo`, etc). To build an image in this configuration, use:

`$ make rootfs.img-in-docker`

Alternatively, to build outside a container, you'll need:

* To run the build process as root.
* [`debootstrap`](https://salsa.debian.org/installer-team/debootstrap)
  (Install via the package of the same name on Debian and Ubuntu)
* `mksquashfs`, available in the
   `[squashfs-tools](https://packages.debian.org/stretch/squashfs-tools)
   package on Debian and Ubuntu.

Then execute `make rootfs.img`
