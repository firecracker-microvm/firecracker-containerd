# firecracker-containerd architecture

This repository contains several components with the goal of enabling the
[containerd](https://containerd.io) container runtime to create and manage
[Firecracker](https://github.com/firecracker-microvm/firecracker) microVMs.

These components are implemented using containerd's extension mechanisms, so
you can configure your existing installation of containerd to work with
Firecracker microVMs.

There are currently three components in this repository:

* A [snapshotter](snapshotter) that creates files used as block-devices for
  pass-through into the microVM.  This snapshotter is used for providing the
  container image to the microVM.  The snapshotter runs as an out-of-process
  gRPC proxy plugin.
* A [runtime](runtime) linking containerd (outside the microVM) to the
  Firecracker virtual machine manager (VMM).  The runtime is implemented as an
  out-of-process
  [shim runtime](https://github.com/containerd/containerd/issues/2426)
  communicating over ttrpc.
* An [agent](agent) running inside the microVM, which invokes
  [runC](https://runc.io) to create standard Linux containers inside the
  microVM.
  
We expect to add at least one additional component to help enable networking
with microVMs in the future.

A high-level diagram of the various components and their interactions can be
seen below:

![firecracker-containerd architecture](architecture-diagram.png)