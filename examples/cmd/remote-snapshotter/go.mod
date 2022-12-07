module github.com/firecracker-microvm/firecracker-containerd/example/remote-snapshotter

go 1.16

require (
	github.com/containerd/containerd v1.6.12
	github.com/containerd/stargz-snapshotter v0.11.3
	github.com/firecracker-microvm/firecracker-containerd v0.0.0-20220430002346-5f6efb9fdce8
)

replace github.com/firecracker-microvm/firecracker-containerd => ../../..
