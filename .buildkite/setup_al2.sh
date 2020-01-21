#!/bin/bash

source ./.buildkite/al2env.sh

echo "Creating necessary directories"

mkdir -p $dir
mkdir -p $bin_path
mkdir -p $devmapper_path
mkdir -p $state_path

echo "Resetting thinpool $uuid"
./tools/thinpool.sh reset $uuid

export INSTALLROOT=$dir
export FIRECRACKER_CONTAINERD_RUNTIME_DIR=$dir
echo "Running make"
sudo make
echo "Running make install"
sudo -E "INSTALLROOT=$INSTALLROOT" make install
echo "Running make install-default-vmlinux"
sudo make install-default-vmlinux
echo "Running make image"
sudo make image
echo "Running make install-default-rootfs"
sudo make install-default-rootfs

echo "Creating $dir/config.toml"
cat << EOF > $dir/config.toml
disabled_plugins = ["cri"]
root = "$dir"
state = "$state_path"
[grpc]
  address = "$dir/containerd.sock"
[plugins]
  [plugins.devmapper]
    pool_name = "fcci--vg-$uuid"
    base_image_size = "10GB"
    root_path = "$devmapper_path"
[debug]
  level = "debug"
EOF

echo "Creating $runtime_config_path"
cat << EOF > $runtime_config_path
{
	"cpu_template": "T2",
	"debug": true,
	"firecracker_binary_path": "/usr/local/bin/$firecracker_bin",
	"shim_base_dir": "$dir",
	"kernel_image_path": "$dir/default-vmlinux.bin",
	"kernel_args": "ro console=ttyS0 noapic reboot=k panic=1 pci=off nomodules systemd.journald.forward_to_console systemd.log_color=false systemd.unit=firecracker.target init=/sbin/overlay-init",
	"log_level": "DEBUG",
	"root_drive": "$dir/default-rootfs.img",
	"jailer": {
		"runc_binary_path": "$bin_path/runc",
		"runc_config_path": "$dir/config.json"
	}
}
EOF

echo "Copying firecracker-runc-config.json"
cp ./runtime/firecracker-runc-config.json.example $dir/config.json
echo "Copying runc"
cp ./_submodules/runc/runc $bin_path/runc
