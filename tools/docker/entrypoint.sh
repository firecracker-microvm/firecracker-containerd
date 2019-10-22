#!/usr/bin/env bash
set -ex

chmod a+rwx ${FICD_LOG_DIR}

case "$FICD_SNAPSHOTTER" in
    naive)
        touch ${FICD_SNAPSHOTTER_OUTFILE}
        chmod a+rw ${FICD_SNAPSHOTTER_OUTFILE}
        /usr/local/bin/naive_snapshotter \
            -address /var/run/firecracker-containerd/naive-snapshotter.sock \
            -path /var/lib/firecracker-containerd/naive \
            -debug &>> ${FICD_SNAPSHOTTER_OUTFILE} &
        ;;
    *)
        "This Docker image doesn't support $FICD_SNAPSHOTTER snapshotter"
        exit 1
        ;;
esac

touch ${FICD_CONTAINERD_OUTFILE}
chmod a+rw ${FICD_CONTAINERD_OUTFILE}
/usr/local/bin/containerd --log-level debug &>> ${FICD_CONTAINERD_OUTFILE} &

exec /bin/bash -c "$@"
