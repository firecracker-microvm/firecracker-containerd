# Host file isolation

firecracker-containerd has a number of host-based components, based on files,
that are accessible to a microVM and need to be isolated.  Isolation means that
one microVM does not have the ability to influence the execution of another
microVM or to gain access to data held by another microVM.  We believe this is
important both for security (i.e., one VM tenant cannot influence the execution
of another tenant's VM) and for repeatability (my container runs the same way
every time).

## File components

### Kernel

The kernel is the bootable component of the microVM.  We expect that users of
firecracker-containerd will either use the same kernel for all microVMs, or have
a small number of kernels shared among multiple microVMs.  Importantly, we do
not expect that each microVM will use a unique kernel and we do not want to
require a user of firecracker-containerd to provide separate kernels for each
microVM.

Because the kernel is shared among multiple microVMs, it must not be mutable by
any microVM.

### Root filesystem

Similar to the kernel, the root filesystem contains the user-mode components
that are invoked upon boot of a microVM.  Like the kernel, we expect users of
firecracker-containerd to either use the same root filesystem for all microVMs
or share a small number of root filesystems among multiple microVMs.  Also like
the kernel, we do not want to require a user of firecracker-containerd to
provide separate root filesystems for each microVM.

Because the backing root filesystem is shared among multiple microVMs, it must
not be mutable by any microVM.  MicroVMs which need to have a mutable root
filesystem would either need that facility to be provided by
firecracker-containerd or use a technique like "live CD" Linux distributions to
allow some ephemeral mutability.

### Firecracker and Jailer binaries

The Firecracker and Jailer binaries are used in the launch process and runtime
management of a VM.  We expect that every VM will use the same version of
Firecracker and Jailer.  While we do not expect there to be a mechanism by which
a running VM can write to either of these files, we do believe it's important to
enforce that expectation external to Firecracker and Jailer.

## Snapshotter and content store

The snapshotter and content store are technically backed by files, but
incorporate enough difference to warrant a separate document.

## Techniques

Any of the file components that need to be shared can be done so through several
basic ways:

* Reusing the same backing storage
* Copying (duplicating the content)
* Copy-on-write

There are a variety of techniques to accomplish each of these.

### Reusing storage

* Opening the same file
* Creating a hard-link to the same file and opening it
* Creating a symbolic-link to the same file and opening it
* Bind-mounting the file elsewhere on the filesystem and opening it
* Opening an existing file descriptor from the `/proc` filesystem

All of these allow for mutation of the original storage, unless protected by
another mechanism layered on top.  These are all very efficient mechanisms; they
do not incur additional latency or storage space.  However, we still need a
mechanism for preventing mutation.

### Copying

Copying ahead of launching a microVM provides assurance that the original file
will not be modified, as the original backing storage is not opened and not
accessible.  However, it has the downsides of being expensive in terms of time
(a latency impact on pulling an image or launching a new microVM) and expensive
in terms of space (scaling linearly with the number of microVMs we launch).

Copies can be performed to local, durable storage, to network-attached storage,
or to memory with a tmpfs or memfd.

### Copy-on-write

Copy-on-write techniques provide mechanisms for attempting to achieve the same
efficiency as reusing storage but allowing the safety of preventing modification
of the original storage.  There are a variety of copy-on-write solutions:

* Overlay filesystem (file-based copy-on-write storage, copy-up is performed
  when a file is opened for write)
* Devicemapper thin devices (block-based copy-on-write, copy-up is performed
  when a block is written)
* Filesystem-integrated copy-on-write (ZFS, BTRFS, XFS, etc)

Overlay has broad support as it is integrated with the Linux kernel and
Firecracker already requires a new-enough kernel.  Overlay is also simple to set
up.  We are already using devicemapper as our snapshotter, even though it is
more challenging to set up.  Filesystem-integrated copy-on-write approaches
require a filesystem that supports the feature; we do not currently require a
specific filesystem to run firecracker-containerd.

### Preventing mutation

Linux provides different capabilities for preventing mutation to files:

* POSIX permissions - These are the simplest to understand and rely on the
  kernel for enforcement.  If a file is read-only and the opening UID/GID does
  not have permission to write or to change the permissions, Linux will
  effectively prevent writes.  However, if a process is run as root or has the
  ability to escalate its privileges, it may be able to change the permission
  bitmask and make the file writable.
* Mounting a filesystem as read-only - Similar to POSIX permissions, this relies
  on the Linux kernel and the underlying filesystem implementation for
  enforcement.  If a filesystem is mounted as read-only, Linux will effectively
  prevent writes.  However, if a process is run as root or has the ability to
  escalate its privileges, it may be able to re-mount the filesystem as
  writable.
* Ensuring files are opened with `O_RDONLY` - The `open(2)` syscall provides a
  set of modes with which to open files.  The `O_RDONLY` mode asks the Linux
  kernel to prevent any calls to change the file through `write(2)` or other
  syscalls.
* File sealing - Supported with memory-backed file descriptors only (memfd),
  sealing enforces stronger permissions preventing modification, truncation,
  growth or changes in the permission set.

Firecracker also provides some capabilitie:

* Attaching the device as read-only - The emulated device visible to the microVM
  is presented as read-only, and the Firecracker VMM should prevent
  modification.  When attaching read-only, Firecracker opens the backing file as
  `O_RDONLY`, allowing the Linux kernel on the host to also enforce this
  restriction.

## Recommendation

For initial implementation, I recommend that we use POSIX permissions and
auditing `open(2)` calls to ensure `O_RDONLY`.

If for some reason we decide later that this is insufficient, we can look at a
copy-on-write technique.  I lean toward using overlay filesystems due to their
wide availability and support.
