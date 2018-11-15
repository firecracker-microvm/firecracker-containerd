# containerd-firecracker-runtime

This is the runtime component enabling containerd to control the Firecracker
VMM.  This component runs on the host, outside your microVM.

## Installation
copy to /bin (or something else on the PATH) following the naming guidelines.
	* If the runtime is invoked as aws.firecracker
	* Then the binary name needs to be containerd-shim-aws-firecracker

Requires containerd-shim-runc-v1 to be in /bin (or on PATH) also.

## Usage
Can invoke by downloading an image and doing 
`ctr run --runtime aws.firecracker <image-name> <id>`
