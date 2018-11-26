# firecracker-containerd

This repository contains software that enables using
[containerd](https://containerd.io) to manage
[Firecracker](https://github.com/firecracker-microvm/firecracker) microVMs
using familiar container ecosystem technologies like OCI images.

Firecracker microVMs enable a container-like experience with fast start-up and
shut-down while providing a separate kernel and KVM hypervisor isolation.

There are three separate components in this repository:

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
  [runC](https://runc.io) via containerd's `containerd-shim-runc-v1`
  to create standard Linux containers inside the microVM.
  
For more detailed information on the components and how they work, see
[architecture.md](docs/architecture.md).

_Please note that this software is still early in its development and
it lacks some basic features. See the [TODO](TODO.md) file for a
partial list._

## Requirements

### Building

Each of the components requires Go 1.11 and utilizes Go modules.  You must have
a properly set up Go toolchain capable of building the components.

### Running

You must have the following components available in order to run Firecracker
microVMs with containerd:

* containerd >= 1.2
* Firecracker >= 0.10.1
* A filesystem image of the Linux kernel (TODO: provide this)
* A filesystem image for the microVM, which must contain `runc` and the `agent`
  component, configured to start on boot (TODO: provide this)
* The `snapshotter` component, configured to be loaded by containerd
* The `runtime` component, configured to be loaded by containerd

## License

This library is licensed under the Apache 2.0 License.
