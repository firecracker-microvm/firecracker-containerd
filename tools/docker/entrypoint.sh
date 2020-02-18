#!/usr/bin/env bash
set -e

chmod a+rwx ${FICD_LOG_DIR}

mkdir -p /etc/containerd/snapshotter

if [[ -z "$FICD_DM_VOLUME_GROUP" ]]; then
   pool_name="${FICD_DM_POOL}"
else
   pool_name="$(echo "$FICD_DM_VOLUME_GROUP" | sed s/-/--/g)-${FICD_DM_POOL}"
fi

cat > /etc/containerd/snapshotter/devmapper.toml <<EOF
[plugins]
  [plugins.devmapper]
    pool_name = "${pool_name}"
    base_image_size = "1024MB"
EOF

touch ${FICD_CONTAINERD_OUTFILE}
chmod a+rw ${FICD_CONTAINERD_OUTFILE}
/usr/local/bin/containerd --log-level debug &>> ${FICD_CONTAINERD_OUTFILE} &

exec /bin/bash -c "$@"
