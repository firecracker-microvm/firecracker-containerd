# firecracker-containerd

This repository enables the use of a container runtime,
[containerd](https://containerd.io), to manage
[Firecracker](https://github.com/firecracker-microvm/firecracker) microVMs in
order to achieve a strong isolation boundary around containers.  Firecracker
microVMs have container-like properties such as fast start-up and shut-down and
minimal overhead, and they can be used to provide a separate kernel and KVM
hypervisor isolation. Examples of potential use cases that might benefit from
stronger container isolation include the desire to sandbox untrusted third party
code and the need to separate disparate workloads that are bin-packed into one
server.

To maintain compatibility with the container ecosystem, where possible, we use
container technologies such as OCI images.

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

## Roadmap

Initially, this project allows you to launch one container per microVM.  We
intend it to be a drop-in component that can run a variety of containerized
applications, so the short term roadmap contains work to support container
standards such as OCI and CNI. In addition, we will support launching multiple
containers inside of one microVM.  To support the widest variety of workloads,
the new runtime component has to work with popular container orchestration
frameworks such as Kubernetes and Amazon ECS, so we will work to ensure that the
software is conformant or compatible where necessary.

Details of specific roadmap items are tracked in [GitHub issues](issues).

## Questions?

About Features/Use cases: [Link to Github Issues] About Usage clarifications/
Issues: [Link to Github Issues] Other discussion:
[Get invited to #containers on AWS Developers [awsdevelopers.slack.com]]

## Requirements

### Building

Each of the components requires Go 1.11 and utilizes Go modules.  You must have
a properly set up Go toolchain capable of building the components.

### Running

You must have the following components available in order to run Firecracker
microVMs with containerd:

* containerd >= 1.2
* Firecracker >= 0.10.1 with [vsock support](https://github.com/firecracker-microvm/firecracker/blob/master/docs/experimental-vsock.md) enabled.
* A firecracker compatible kernel
* A filesystem image for the microVM, including the [agent](agent)
  component configured to start on boot
* The [snapshotter](snapshotter) component, configured to be loaded by containerd
* The [runtime](runtime) component, configured to be loaded by containerd

## License

This library is licensed under the Apache 2.0 License.
