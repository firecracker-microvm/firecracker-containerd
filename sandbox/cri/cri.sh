#!/bin/bash
mkdir /tmp/mnt
sudo mount /var/lib/firecracker-containerd/runtime/hello-rootfs.ext4 /tmp/mnt
sudo cp /firecracker-containerd/agent/agent /tmp/mnt/usr/local/bin
sudo cp $(which runc) /tmp/mnt/usr/local/bin
sudo cp /firecracker-containerd/sandbox/cri/fc-agent.start /tmp/mnt/etc/local.d
#sudo umount /tmp/mnt

set -ex

naive_snapshotter -address /var/run/firecracker-containerd/naive-snapshotter.sock -path /var/lib/firecracker-snapshotter &> /dev/null &
pid=$!
make test-cri
kill -9 $pid

exec "$@"

cp /tmp/mnt/container/agent-debug.log /foo
sudo umount /tmp/mnt
