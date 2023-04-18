module github.com/firecracker-microvm/firecracker-containerd

go 1.16

require (
	github.com/StackExchange/wmi v0.0.0-20181212234831-e0a55b97c705 // indirect
	github.com/awslabs/tc-redirect-tap v0.0.0-20211025175357-e30dfca224c2
	github.com/containerd/containerd v1.6.18
	github.com/containerd/continuity v0.3.0
	github.com/containerd/fifo v1.0.0
	github.com/containerd/go-runc v1.0.0
	github.com/containerd/ttrpc v1.1.0
	github.com/containerd/typeurl v1.0.2
	github.com/containernetworking/cni v1.1.1
	github.com/containernetworking/plugins v1.1.1
	github.com/firecracker-microvm/firecracker-go-sdk v0.22.1-0.20220427214706-47505a9cf951
	github.com/go-ole/go-ole v1.2.4 // indirect
	github.com/gofrs/uuid v3.3.0+incompatible
	github.com/gogo/googleapis v1.4.1 // indirect
	github.com/gogo/protobuf v1.3.2
	github.com/hashicorp/go-multierror v1.1.1
	github.com/klauspost/compress v1.15.6 // indirect
	github.com/miekg/dns v1.1.25
	github.com/moby/sys/mountinfo v0.6.2 // indirect
	github.com/moby/sys/signal v0.7.0 // indirect
	github.com/opencontainers/image-spec v1.0.3-0.20211202183452-c5a74bcca799
	github.com/opencontainers/runc v1.1.5
	github.com/opencontainers/runtime-spec v1.0.3-0.20210910115017-0d6cc581aeea
	github.com/pelletier/go-toml v1.9.5
	github.com/shirou/gopsutil v2.18.12+incompatible
	github.com/shirou/w32 v0.0.0-20160930032740-bb4de0191aa4 // indirect
	github.com/sirupsen/logrus v1.8.1
	github.com/stretchr/testify v1.7.1
	github.com/vishvananda/netlink v1.1.1-0.20210330154013-f5de75959ad5
	go.uber.org/goleak v1.1.12
	golang.org/x/sync v0.0.0-20220601150217-0de741cfad7f
	golang.org/x/sys v0.0.0-20220722155257-8c9f86f7a55f
	google.golang.org/genproto v0.0.0-20220617124728-180714bec0ad // indirect
	google.golang.org/grpc v1.47.0
)

replace (
	google.golang.org/genproto => google.golang.org/genproto v0.0.0-20200224152610-e50cd9704f63
	google.golang.org/grpc => google.golang.org/grpc v1.38.1
)
