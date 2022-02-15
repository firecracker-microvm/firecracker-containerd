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
  # The 'plugins."io.containerd.grpc.v1.cri"' table contains all of the server options.
  [plugins."io.containerd.grpc.v1.cri"]
 
    # 'plugins."io.containerd.grpc.v1.cri".containerd' contains config related to containerd
    [plugins."io.containerd.grpc.v1.cri".containerd]

      # snapshotter is the snapshotter used by containerd.
      snapshotter = "devmapper"

      # default_runtime_name is the default runtime name to use.
      default_runtime_name = "containerd-shim-aws-firecracker"

      # 'plugins."io.containerd.grpc.v1.cri".containerd.runtimes' is a map from CRI RuntimeHandler strings, which specify types
      # of runtime configurations, to the matching configurations.
      # In this example, 'runc' is the RuntimeHandler string to match.
      [plugins."io.containerd.grpc.v1.cri".containerd.runtimes.containerd-shim-aws-firecracker]
        # runtime_type is the runtime type to use in containerd.
        # The default value is "io.containerd.runc.v2" since containerd 1.4.
        # The default value was "io.containerd.runc.v1" in containerd 1.3, "io.containerd.runtime.v1.linux" in prior releases.
        runtime_type = "aws.firecracker"


        # conf_dir is the directory in which the admin places a CNI conf.
        # this allows a different CNI conf for the network stack when a different runtime is being used.
        cni_conf_dir = "/etc/cni/net.d"

    # 'plugins."io.containerd.grpc.v1.cri".cni' contains config related to cni
    [plugins."io.containerd.grpc.v1.cri".cni]
      # bin_dir is the directory in which the binaries for the plugin is kept.
      bin_dir = "/opt/cni/bin"

      # conf_dir is the directory in which the admin places a CNI conf.
      conf_dir = "/etc/cni/net.d"

[debug]
  level = "debug"
EOF

touch ${FICD_CONTAINERD_OUTFILE}
chmod a+rw ${FICD_CONTAINERD_OUTFILE}
/usr/local/bin/containerd --log-level debug &>> ${FICD_CONTAINERD_OUTFILE} &

exec /bin/bash -c "$@"
