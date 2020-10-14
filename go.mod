module github.com/firecracker-microvm/firecracker-containerd

require (
	github.com/Microsoft/hcsshim v0.8.10 // indirect
	github.com/Microsoft/hcsshim/test v0.0.0-20201001234239-936eeeb286fd // indirect
	github.com/StackExchange/wmi v0.0.0-20181212234831-e0a55b97c705 // indirect
	github.com/containerd/cgroups v0.0.0-20200824123100-0b889c03f102 // indirect
	github.com/containerd/console v0.0.0-20191219165238-8375c3424e4d // indirect
	github.com/containerd/containerd v1.4.1-0.20201014210714-22aea1e9a7a0
	github.com/containerd/continuity v0.0.0-20200928162600-f2cc35102c2a // indirect
	github.com/containerd/fifo v0.0.0-20200410184934-f15a3290365b
	github.com/containerd/go-runc v0.0.0-20190226155025-7d11b49dc076
	github.com/containerd/ttrpc v1.0.2
	github.com/containerd/typeurl v1.0.1
	github.com/containernetworking/cni v0.7.2-0.20190807151350-8c6c47d1c7fc
	github.com/containernetworking/plugins v0.8.6
	github.com/docker/go-metrics v0.0.0-20181218153428-b84716841b82 // indirect
	github.com/firecracker-microvm/firecracker-go-sdk v0.21.1-0.20200811001213-ee1e7c41b7bd
	github.com/go-ole/go-ole v1.2.4 // indirect
	github.com/gofrs/uuid v3.3.0+incompatible
	github.com/gogo/googleapis v1.4.0 // indirect
	github.com/gogo/protobuf v1.3.1
	github.com/golang/groupcache v0.0.0-20200121045136-8c9f03a8e57e // indirect
	github.com/golang/protobuf v1.4.2
	github.com/google/go-cmp v0.5.0 // indirect
	github.com/google/uuid v1.1.2 // indirect
	github.com/grpc-ecosystem/go-grpc-prometheus v1.2.0 // indirect
	github.com/hashicorp/go-multierror v1.0.0
	github.com/mdlayher/vsock v0.0.0-20190329173812-a92c53d5dcab
	github.com/miekg/dns v1.1.16
	github.com/opencontainers/image-spec v1.0.1 // indirect
	github.com/opencontainers/runc v1.0.0-rc9
	github.com/opencontainers/runtime-spec v1.0.2
	github.com/opencontainers/selinux v1.6.0 // indirect
	github.com/pkg/errors v0.9.1
	github.com/prometheus/client_golang v0.9.2 // indirect
	github.com/shirou/gopsutil v2.18.12+incompatible
	github.com/shirou/w32 v0.0.0-20160930032740-bb4de0191aa4 // indirect
	github.com/sirupsen/logrus v1.7.0
	github.com/stretchr/testify v1.6.1
	github.com/syndtr/gocapability v0.0.0-20200815063812-42c35b437635 // indirect
	github.com/vishvananda/netlink v1.1.0
	github.com/willf/bitset v1.1.11 // indirect
	go.opencensus.io v0.22.5 // indirect
	golang.org/x/net v0.0.0-20201010224723-4f7140c49acb // indirect
	golang.org/x/sync v0.0.0-20201008141435-b3e1573b7520
	golang.org/x/sys v0.0.0-20201014080544-cc95f250f6bc
	google.golang.org/genproto v0.0.0-20201014134559-03b6142f0dc9 // indirect
	google.golang.org/grpc v1.32.0
	gotest.tools/v3 v3.0.3 // indirect
)

replace (
	// Workaround for github.com/containerd/containerd issue #3031
	github.com/docker/distribution v2.7.1+incompatible => github.com/docker/distribution v2.7.1-0.20190205005809-0d3efadf0154+incompatible

	// Downgrade protobuf due to https://github.com/containerd/ttrpc/issues/62
	github.com/golang/protobuf v1.4.2 => github.com/golang/protobuf v1.3.5

	// Downgrade genproto to exclude protobuf v1.4.x
	google.golang.org/genproto v0.0.0-20201014134559-03b6142f0dc9 => google.golang.org/genproto v0.0.0-20200513103714-09dca8ec2884
)

go 1.11
