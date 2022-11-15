module github.com/firecracker-microvm/firecracker-containerd

go 1.16

require (
	github.com/awslabs/tc-redirect-tap v0.0.0-20221111211845-b589b55eaf3c
	github.com/containerd/cgroups v1.0.4 // indirect
	github.com/containerd/containerd v1.6.10
	github.com/containerd/continuity v0.3.0
	github.com/containerd/fifo v1.0.0
	github.com/containerd/go-runc v1.0.0
	github.com/containerd/ttrpc v1.1.0
	github.com/containerd/typeurl v1.0.2
	github.com/containernetworking/cni v1.1.2
	github.com/containernetworking/plugins v1.1.1
	github.com/firecracker-microvm/firecracker-go-sdk v1.0.0
	github.com/gofrs/uuid v4.3.1+incompatible
	github.com/gogo/googleapis v1.4.1 // indirect
	github.com/gogo/protobuf v1.3.2
	github.com/hashicorp/go-multierror v1.1.1
	github.com/klauspost/compress v1.15.12 // indirect
	github.com/miekg/dns v1.1.50
	github.com/moby/sys/mountinfo v0.6.2 // indirect
	github.com/moby/sys/signal v0.7.0 // indirect
	github.com/opencontainers/image-spec v1.1.0-rc2
	github.com/opencontainers/runc v1.1.4
	github.com/opencontainers/runtime-spec v1.0.3-0.20210910115017-0d6cc581aeea
	github.com/pelletier/go-toml v1.9.5
	github.com/shirou/gopsutil v3.21.11+incompatible
	github.com/sirupsen/logrus v1.9.0
	github.com/stretchr/testify v1.8.1
	github.com/tklauser/go-sysconf v0.3.11 // indirect
	github.com/vishvananda/netlink v1.2.1-beta.2
	github.com/yusufpapurcu/wmi v1.2.2 // indirect
	go.uber.org/goleak v1.2.0
	golang.org/x/net v0.2.0 // indirect
	golang.org/x/sync v0.1.0
	golang.org/x/sys v0.2.0
	google.golang.org/genproto v0.0.0-20221114212237-e4508ebdbee1 // indirect
	google.golang.org/grpc v1.50.1
	google.golang.org/protobuf v1.28.1 // indirect
)

replace (
	google.golang.org/genproto => google.golang.org/genproto v0.0.0-20200224152610-e50cd9704f63
	google.golang.org/grpc => google.golang.org/grpc v1.38.1
)
