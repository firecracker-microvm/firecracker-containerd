module github.com/firecracker-microvm/firecracker-containerd

require (
	github.com/Microsoft/go-winio v0.4.14 // indirect
	github.com/Microsoft/hcsshim v0.8.1 // indirect
	github.com/StackExchange/wmi v0.0.0-20181212234831-e0a55b97c705 // indirect
	github.com/containerd/cgroups v0.0.0-20181105182409-82cb49fc1779 // indirect
	github.com/containerd/console v0.0.0-20181022165439-0650fd9eeb50 // indirect
	github.com/containerd/containerd v1.3.0-beta.2
	github.com/containerd/continuity v0.0.0-20181027224239-bea7585dbfac
	github.com/containerd/fifo v0.0.0-20190816180239-bda0ff6ed73c
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
	github.com/opencontainers/runc v1.0.0-rc8
	github.com/opencontainers/runtime-spec v1.0.2-0.20190207185410-29686dbc5559
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

replace k8s.io/api => k8s.io/api v0.0.0-20190620084959-7cf5895f2711

replace k8s.io/apiextensions-apiserver => k8s.io/apiextensions-apiserver v0.0.0-20190620085554-14e95df34f1f

replace k8s.io/apimachinery => k8s.io/apimachinery v0.0.0-20190612205821-1799e75a0719

replace k8s.io/apiserver => k8s.io/apiserver v0.0.0-20190620085212-47dc9a115b18

replace k8s.io/cli-runtime => k8s.io/cli-runtime v0.0.0-20190620085706-2090e6d8f84c

replace k8s.io/client-go => k8s.io/client-go v0.0.0-20190620085101-78d2af792bab

replace k8s.io/cloud-provider => k8s.io/cloud-provider v0.0.0-20190620090043-8301c0bda1f0

replace k8s.io/cluster-bootstrap => k8s.io/cluster-bootstrap v0.0.0-20190620090013-c9a0fc045dc1

replace k8s.io/code-generator => k8s.io/code-generator v0.0.0-20190612205613-18da4a14b22b

replace k8s.io/component-base => k8s.io/component-base v0.0.0-20190620085130-185d68e6e6ea

replace k8s.io/cri-api => k8s.io/cri-api v0.0.0-20190531030430-6117653b35f1

replace k8s.io/csi-translation-lib => k8s.io/csi-translation-lib v0.0.0-20190620090116-299a7b270edc

replace k8s.io/kube-aggregator => k8s.io/kube-aggregator v0.0.0-20190620085325-f29e2b4a4f84

replace k8s.io/kube-controller-manager => k8s.io/kube-controller-manager v0.0.0-20190620085942-b7f18460b210

replace k8s.io/kube-proxy => k8s.io/kube-proxy v0.0.0-20190620085809-589f994ddf7f

replace k8s.io/kube-scheduler => k8s.io/kube-scheduler v0.0.0-20190620085912-4acac5405ec6

replace k8s.io/kubectl => k8s.io/kubectl v0.0.0-20190602132728-7075c07e78bf

replace k8s.io/kubelet => k8s.io/kubelet v0.0.0-20190620085838-f1cb295a73c9

replace k8s.io/legacy-cloud-providers => k8s.io/legacy-cloud-providers v0.0.0-20190620090156-2138f2c9de18

replace k8s.io/metrics => k8s.io/metrics v0.0.0-20190620085625-3b22d835f165

replace k8s.io/node-api => k8s.io/node-api v0.0.0-20190620090231-de81913bee0f

replace k8s.io/sample-apiserver => k8s.io/sample-apiserver v0.0.0-20190620085408-1aef9010884e

replace k8s.io/sample-cli-plugin => k8s.io/sample-cli-plugin v0.0.0-20190620085735-97320335c8c8

replace k8s.io/sample-controller => k8s.io/sample-controller v0.0.0-20190620085445-e8a5bf14f039

replace github.com/Sirupsen/logrus => github.com/sirupsen/logrus v1.0.5

