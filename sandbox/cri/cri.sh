#!/bin/bash
set -ex

mkdir /tmp/mnt
mount /var/lib/firecracker-containerd/runtime/hello-rootfs.ext4 /tmp/mnt
cp /firecracker-containerd/agent/agent /tmp/mnt/usr/local/bin
cp $(which runc) /tmp/mnt/usr/local/bin
cp /firecracker-containerd/sandbox/cri/fc-agent.start /tmp/mnt/etc/local.d
ln -s /etc/init.d/local /tmp/mnt/etc/runlevels/default/local
ln -s /etc/init.d/cgroups /tmp/mnt/etc/runlevels/default/cgroups
umount /tmp/mnt
naive_snapshotter -address /var/run/firecracker-containerd/snapshotter.sock -path /var/lib/firecracker-snapshotter &> /dev/null &
pid=$!

function finish {
    kill -3 $pid
}

trap finish EXIT
make test-cri
exec "$@"
