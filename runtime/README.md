# containerd-firecracker-runtime

This is the runtime component enabling containerd to control the Firecracker
VMM.  This component runs on the host, outside your microVM. In general, it
strives for
[OCI runtime](https://github.com/opencontainers/runtime-spec/blob/master/spec.md)
compliance within the bounds of Firecracker's feature set.

## Building

`make`

This will generate a `containerd-shim-aws-firecracker` binary in the current
working directory.

## Installation

Ensure that you have [containerd](https://github.com/containerd/containerd)
installed and configured.

Copy `containerd-shim-aws-firecracker` to /bin (or something else on the PATH)
following the naming guidelines for a containerd
[runtime v2](https://github.com/containerd/containerd/blob/master/runtime/v2/README.md)
shim:

	* If the runtime is invoked as aws.firecracker
	* Then the binary name needs to be containerd-shim-aws-firecracker

## Usage

Can invoke by downloading an image and doing 
`ctr run --runtime aws.firecracker <image-name> <id>`
