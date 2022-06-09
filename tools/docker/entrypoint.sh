#!/usr/bin/env bash
set -e

chmod a+rwx ${FICD_LOG_DIR}

mkdir -p /etc/containerd/snapshotter
mkdir -p /etc/containerd/cri

if [[ -z "$FICD_DM_VOLUME_GROUP" ]]; then
   pool_name="${FICD_DM_POOL}"
else
   pool_name="$(echo "$FICD_DM_VOLUME_GROUP" | sed s/-/--/g)-${FICD_DM_POOL}"
fi

cat > /etc/containerd/snapshotter/devmapper.toml <<EOF
version = 2
[plugins]
  [plugins."io.containerd.snapshotter.v1.devmapper"]
    pool_name = "${pool_name}"
    base_image_size = "1024MB"
EOF

cat > /etc/containerd/cri/criconfig.toml <<EOF
version = 2
[plugins]
  [plugins."io.containerd.grpc.v1.cri"]
    [plugins."io.containerd.grpc.v1.cri".containerd]
      snapshotter = "devmapper"
      default_runtime_name = "containerd-shim-aws-firecracker"

      [plugins."io.containerd.grpc.v1.cri".containerd.runtimes.containerd-shim-aws-firecracker]
        runtime_type = "aws.firecracker"
        cni_conf_dir = "/etc/cni/net.d"

    [plugins."io.containerd.grpc.v1.cri".cni]
      bin_dir = "/opt/cni/bin"
      conf_dir = "/etc/cni/net.d"

[debug]
  level = "debug"
EOF

mkdir -p /etc/demux-snapshotter /var/lib/demux-snapshotter
cat > /etc/demux-snapshotter/config.toml <<EOF
[snapshotter.proxy.address.resolver]
  type = "http"
  address = "http://127.0.0.1:10001"
[snapshotter.metrics]
  enable = true
  port_range = "9000-9999"
  host = "0.0.0.0"
  service_discovery_port = 8080
[debug]
  logLevel = "debug"
EOF

touch ${FICD_CONTAINERD_OUTFILE}
chmod a+rw ${FICD_CONTAINERD_OUTFILE}
/usr/local/bin/containerd --log-level debug &>> ${FICD_CONTAINERD_OUTFILE} &

/usr/local/bin/http-address-resolver &>> ${FICD_LOG_DIR}/http-address-resolver.out &
/usr/local/bin/demux-snapshotter &>> ${FICD_LOG_DIR}/demux-snapshotter.out &

exec /bin/bash -c "$@"
