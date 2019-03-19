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

## Configuration

The runtime expects a JSON-formatted configuration file to be located either in
`/etc/containerd/firecracker-runtime.json` or in a location defined by the
`FIRECRACKER_CONTAINERD_RUNTIME_CONFIG_PATH` environment variable.  The
configuration file has the following fields:

* `firecracker_binary_path` (optional) - A path to locate the `firecracker`
  executable.  If left undefined, the runtime looks for an executable named
  `firecracker` located in its working directory.  A fully-qualified path to the
  `firecracker` binary is recommended, as the working directory typically
  changes every execution when run by containerd.
* `kernel_image_path` (required) - A path where the kernel image file is
  located.  A fully-qualified path is recommended.
* `kernel_args` (required) - Arguments for the kernel command line.
* `root_drive` (required) - A path where the root drive image file is located. A
  fully-qualified path is recommended.
* `cpu_count` (required) - The number of vCPUs to make available to a microVM.
* `cpu_template` (required) - The Firecracker CPU emulation template.  Supported
  values are "C3" and "T2".
* `additional_drives` (unused)
* `console` (optional) - How the console device should be handled.  Supported
  values are "" (blank), "stdio", and "xterm".  Setting "xterm" will launch a
  new xterm instance and requires a running X server.
* `log_fifo` (optional) - Named pipe where Firecracker logs should be delivered.
* `log_level` (optional) - Log level for the Firecracker logs
* `metrics_fifo` (optional) - Named pipe where Firecracker metrics should be
  delivered.
* `ht_enabled` (unused) - Reserved for future use.
* `debug` (optional) - Enable debug-level logging from the runtime.

## Usage

Can invoke by downloading an image and doing 
`ctr run --runtime aws.firecracker <image-name> <id>`
