# containerd-firecracker-agent

The containerd Firecracker agent is a component which runs inside
containerd-managed Firecracker microVMs.  The containerd Firecracker agent
communicates with the containerd-firecracker-runtime over a vsock and proxies
commands to `runc`

## Installation

The containerd Firecracker agent must be embedded into the filesystem image used
to launch the microVM and configured to start on boot.

## Usage

Once started and set up with a properly-configured vsock, the containerd
Firecracker agent is used automatically by the `containerd-shim-aws-firecracker`
process running outside the microVM.
