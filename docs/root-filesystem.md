# Root Filesystem

firecracker-containerd requires a kernel and root filesystem to be specified for
the Firecracker virtual machine where containers run.  This document specifies
the hard requirements for that root filesystem, the development tools we built,
and some thoughts around where we might land for a production recommendation.

## Requirements

* The firecracker-containerd agent, configured to start on boot
* runc
* Standard Linux filesystem mounts like procfs and sysfs
* A cgroup v1 filesystem configured and mounted, with the controllers you intend
  to use for your containers (typical controllers include things like blkio,
  cpu, cpuacct, cpuset, devices, freezer, hugetlb, memory, net_cls, net_prio,
  perf_event, pids, and rdma, though you may not use all of them)
* Any supporting software you may be interested in leveraging inside the VM
  (e.g., a local caching DNS resolver)
* Any supporting dynamically-linked libraries necessary to run the components
  inside (e.g., libc, libseccomp, etc)
* (If sharing the root filesystem image among multiple VMs) Ability to run
  successfully from a read-only device, in order to prevent a VM from
  manipulating the filesystem in use by another VM.

## Development tools

We've built a tool to generate root filesystems that are suitable for
development.  The tool can be invoked with `make image` from the root directory
of this repository.  It will generate a Debian-based root filesystem as a
squashfs filesystem with the firecracker-containerd agent configured to start
through systemd, a runc binary built from the git submodule in the
`_submodules/runc` folder, and support for multiple VMs by an overlay filesystem
on top of the squashfs base.

To minimize on-disk overhead, we start with the "minbase" debootstrap
configuration, and only add minimal dependencies on top of it. We ensure that
obviously extraneous files such as the content of `/usr/share/doc` and
`/var/cache/apt` are removed. Further, we reduce startup latency by defining a
custom systemd target that minimizes the services that systemd tries to start at
boot.

More information on the tool and how to use it can be found in [the README.md
file](../tools/image-builder/README.md).

## Further thoughts

For production use, we want to achieve the following goals:

* Safe to share between multiple VMs
* Fast to initialize and start the agent
* Reasonably pared-down set of installed software

The current development filesystem meets the first goal already and has some
initial work toward the second and third goals, but has not been aggressively
optimized yet.  We're considering a few things, but are also open to other
ideas.  Some ideas we've talked about:

* Should we use a fully-featured init system like systemd, or something smaller?
* Should we statically- or dynamically-link the software inside the filesystem?
* What is the minimal set of software that is necessary?
* Should we embed runc/libcontainer inside the firecracker-containerd agent to
  reduce the requirement for both binaries?
* Should we enforce a size limitation for the writable layer in the VM?
* Should we care about disk I/O performance inside the VM?
