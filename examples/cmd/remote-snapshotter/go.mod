module github.com/firecracker-microvm/firecracker-containerd/example/remote-snapshotter

go 1.16

require (
	github.com/containerd/containerd v1.6.3
	github.com/containerd/stargz-snapshotter v0.11.3
	github.com/firecracker-microvm/firecracker-containerd v0.0.0-20220430002346-5f6efb9fdce8
)

require (
	github.com/containerd/continuity v0.3.0 // indirect
	google.golang.org/genproto v0.0.0-20220107163113-42d7afdf6368 // indirect

)

replace github.com/firecracker-microvm/firecracker-containerd => ../../..
