module github.com/firecracker-microvm/firecracker-containerd

go 1.23.0

require (
	github.com/awslabs/tc-redirect-tap v0.0.0-20240321154902-8932987843e8
	github.com/containerd/containerd v1.7.27
	github.com/containerd/containerd/api v1.8.0
	github.com/containerd/continuity v0.4.4
	github.com/containerd/fifo v1.1.0
	github.com/containerd/go-runc v1.0.0
	github.com/containerd/log v0.1.0
	github.com/containerd/ttrpc v1.2.7
	github.com/containerd/typeurl/v2 v2.2.0
	github.com/containernetworking/cni v1.2.3
	github.com/containernetworking/plugins v1.4.0
	github.com/firecracker-microvm/firecracker-go-sdk v0.22.1-0.20220427214706-47505a9cf951
	github.com/gofrs/uuid v3.3.0+incompatible
	github.com/golang/protobuf v1.5.4
	github.com/hashicorp/go-multierror v1.1.1
	github.com/miekg/dns v1.1.62
	github.com/opencontainers/image-spec v1.1.0
	github.com/opencontainers/runc v1.1.14
	github.com/opencontainers/runtime-spec v1.2.0
	github.com/pelletier/go-toml v1.9.5
	github.com/shirou/gopsutil v2.18.12+incompatible
	github.com/sirupsen/logrus v1.9.3
	github.com/stretchr/testify v1.10.0
	github.com/vishvananda/netlink v1.3.0
	go.uber.org/goleak v1.1.12
	golang.org/x/sync v0.11.0
	golang.org/x/sys v0.31.0
	google.golang.org/grpc v1.67.0
	google.golang.org/protobuf v1.35.2
)

