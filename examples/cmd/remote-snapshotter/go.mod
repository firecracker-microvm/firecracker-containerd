module github.com/firecracker-microvm/firecracker-containerd/example/remote-snapshotter

go 1.16

require (
	github.com/Microsoft/go-winio v0.6.0 // indirect
	github.com/containerd/containerd v1.6.10
	github.com/containerd/stargz-snapshotter v0.13.0
	github.com/firecracker-microvm/firecracker-containerd v0.0.0-20221104221814-24f1fcf99ebf
	github.com/opencontainers/selinux v1.10.2 // indirect
	go.opencensus.io v0.24.0 // indirect
	golang.org/x/tools v0.3.0 // indirect
)

replace github.com/firecracker-microvm/firecracker-containerd => ../../..
