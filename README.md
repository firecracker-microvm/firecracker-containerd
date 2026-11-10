# As of November 10 2026, this project is no longer maintained.

### TL;DR firecracker-containerd is no longer maintained. Please see [alternatives](#alternatives) for migrating workloads to more modern, secure alternatives.

---

We have made the decision to deprecate firecracker-containerd.

firecracker-containerd was created to combine the lightweight, friendly interface of containers with the security of kernel-level isolation. When the project started, this was a novel approach. Since then, the ecosystem has matured: several projects now deliver the same model with more robust implementations and active investment behind them. Amazon has done the same via [Nitro Sandboxes](https://docs.aws.amazon.com/AWSEC2/latest/UserGuide/amazon-ec2-nitro-sandboxes.html), which leverages Nitro Enclaves to achieve hardware-backed isolation for your container workloads.

Given those alternatives, we have archived firecracker-containerd rather than continue partial maintenance. The repository will remain available in read-only form for reference.

If you want to keep developing the project, you are welcome to fork it and maintain your own copy, provided you continue to follow the original [LICENSE](./LICENSE).

## Alternatives

Below are a list of alternatives that achieve the goal of kernel-level isolation for containerized workloads.
- [Nitro Sandboxes](https://docs.aws.amazon.com/AWSEC2/latest/UserGuide/amazon-ec2-nitro-sandboxes.html) - for folks who wish to have an Amazon-backed sandboxed container experience, this is the best option offered by Amazon that is currently supported.
  - Please note that this is currently closed-source, unlike firecracker-containerd.

All of the below alternatives are not owned or endorsed by Amazon, but they are open-source alternatives to achieve VM-level isolation for containers.

- [kata-containers](https://github.com/kata-containers/kata-containers) — likely the closest open-source alternative, supports multiple hypervisors including Firecracker
- [apple/container](https://github.com/apple/container) — useful for local dev work on Apple silicon machines
- [nerdbox](https://github.com/containerd/nerdbox) — runs on all platforms using libkrun hypervisor
- [Lima](https://github.com/lima-vm/lima) — used in conjunction with a container runtime to run Linux containers.
  - Note that Lima runs multiple containers inside of a single VM, which is a different architecture from firecracker-containerd, but may still be an option users want to consider

---

# firecracker-containerd

[![BuildKite](https://badge.buildkite.com/aab4ae547d5e5079a5915522e8cdb18492349aef67aae5a8c5.svg?branch=main)](https://buildkite.com/firecracker-microvm/firecracker-containerd)
[![GitHub Actions](https://github.com/firecracker-microvm/firecracker-containerd/actions/workflows/build.yaml/badge.svg)](https://github.com/firecracker-microvm/firecracker-containerd/actions)

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
  without the isolation compromise of such solutions.  Multi-tenant
  hosts would particularly benefit from this use case.
  * Please note that multi-tenant use cases are at the user's own risk.

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
guidelines](https://github.com/firecracker-microvm/firecracker/blob/main/SECURITY-POLICY.md).

Other discussion: For general discussion, please join us in the `#containerd`
channel on the [Firecracker Slack](https://join.slack.com/t/firecracker-microvm/shared_invite/zt-oxbm7tqt-GLlze9zZ7sdRSDY6OnXXHg).

## License

This library is licensed under the Apache 2.0 License.
