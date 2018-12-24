# firecracker-containerd

This repository enables the use of a container runtime,
[containerd](https://containerd.io), to manage
[Firecracker](https://github.com/firecracker-microvm/firecracker) microVMs.
Like traditional containers, Firecracker microVMs offer fast start-up and
shut-down and minimal overhead.  Unlike traditional containers, however, they
can provide an additional layer of isolation via the KVM hypervisor.

Potential use cases of Firecracker-based containers include:

* Sandbox a partially or fully untrusted third party container
  in its own microVM.  This would reduce the likelihood of
  leaking secrets via the third party container, for example.
* Bin-pack disparate container workloads on the same host,
  while maintaining a high level of isolation between containers.  Because
  the overhead of Firecracker is low, the achievable container
  density per host should be comparable to
  running containers using kernel-based container runtimes,
  without the isolation compromise of such solutions.  Multi-tentant
  hosts would particularly benefit from this use case.

To maintain compatibility with the container ecosystem, where possible, we use
container standards such as the OCI image format.

There are three separate components in this repository that enable containerd
to use Firecracker microVMs to run containers:

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
standards such as OCI and CNI. In addition, we intend to support launching multiple
containers inside of one microVM.  To support the widest variety of workloads,
the new runtime component has to work with popular container orchestration
frameworks such as Kubernetes and Amazon ECS, so we will work to ensure that the
software is conformant or compatible where necessary.

Details of specific roadmap items are tracked in [GitHub issues](https://github.com/firecracker-microvm/firecracker-containerd/issues).

## Questions?

Please use [GitHub issues](https://github.com/firecracker-microvm/firecracker-containerd/issues) to report problems, discuss roadmap items,
or make feature requests.

Other discussion: For general discussion, please join us in the `#containerd`
channel on the [Firecracker Slack](https://tinyurl.com/firecracker-microvm)

## Requirements

### Building

Each of the components requires Go 1.11 and utilizes Go modules.  You must have
a properly set up Go toolchain capable of building the components.

The devicemapper snapshotter requires the C-language libdevmapper headers to be
installed and available on your computer.  On Ubuntu, these headers can be
installed from the `libdevmapper-dev` package.

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
