# firecracker-containerd

| Automation | Status |
|------------|--------|
| Tests      | [![Build status](https://badge.buildkite.com/aab4ae547d5e5079a5915522e8cdb18492349aef67aae5a8c5.svg?branch=master)](https://buildkite.com/firecracker-microvm/firecracker-containerd)
| Lint       | [![Build Status](https://travis-ci.org/firecracker-microvm/firecracker-containerd.svg?branch=master)](https://travis-ci.org/firecracker-microvm/firecracker-containerd)

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

There are several components in this repository that enable containerd to use
Firecracker microVMs to run containers:

* A [control plugin](firecracker-control) managing the lifecycle of the
  runtime and implementing our [control API](proto/firecracker.proto) to
  manage the lifecycle of microVMs. The control plugin is compiled in to the
  containerd binary since [building a Go plugin out-of-tree is hard](https://github.com/golang/go/issues/20481),
  which requires us to build a specialized containerd binary for
  firecracker-containerd.
* A [runtime](runtime) linking containerd (outside the microVM) to the
  Firecracker virtual machine monitor (VMM).  The runtime is implemented as an
  out-of-process
  [shim runtime](https://github.com/containerd/containerd/issues/2426)
  communicating over ttrpc.
* An [agent](agent) running inside the microVM, which invokes
  [runC](https://runc.io) via containerd's `containerd-shim-runc-v1`
  to create standard Linux containers inside the microVM.
* A [root file filesystem image builder](tools/image-builder) that
  constructs a firecracker microVM root filesystem containing runc and
  the firecracker-containerd agent.

For more detailed information on the components and how they work, see
[architecture.md](docs/architecture.md).

## Roadmap

To support the widest variety of workloads, firecracker-containerd has to work
with popular container orchestration frameworks such as Kubernetes and Amazon
ECS, so we will work to ensure that the software is conformant or compatible
where necessary.  The project currently allows you to launch a few containers
colocated in the same microVM, and we are exploring how to raise the number of
containers.  We recently added support for configuring networking at the microVM
level with CNI plugins and provide a CNI plugin suitable for chaining called
"tc-redirect-tap".  Our short term roadmap includes constraining or "jailing"
the Firecracker VMM process to improve the host security posture.  Our
longer-term roadmap includes polishing, packaging, and generally making
firecracker-containerd easier to run as well as exploring CRI conformance and
compatibility with Kubernetes.

Details of specific roadmap items are tracked in [GitHub
issues](https://github.com/firecracker-microvm/firecracker-containerd/issues).

## Usage

For detailed instructions on building and running
firecracker-containerd, see the
[getting started guide](docs/getting-started.md) and the
[quickstart guide](docs/quickstart.md).

## Questions?

Please use [GitHub
issues](https://github.com/firecracker-microvm/firecracker-containerd/issues) to
report problems, discuss roadmap items, or make feature requests.

If you've discovered an issue that may have security implications to
users or developers of this software, please do not report it using
GitHub issues, but instead follow
[Firecracker's security reporting
guidelines](https://github.com/firecracker-microvm/firecracker/blob/master/SECURITY-POLICY.md).

Other discussion: For general discussion, please join us in the `#containerd`
channel on the [Firecracker Slack](https://tinyurl.com/firecracker-microvm).

## License

This library is licensed under the Apache 2.0 License.
