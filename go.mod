module github.com/firecracker-microvm/firecracker-containerd

require (
	github.com/Microsoft/go-winio v0.4.14 // indirect
	github.com/StackExchange/wmi v0.0.0-20181212234831-e0a55b97c705 // indirect
	github.com/awslabs/tc-redirect-tap v0.0.0-20200708224642-a0300978797d
	github.com/containerd/cgroups v0.0.0-20181105182409-82cb49fc1779 // indirect
	github.com/containerd/console v0.0.0-20191219165238-8375c3424e4d // indirect
	github.com/containerd/containerd v1.3.8-0.20200824223617-f99bb2cc4483
	github.com/containerd/continuity v0.0.0-20181027224239-bea7585dbfac // indirect
	github.com/containerd/fifo v0.0.0-20191213151349-ff969a566b00
	github.com/containerd/go-runc v0.0.0-20190226155025-7d11b49dc076
	github.com/containerd/ttrpc v0.0.0-20190613183316-1fb3814edf44
	github.com/containerd/typeurl v0.0.0-20181015155603-461401dc8f19
	github.com/containernetworking/cni v0.8.0
	github.com/containernetworking/plugins v0.8.7
	github.com/coreos/go-systemd v0.0.0-20181031085051-9002847aa142 // indirect
	github.com/docker/distribution v2.7.1+incompatible // indirect
	github.com/docker/go-events v0.0.0-20170721190031-9461782956ad // indirect
	github.com/docker/go-metrics v0.0.0-20181218153428-b84716841b82 // indirect
	github.com/firecracker-microvm/firecracker-go-sdk v0.22.1-0.20201117001223-cd822c0d457c
	github.com/go-ole/go-ole v1.2.4 // indirect
	github.com/godbus/dbus v0.0.0-20181025153459-66d97aec3384 // indirect
	github.com/gofrs/uuid v3.3.0+incompatible
	github.com/gogo/googleapis v1.1.0 // indirect
	github.com/gogo/protobuf v1.3.0
	github.com/golang/protobuf v1.3.1
	github.com/grpc-ecosystem/go-grpc-prometheus v1.2.0 // indirect
	github.com/hashicorp/go-multierror v1.1.0
	github.com/imdario/mergo v0.3.8 // indirect
	github.com/konsorten/go-windows-terminal-sequences v1.0.3 // indirect
	github.com/mdlayher/vsock v0.0.0-20190329173812-a92c53d5dcab
	github.com/miekg/dns v1.1.16
	github.com/opencontainers/go-digest v1.0.0-rc1 // indirect
	github.com/opencontainers/image-spec v1.0.1 // indirect
	github.com/opencontainers/runc v1.0.0-rc9
	github.com/opencontainers/runtime-spec v0.1.2-0.20181106065543-31e0d16c1cb7
	github.com/pkg/errors v0.9.1
	github.com/prometheus/client_golang v0.9.2 // indirect
	github.com/shirou/gopsutil v2.18.12+incompatible
	github.com/shirou/w32 v0.0.0-20160930032740-bb4de0191aa4 // indirect
	github.com/sirupsen/logrus v1.7.0
	github.com/stretchr/testify v1.6.1
	github.com/syndtr/gocapability v0.0.0-20180916011248-d98352740cb2 // indirect
	github.com/urfave/cli v1.20.0 // indirect
	github.com/vishvananda/netlink v1.1.0
	go.etcd.io/bbolt v1.3.1-etcd.8 // indirect
	golang.org/x/sync v0.0.0-20190911185100-cd5d95a43a6e
	golang.org/x/sys v0.0.0-20200323222414-85ca7c5b95cd
	google.golang.org/genproto v0.0.0-20181109154231-b5d43981345b // indirect
	google.golang.org/grpc v1.21.0
	gotest.tools v2.2.0+incompatible // indirect
)

// Workaround for github.com/containerd/containerd issue #3031
replace github.com/docker/distribution v2.7.1+incompatible => github.com/docker/distribution v2.7.1-0.20190205005809-0d3efadf0154+incompatible

go 1.11
