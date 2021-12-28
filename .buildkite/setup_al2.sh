#!/bin/bash

set -eux

source ./.buildkite/al2env.sh

mkdir -p $dir
mkdir -p $dir/rootfs
mkdir -p $bin_path
mkdir -p $devmapper_path
mkdir -p $state_path

./tools/thinpool.sh reset $unique_id

export INSTALLROOT=$dir
export FIRECRACKER_CONTAINERD_RUNTIME_DIR=$dir
make
cp /var/lib/fc-ci/vmlinux.bin $dir/default-vmlinux.bin
make image firecracker
sudo -E INSTALLROOT=$INSTALLROOT PATH=$PATH \
     make install install-firecracker install-default-rootfs

cat << EOF > $dir/config.toml
version = 2
disabled_plugins = ["io.containerd.grpc.v1.cri"]
root = "$dir"
state = "$state_path"
[grpc]
  address = "$dir/containerd.sock"
[plugins]
  [plugins."io.containerd.snapshotter.v1.devmapper"]
    pool_name = "fcci--vg-$unique_id"
    base_image_size = "10GB"
    root_path = "$devmapper_path"
[debug]
  level = "debug"
EOF

cat << EOF > $runtime_config_path
{
	"cpu_template": "T2",
	"debug": true,
	"firecracker_binary_path": "$bin_path/firecracker",
	"shim_base_dir": "$dir",
	"kernel_image_path": "$dir/default-vmlinux.bin",
	"kernel_args": "ro console=ttyS0 noapic reboot=k panic=1 pci=off nomodules systemd.unified_cgroup_hierarchy=0 systemd.journald.forward_to_console systemd.log_color=false systemd.unit=firecracker.target init=/sbin/overlay-init",
	"log_levels": ["debug"],
	"root_drive": "$dir/default-rootfs.img",
	"jailer": {
		"runc_binary_path": "$bin_path/runc",
		"runc_config_path": "$dir/config.json"
	}
}
EOF

cp ./runtime/firecracker-runc-config.json.example $dir/config.json

# runc uses /run/runc directory by default. Since our Amazon Linux 2 tests on
# BuildKite don't use Docker, sharing the directory across multiple test
# runs causes a race condition.
#
# This wrapper script gives runc /run/runc-$unique_id instead to avoid
# the race condition.
cp ./_submodules/runc/runc "$bin_path/runc.real"
cat > "$bin_path/runc" <<EOF
#! /bin/bash
set -euo pipefail
"$bin_path/runc.real" --root "/run/runc-$unique_id" "\$@"
EOF
chmod +x "$bin_path/runc"
