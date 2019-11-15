#!/usr/bin/env bash
set -e

chmod a+rwx ${FICD_LOG_DIR}

mkdir -p /etc/containerd/snapshotter

case "$FICD_SNAPSHOTTER" in
    naive)
        cat > /etc/containerd/snapshotter/naive.toml <<EOF
[proxy_plugins]
  [proxy_plugins.firecracker-naive]
    type = "snapshot"
    address = "/var/run/firecracker-containerd/naive-snapshotter.sock"
EOF

        touch ${FICD_SNAPSHOTTER_OUTFILE}
        chmod a+rw ${FICD_SNAPSHOTTER_OUTFILE}
        /usr/local/bin/naive_snapshotter \
            -address /var/run/firecracker-containerd/naive-snapshotter.sock \
            -path /var/lib/firecracker-containerd/naive \
            -debug &>> ${FICD_SNAPSHOTTER_OUTFILE} &
        ;;
    devmapper)
        cat > /etc/containerd/snapshotter/devmapper.toml <<EOF
[plugins]
  [plugins.devmapper]
    pool_name = "fcci--vg-${FICD_DM_POOL}"
    base_image_size = "1024MB"
EOF
        ;;
    *)
        echo "This Docker image doesn't support $FICD_SNAPSHOTTER snapshotter"
        exit 1
        ;;
esac

touch ${FICD_CONTAINERD_OUTFILE}
chmod a+rw ${FICD_CONTAINERD_OUTFILE}
/usr/local/bin/containerd --log-level debug &>> ${FICD_CONTAINERD_OUTFILE} &

exec /bin/bash -c "$@"
