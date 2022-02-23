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

touch ${FICD_CONTAINERD_OUTFILE}
chmod a+rw ${FICD_CONTAINERD_OUTFILE}
/usr/local/bin/containerd --log-level debug &>> ${FICD_CONTAINERD_OUTFILE} &

exec /bin/bash -c "$@"
