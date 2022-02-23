module github.com/firecracker-microvm/firecracker-containerd

require (
	github.com/Microsoft/go-winio v0.5.0 // indirect
	github.com/StackExchange/wmi v0.0.0-20181212234831-e0a55b97c705 // indirect
	github.com/awslabs/tc-redirect-tap v0.0.0-20211025175357-e30dfca224c2
	github.com/bits-and-blooms/bitset v1.2.1 // indirect
	github.com/containerd/containerd v1.5.9
	github.com/containerd/continuity v0.2.0
	github.com/containerd/fifo v1.0.0
	github.com/containerd/go-cni v1.1.1 // indirect
	github.com/containerd/go-runc v1.0.0
	github.com/containerd/ttrpc v1.1.0
	github.com/containerd/typeurl v1.0.2
	github.com/containernetworking/cni v1.0.1
	github.com/containernetworking/plugins v1.0.1
	github.com/firecracker-microvm/firecracker-go-sdk v0.22.1-0.20220214213810-2380785d98b7
	github.com/go-ole/go-ole v1.2.4 // indirect
	github.com/gofrs/uuid v3.3.0+incompatible
	github.com/gogo/googleapis v1.4.1 // indirect
	github.com/gogo/protobuf v1.3.2
	github.com/golang/groupcache v0.0.0-20210331224755-41bb18bfe9da // indirect
	github.com/golang/protobuf v1.5.2
	github.com/hashicorp/go-multierror v1.1.1
	github.com/klauspost/compress v1.13.6 // indirect
	github.com/mdlayher/vsock v1.1.0
	github.com/miekg/dns v1.1.25
	github.com/opencontainers/image-spec v1.0.2
	github.com/opencontainers/runc v1.0.3
	github.com/opencontainers/runtime-spec v1.0.3-0.20210910115017-0d6cc581aeea
	github.com/opencontainers/selinux v1.8.5 // indirect
	github.com/pkg/errors v0.9.1
	github.com/shirou/gopsutil v2.18.12+incompatible
	github.com/shirou/w32 v0.0.0-20160930032740-bb4de0191aa4 // indirect
	github.com/sirupsen/logrus v1.8.1
	github.com/stretchr/testify v1.7.0
	github.com/vishvananda/netlink v1.1.1-0.20210330154013-f5de75959ad5
	go.opencensus.io v0.23.0 // indirect
	golang.org/x/sync v0.0.0-20210220032951-036812b2e83c
	golang.org/x/sys v0.0.0-20220209214540-3681064d5158
	google.golang.org/genproto v0.0.0-20211005153810-c76a74d43a8e // indirect
	google.golang.org/grpc v1.41.0
)

replace (
	// Pin gPRC-related dependencies as like containerd v1.5.x.
	github.com/gogo/googleapis => github.com/gogo/googleapis v1.3.2
	github.com/golang/protobuf => github.com/golang/protobuf v1.3.5

	// Upgrade mongo-driver before go-openapi packages update the package.
	go.mongodb.org/mongo-driver => go.mongodb.org/mongo-driver v1.5.1

	// Pin gPRC-related dependencies as like containerd v1.5.x.
	google.golang.org/genproto => google.golang.org/genproto v0.0.0-20200224152610-e50cd9704f63
	google.golang.org/grpc => google.golang.org/grpc v1.27.1
)

go 1.11
