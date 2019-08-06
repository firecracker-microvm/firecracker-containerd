module github.com/firecracker-microvm/firecracker-containerd

require (
	github.com/Microsoft/go-winio v0.4.14 // indirect
	github.com/Microsoft/hcsshim v0.8.1 // indirect
	github.com/StackExchange/wmi v0.0.0-20181212234831-e0a55b97c705 // indirect
	github.com/containerd/cgroups v0.0.0-20181105182409-82cb49fc1779 // indirect
	github.com/containerd/console v0.0.0-20181022165439-0650fd9eeb50 // indirect
	github.com/containerd/containerd v1.2.1-0.20190711201753-f2b6c31d0fa7
	github.com/containerd/continuity v0.0.0-20181027224239-bea7585dbfac
	github.com/containerd/fifo v0.0.0-20180307165137-3d5202aec260
	github.com/containerd/go-runc v0.0.0-20190226155025-7d11b49dc076 // indirect
	github.com/containerd/ttrpc v0.0.0-20190613183316-1fb3814edf44
	github.com/containerd/typeurl v0.0.0-20181015155603-461401dc8f19
	github.com/coreos/go-systemd v0.0.0-20181031085051-9002847aa142 // indirect
	github.com/docker/distribution v2.7.1+incompatible // indirect
	github.com/docker/go-events v0.0.0-20170721190031-9461782956ad // indirect
	github.com/docker/go-metrics v0.0.0-20181218153428-b84716841b82 // indirect
	github.com/docker/go-units v0.3.3
	github.com/firecracker-microvm/firecracker-go-sdk v0.17.0
	github.com/go-ole/go-ole v1.2.4 // indirect
	github.com/godbus/dbus v0.0.0-20181025153459-66d97aec3384 // indirect
	github.com/gofrs/uuid v3.2.0+incompatible
	github.com/gogo/googleapis v1.1.0 // indirect
	github.com/gogo/protobuf v1.2.1
	github.com/golang/protobuf v1.2.0
	github.com/grpc-ecosystem/go-grpc-prometheus v1.2.0 // indirect
	github.com/hashicorp/go-multierror v1.0.0
	github.com/mdlayher/vsock v0.0.0-20190329173812-a92c53d5dcab
	github.com/opencontainers/go-digest v1.0.0-rc1 // indirect
	github.com/opencontainers/image-spec v1.0.1 // indirect
	github.com/opencontainers/runc v0.1.1 // indirect
	github.com/opencontainers/runtime-spec v0.1.2-0.20181106065543-31e0d16c1cb7
	github.com/pkg/errors v0.8.1
	github.com/prometheus/client_golang v0.9.2 // indirect
	github.com/shirou/gopsutil v2.18.12+incompatible
	github.com/shirou/w32 v0.0.0-20160930032740-bb4de0191aa4 // indirect
	github.com/sirupsen/logrus v1.4.1
	github.com/stretchr/testify v1.2.2
	github.com/syndtr/gocapability v0.0.0-20180916011248-d98352740cb2 // indirect
	github.com/urfave/cli v1.20.0 // indirect
	go.etcd.io/bbolt v1.3.1-etcd.8
	golang.org/x/sync v0.0.0-20181108010431-42b317875d0f
	golang.org/x/sys v0.0.0-20190507160741-ecd444e8653b
	google.golang.org/genproto v0.0.0-20181109154231-b5d43981345b // indirect
	google.golang.org/grpc v1.21.0
	gotest.tools v2.2.0+incompatible // indirect
)

// Workaround for github.com/containerd/containerd issue #3031
replace github.com/docker/distribution v2.7.1+incompatible => github.com/docker/distribution v2.7.1-0.20190205005809-0d3efadf0154+incompatible
