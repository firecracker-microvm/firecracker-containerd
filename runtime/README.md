copy to /bin (or something else on the PATH) following the naming guidelines.
	* If the runtime is invoked as aws.firecracker
	* Then the binary name needs to be containerd-shim-aws-firecracker

Requires containerd-shim-runc-v1 to be in /bin (or on PATH) also.

Can invoke by downloading and image and doing 
` ./ctr run --runtime io.containerd.cody.v2 <image-name> <id>`