require (
	dario.cat/mergo v1.0.0 // indirect
	github.com/AdaLogics/go-fuzz-headers v0.0.0-20230811130428-ced1acdcaa24 // indirect
	github.com/AdamKorcz/go-118-fuzz-build v0.0.0-20230306123547-8075edf89bb0 // indirect
	github.com/Microsoft/go-winio v0.6.2 // indirect
	github.com/Microsoft/hcsshim v0.11.7 // indirect
	github.com/StackExchange/wmi v0.0.0-20181212234831-e0a55b97c705 // indirect
	github.com/asaskevich/govalidator v0.0.0-20210307081110-f21760c49a8d // indirect
	github.com/beorn7/perks v1.0.1 // indirect
	github.com/blang/semver/v4 v4.0.0 // indirect
	github.com/cespare/xxhash/v2 v2.3.0 // indirect
	github.com/cilium/ebpf v0.11.0 // indirect
	github.com/containerd/cgroups v1.1.0 // indirect
	github.com/containerd/cgroups/v3 v3.0.3 // indirect
	github.com/containerd/console v1.0.4 // indirect
	github.com/containerd/errdefs v0.3.0 // indirect
	github.com/containerd/go-cni v1.1.9 // indirect
	github.com/containerd/imgcrypt v1.1.8 // indirect
	github.com/containerd/nri v0.8.0 // indirect
	github.com/containerd/platforms v0.2.1 // indirect
	github.com/containerd/typeurl v1.0.2 // indirect
	github.com/containers/ocicrypt v1.1.10 // indirect
	github.com/coreos/go-systemd/v22 v22.5.0 // indirect
	github.com/cpuguy83/go-md2man/v2 v2.0.4 // indirect
	github.com/davecgh/go-spew v1.1.1 // indirect
	github.com/distribution/reference v0.6.0 // indirect
	github.com/docker/go-events v0.0.0-20190806004212-e31b211e4f1c // indirect
	github.com/docker/go-metrics v0.0.1 // indirect
	github.com/docker/go-units v0.5.0 // indirect
	github.com/emicklei/go-restful/v3 v3.10.1 // indirect
	github.com/felixge/httpsnoop v1.0.4 // indirect
	github.com/fsnotify/fsnotify v1.6.0 // indirect
	github.com/go-jose/go-jose/v3 v3.0.3 // indirect
	github.com/go-logr/logr v1.4.2 // indirect
	github.com/go-logr/stdr v1.2.2 // indirect
	github.com/go-ole/go-ole v1.2.4 // indirect
	github.com/go-openapi/analysis v0.21.2 // indirect
	github.com/go-openapi/errors v0.20.2 // indirect
	github.com/go-openapi/jsonpointer v0.19.6 // indirect
	github.com/go-openapi/jsonreference v0.20.1 // indirect
	github.com/go-openapi/loads v0.21.1 // indirect
	github.com/go-openapi/runtime v0.23.3 // indirect
	github.com/go-openapi/spec v0.20.4 // indirect
	github.com/go-openapi/strfmt v0.21.2 // indirect
	github.com/go-openapi/swag v0.22.3 // indirect
	github.com/go-openapi/validate v0.21.0 // indirect
	github.com/go-stack/stack v1.8.1 // indirect
	github.com/godbus/dbus/v5 v5.1.0 // indirect
	github.com/gogo/protobuf v1.3.2 // indirect
	github.com/golang/groupcache v0.0.0-20210331224755-41bb18bfe9da // indirect
	github.com/google/go-cmp v0.6.0 // indirect
	github.com/google/gofuzz v1.2.0 // indirect
	github.com/google/uuid v1.6.0 // indirect
	github.com/grpc-ecosystem/go-grpc-middleware v1.3.0 // indirect
	github.com/grpc-ecosystem/go-grpc-prometheus v1.2.0 // indirect
	github.com/hashicorp/errwrap v1.1.0 // indirect
	github.com/intel/goresctrl v0.5.0 // indirect
	github.com/josharian/intern v1.0.0 // indirect
	github.com/json-iterator/go v1.1.12 // indirect
	github.com/klauspost/compress v1.17.9 // indirect
	github.com/klauspost/cpuid/v2 v2.0.4 // indirect
	github.com/mailru/easyjson v0.7.7 // indirect
	github.com/mdlayher/socket v0.5.1 // indirect
	github.com/mdlayher/vsock v1.2.1 // indirect
	github.com/miekg/pkcs11 v1.1.1 // indirect
	github.com/minio/sha256-simd v1.0.0 // indirect
	github.com/mitchellh/mapstructure v1.4.3 // indirect
	github.com/moby/locker v1.0.1 // indirect
	github.com/moby/spdystream v0.2.0 // indirect
	github.com/moby/sys/mountinfo v0.6.2 // indirect
	github.com/moby/sys/sequential v0.5.0 // indirect
	github.com/moby/sys/signal v0.7.0 // indirect
	github.com/moby/sys/symlink v0.2.0 // indirect
	github.com/moby/sys/user v0.3.0 // indirect
	github.com/moby/sys/userns v0.1.0 // indirect
	github.com/modern-go/concurrent v0.0.0-20180306012644-bacd9c7ef1dd // indirect
	github.com/modern-go/reflect2 v1.0.2 // indirect
	github.com/munnerz/goautoneg v0.0.0-20191010083416-a7dc8b61c822 // indirect
	github.com/oklog/ulid v1.3.1 // indirect
	github.com/opencontainers/go-digest v1.0.0 // indirect
	github.com/opencontainers/runtime-tools v0.9.1-0.20221107090550-2e043c6bd626 // indirect
	github.com/opencontainers/selinux v1.11.1 // indirect
	github.com/opentracing/opentracing-go v1.2.0 // indirect
	github.com/pkg/errors v0.9.1 // indirect
	github.com/pmezard/go-difflib v1.0.0 // indirect
	github.com/prometheus/client_golang v1.20.2 // indirect
	github.com/prometheus/client_model v0.6.1 // indirect
	github.com/prometheus/common v0.55.0 // indirect
	github.com/prometheus/procfs v0.15.1 // indirect
	github.com/russross/blackfriday/v2 v2.1.0 // indirect
	github.com/shirou/w32 v0.0.0-20160930032740-bb4de0191aa4 // indirect
	github.com/spf13/pflag v1.0.5 // indirect
	github.com/stefanberger/go-pkcs11uri v0.0.0-20230803200340-78284954bff6 // indirect
	github.com/syndtr/gocapability v0.0.0-20200815063812-42c35b437635 // indirect
	github.com/tchap/go-patricia/v2 v2.3.1 // indirect
	github.com/urfave/cli v1.22.15 // indirect
	github.com/vishvananda/netns v0.0.5 // indirect
	go.etcd.io/bbolt v1.3.10 // indirect
	go.mongodb.org/mongo-driver v1.8.3 // indirect
	go.mozilla.org/pkcs7 v0.0.0-20200128120323-432b2356ecb1 // indirect
	go.opencensus.io v0.24.0 // indirect
	go.opentelemetry.io/contrib/instrumentation/google.golang.org/grpc/otelgrpc v0.52.0 // indirect
	go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp v0.53.0 // indirect
	go.opentelemetry.io/otel v1.28.0 // indirect
	go.opentelemetry.io/otel/metric v1.28.0 // indirect
	go.opentelemetry.io/otel/trace v1.28.0 // indirect
	golang.org/x/crypto v0.35.0 // indirect
	golang.org/x/exp v0.0.0-20231006140011-7918f672742d // indirect
	golang.org/x/mod v0.21.0 // indirect
	golang.org/x/net v0.36.0 // indirect
	golang.org/x/oauth2 v0.22.0 // indirect
	golang.org/x/term v0.29.0 // indirect
	golang.org/x/text v0.22.0 // indirect
	golang.org/x/time v0.0.0-20220210224613-90d013bbcef8 // indirect
	golang.org/x/tools v0.26.0 // indirect
	google.golang.org/genproto v0.0.0-20240123012728-ef4313101c80 // indirect
	google.golang.org/genproto/googleapis/rpc v0.0.0-20240903143218-8af14fe29dc1 // indirect
	gopkg.in/inf.v0 v0.9.1 // indirect
	gopkg.in/yaml.v2 v2.4.0 // indirect
	gopkg.in/yaml.v3 v3.0.1 // indirect
	k8s.io/api v0.26.2 // indirect
	k8s.io/apimachinery v0.27.4 // indirect
	k8s.io/apiserver v0.26.2 // indirect
	k8s.io/client-go v0.26.2 // indirect
	k8s.io/component-base v0.26.2 // indirect
	k8s.io/cri-api v0.27.1 // indirect
	k8s.io/klog/v2 v2.90.1 // indirect
	k8s.io/utils v0.0.0-20230220204549-a5ecb0141aa5 // indirect
	sigs.k8s.io/json v0.0.0-20221116044647-bc3834ca7abd // indirect
	sigs.k8s.io/structured-merge-diff/v4 v4.2.3 // indirect
	sigs.k8s.io/yaml v1.4.0 // indirect
	tags.cncf.io/container-device-interface v0.8.1 // indirect
	tags.cncf.io/container-device-interface/specs-go v0.8.0 // indirect
)

// Workaround for indirect dependency no longer being available.
exclude github.com/mitchellh/osext v0.0.0-20151018003038-5e2d6d41470f

replace github.com/mitchellh/osext => github.com/mitchellh/osext v0.0.0-20151018003038-5e2d6d41470f
