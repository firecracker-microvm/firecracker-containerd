# Quickstart with firecracker-containerd

This quickstart guide provides simple steps to get a working
firecracker-containerd environment, with each of the major components built from
source.  Once you have completed this quickstart, you should be able to run and
develop firecracker-containerd (the components in this repository), the
Firecracker VMM, and containerd. Note that the guide below should result in VMs
by default having network access to IPs assigned on the host and may, depending
on the configuration of your host's network, also have outbound access to the
internet.

This quickstart will clone repositories under your `$HOME` directory and install
files into `/usr/local/bin`.

1. Get an AWS account (see
   [this article](https://aws.amazon.com/premiumsupport/knowledge-center/create-and-activate-aws-account/)
   if you need help creating one)
2. Launch an i3.metal instance running Debian Buster (you can find it in the
   [AWS marketplace](https://aws.amazon.com/marketplace/pp/B0859NK4HC) or
   on [this page](https://wiki.debian.org/Cloud/AmazonEC2Image/Buster).
   If you need help launching an EC2 instance, see the
   [EC2 getting started guide](https://docs.aws.amazon.com/AWSEC2/latest/UserGuide/EC2_GetStarted.html).
3. Run the script below to download and install all the required dependencies on a debian based instance.
   Alternate steps for rpm based instance is provided as well. This script expects to be run from your `$HOME` directory.

```bash
#!/bin/bash

cd ~

# Install git, Go 1.16, make, curl
sudo mkdir -p /etc/apt/sources.list.d
echo "deb http://ftp.debian.org/debian buster-backports main" | \
  sudo tee /etc/apt/sources.list.d/buster-backports.list
sudo DEBIAN_FRONTEND=noninteractive apt-get update
sudo DEBIAN_FRONTEND=noninteractive apt-get \
  install --yes \
  golang-1.16 \
  make \
  git \
  curl \
  e2fsprogs \
  util-linux \
  bc \
  gnupg

# Debian's Go 1.16 package installs "go" command under /usr/lib/go-1.16/bin
export PATH=/usr/lib/go-1.16/bin:$PATH

cd ~

# Install Docker CE
# Docker CE includes containerd, but we need a separate containerd binary, built
# in a later step
curl -fsSL https://download.docker.com/linux/debian/gpg | sudo apt-key add -
apt-key finger docker@docker.com | grep '9DC8 5822 9FC7 DD38 854A  E2D8 8D81 803C 0EBF CD88' || echo '**Cannot find Docker key**'
echo "deb [arch=amd64] https://download.docker.com/linux/debian $(lsb_release -cs) stable" | \
     sudo tee /etc/apt/sources.list.d/docker.list
sudo DEBIAN_FRONTEND=noninteractive apt-get update
sudo DEBIAN_FRONTEND=noninteractive apt-get \
     install --yes \
     docker-ce aufs-tools-
sudo usermod -aG docker $(whoami)

# Install device-mapper
sudo DEBIAN_FRONTEND=noninteractive apt-get install -y dmsetup
```

A similar script to install dependencies for rpm based linux distro e.g. Amazon Linux 2 can be found here

<details>

```bash
#!/bin/bash

cd ~

# Install git, make, curl
sudo yum -y update
sudo yum -y install \
  make \
  git \
  curl \
  e2fsprogs \
  util-linux \
  bc \
  gnupg


# Amazon Linux 2 packages can sometimes be dated, so let's install using
# the Go installer. The installer will handle any path changes and just
# need to source environment variables afterwards for the existing shell session.
curl -LO https://get.golang.org/$(uname)/go_installer && \
  chmod +x go_installer && \
  ./go_installer -version 1.16 && \
  rm go_installer && \
  source .bash_profile

cd ~

# Install Docker CE
# Docker CE includes containerd, but we need a separate containerd binary, built
# in a later step
sudo yum -y update
sudo amazon-linux-extras install -y  docker
sudo usermod -aG docker $(whoami)

sudo yum -y install device-mapper
```
</details>

4. Now run the following script below to download and install firecracker containerd.

```bash
#!/bin/bash
cd ~

# Check out firecracker-containerd and build it.  This includes:
# * firecracker-containerd runtime, a containerd v2 runtime
# * firecracker-containerd agent, an inside-VM component
# * runc, to run containers inside the VM
# * a Debian-based root filesystem configured as read-only with a read-write
#   overlay
# * firecracker-containerd, an alternative containerd binary that includes the
#   firecracker VM lifecycle plugin and API
# * tc-redirect-tap and other CNI dependencies that enable VMs to start with
#   access to networks available on the host
git clone https://github.com/firecracker-microvm/firecracker-containerd.git
cd firecracker-containerd
sg docker -c 'make all image firecracker'
sudo make install install-firecracker demo-network

cd ~

# Download kernel
curl -fsSL -o hello-vmlinux.bin https://s3.amazonaws.com/spec.ccfc.min/img/quickstart_guide/x86_64/kernels/vmlinux.bin

# Configure our firecracker-containerd binary to use our new snapshotter and
# separate storage from the default containerd binary
sudo mkdir -p /etc/firecracker-containerd
sudo mkdir -p /var/lib/firecracker-containerd/containerd
# Create the shim base directory for which firecracker-containerd will run the
# shim from
sudo mkdir -p /var/lib/firecracker-containerd
sudo tee /etc/firecracker-containerd/config.toml <<EOF
version = 2
disabled_plugins = ["io.containerd.grpc.v1.cri"]
root = "/var/lib/firecracker-containerd/containerd"
state = "/run/firecracker-containerd"
[grpc]
  address = "/run/firecracker-containerd/containerd.sock"
[plugins]
  [plugins."io.containerd.snapshotter.v1.devmapper"]
    pool_name = "fc-dev-thinpool"
    base_image_size = "10GB"
    root_path = "/var/lib/firecracker-containerd/snapshotter/devmapper"

[debug]
  level = "debug"
EOF

# Setup device mapper thin pool
sudo mkdir -p /var/lib/firecracker-containerd/snapshotter/devmapper
cd /var/lib/firecracker-containerd/snapshotter/devmapper
DIR=/var/lib/firecracker-containerd/snapshotter/devmapper
POOL=fc-dev-thinpool

if [[ ! -f "${DIR}/data" ]]; then
    sudo touch "${DIR}/data"
    sudo truncate -s 100G "${DIR}/data"
fi

if [[ ! -f "${DIR}/metadata" ]]; then
    sudo touch "${DIR}/metadata"
    sudo truncate -s 2G "${DIR}/metadata"
fi

DATADEV="$(sudo losetup --output NAME --noheadings --associated ${DIR}/data)"
if [[ -z "${DATADEV}" ]]; then
    DATADEV="$(sudo losetup --find --show ${DIR}/data)"
fi

METADEV="$(sudo losetup --output NAME --noheadings --associated ${DIR}/metadata)"
if [[ -z "${METADEV}" ]]; then
    METADEV="$(sudo losetup --find --show ${DIR}/metadata)"
fi

SECTORSIZE=512
DATASIZE="$(sudo blockdev --getsize64 -q ${DATADEV})"
LENGTH_SECTORS=$(bc <<< "${DATASIZE}/${SECTORSIZE}")
DATA_BLOCK_SIZE=128
LOW_WATER_MARK=32768
THINP_TABLE="0 ${LENGTH_SECTORS} thin-pool ${METADEV} ${DATADEV} ${DATA_BLOCK_SIZE} ${LOW_WATER_MARK} 1 skip_block_zeroing"
echo "${THINP_TABLE}"

if ! $(sudo dmsetup reload "${POOL}" --table "${THINP_TABLE}"); then
    sudo dmsetup create "${POOL}" --table "${THINP_TABLE}"
fi

cd ~

# Configure the aws.firecracker runtime
# The long kernel command-line configures systemd inside the Debian-based image
# and uses a special init process to create a read-write overlay on top of the
# read-only image.
sudo mkdir -p /var/lib/firecracker-containerd/runtime
sudo cp ~/firecracker-containerd/tools/image-builder/rootfs.img /var/lib/firecracker-containerd/runtime/default-rootfs.img
sudo cp ~/hello-vmlinux.bin /var/lib/firecracker-containerd/runtime/default-vmlinux.bin
sudo mkdir -p /etc/containerd
sudo tee /etc/containerd/firecracker-runtime.json <<EOF
{
  "firecracker_binary_path": "/usr/local/bin/firecracker",
  "cpu_template": "T2",
  "log_fifo": "fc-logs.fifo",
  "log_levels": ["debug"],
  "metrics_fifo": "fc-metrics.fifo",
  "kernel_args": "console=ttyS0 noapic reboot=k panic=1 pci=off nomodules ro systemd.unified_cgroup_hierarchy=0 systemd.journald.forward_to_console systemd.unit=firecracker.target init=/sbin/overlay-init",
  "default_network_interfaces": [{
    "CNIConfig": {
      "NetworkName": "fcnet",
      "InterfaceName": "veth0"
    }
  }]
}
EOF
```

5. Open a new terminal and start `firecracker-containerd` in the foreground

```bash
sudo firecracker-containerd --config /etc/firecracker-containerd/config.toml
```

6. Open a new terminal, pull an image, and run a container!

```bash
sudo firecracker-ctr --address /run/firecracker-containerd/containerd.sock \
     image pull \
     --snapshotter devmapper \
     docker.io/library/debian:latest
sudo firecracker-ctr --address /run/firecracker-containerd/containerd.sock \
     run \
     --snapshotter devmapper \
     --runtime aws.firecracker \
     --rm --tty --net-host \
     docker.io/library/debian:latest \
     test
```

In the commands above, note the `--address` argument targeting the
`firecracker-containerd` binary instead of the normal `containerd` binary, the
`--snapshotter` argument targeting the block-device snapshotter, and the
`--runtime` argument targeting the firecracker-containerd runtime.

When you're done, you can stop or terminate your i3.metal EC2 instance to avoid
incurring additional charges from EC2.
