# Quickstart with firecracker-containerd

This quickstart guide provides simple steps to get a working
firecracker-containerd environment, with each of the major components built from
source.  Once you have completed this quickstart, you should be able to run and
develop firecracker-containerd (the components in this repository), the
Firecracker VMM, and containerd.

This quickstart will clone repositories under your `$HOME` directory and install
files into `/usr/local/bin`.

1. Get an AWS account (see
   [this article](https://aws.amazon.com/premiumsupport/knowledge-center/create-and-activate-aws-account/)
   if you need help creating one)
2. Launch an i3.metal instance running Debian Stretch (you can find it in the
   [AWS marketplace](http://deb.li/awsmp) or on [this
   page](https://wiki.debian.org/Cloud/AmazonEC2Image/Stretch).  If you need
   help launching an EC2 instance, see the
   [EC2 getting started guide](https://docs.aws.amazon.com/AWSEC2/latest/UserGuide/EC2_GetStarted.html).
3. Run the script below to download and install all the required components.
   This script expects to be run from your `$HOME` directory.

```bash
#!/bin/bash

cd ~

# Install git, Go 1.11, make, curl
sudo mkdir -p /etc/apt/sources.list.d
echo "deb http://ftp.debian.org/debian stretch-backports main" | \
     sudo tee /etc/apt/sources.list.d/stretch-backports.list
sudo DEBIAN_FRONTEND=noninteractive apt-get update
sudo DEBIAN_FRONTEND=noninteractive apt-get \
  --target-release stretch-backports \
  install --yes \
  golang-go \
  make \
  git \
  curl \
  e2fsprogs \
  musl-tools \
  util-linux

# Install Rust
curl https://sh.rustup.rs -sSf | sh -s -- --verbose -y --default-toolchain 1.32.0
source $HOME/.cargo/env
rustup target add x86_64-unknown-linux-musl

# Check out Firecracker and build it from the v0.15.2 tag
git clone https://github.com/firecracker-microvm/firecracker.git
cd firecracker
git checkout v0.15.2
cargo build --release --features vsock --target x86_64-unknown-linux-musl
sudo cp target/x86_64-unknown-linux-musl/release/{firecracker,jailer} /usr/local/bin

cd ~

# Check out containerd and build it from the v1.2.4 tag
mkdir -p ~/go/src/github.com/containerd/containerd
git clone https://github.com/containerd/containerd.git ~/go/src/github.com/containerd/containerd
cd ~/go/src/github.com/containerd/containerd
git checkout v1.2.4
sudo DEBIAN_FRONTEND=noninteractive apt-get install -y libseccomp-dev btrfs-progs
make
sudo cp bin/* /usr/local/bin

cd ~

# Check out runc and build it from the 6635b4f0c6af3810594d2770f662f34ddc15b40d
# commit.  Note that this is the version described in
# https://github.com/containerd/containerd/blob/v1.2.4/RUNC.md and
# https://github.com/containerd/containerd/blob/v1.2.4/vendor.conf#L23
mkdir -p ~/go/src/github.com/opencontainers/runc
git clone https://github.com/opencontainers/runc ~/go/src/github.com/opencontainers/runc
cd ~/go/src/github.com/opencontainers/runc
git checkout 6635b4f0c6af3810594d2770f662f34ddc15b40d
make static BUILDTAGS='seccomp'
sudo make BINDIR='/usr/local/bin' install

cd ~

# Check out firecracker-containerd and build it
git clone https://github.com/firecracker-microvm/firecracker-containerd.git
cd firecracker-containerd
sudo DEBIAN_FRONTEND=noninteractive apt-get install -y dmsetup
make STATIC_AGENT='true'
sudo cp runtime/containerd-shim-aws-firecracker snapshotter/cmd/{devmapper/devmapper_snapshotter,naive/naive_snapshotter} /usr/local/bin

cd ~

# Download kernel and generic VM image
curl -fsSL -o hello-vmlinux.bin https://s3.amazonaws.com/spec.ccfc.min/img/hello/kernel/hello-vmlinux.bin
curl -fsSL -o hello-rootfs.ext4 https://s3.amazonaws.com/spec.ccfc.min/img/hello/fsfiles/hello-rootfs.ext4

# Inject the agent, runc, and a startup script into the VM image
mkdir /tmp/mnt
# Construct fc-agent.start
cat >fc-agent.start <<EOF
#!/bin/sh
mkdir -p /container
exec > /container/agent-debug.log # Debug logs from the agent
exec 2>&1
touch /container/runtime
mkdir /container/rootfs
mount -t auto -o rw /dev/vdb /container/rootfs
cd /container
/usr/local/bin/agent -id 1 -debug &
EOF
chmod +x fc-agent.start
truncate --size=+50M hello-rootfs.ext4
/sbin/e2fsck -f hello-rootfs.ext4
/sbin/resize2fs hello-rootfs.ext4
sudo mount hello-rootfs.ext4 /tmp/mnt
sudo cp $(which runc) firecracker-containerd/agent/agent /tmp/mnt/usr/local/bin
sudo cp fc-agent.start /tmp/mnt/etc/local.d
sudo ln -s /etc/init.d/local /tmp/mnt/etc/runlevels/default/local
sudo ln -s /etc/init.d/cgroups /tmp/mnt/etc/runlevels/default/cgroups
sudo umount /tmp/mnt
rmdir /tmp/mnt

cd ~

# Configure containerd to use our new snapshotter
sudo mkdir -p /etc/containerd
sudo tee -a /etc/containerd/config.toml <<EOF
[proxy_plugins]
  [proxy_plugins.firecracker-naive]
    type = "snapshot"
    address = "/var/run/firecracker-containerd/naive-snapshotter.sock"
EOF

cd ~

# Configure the aws.firecracker runtime
sudo mkdir -p /var/lib/firecracker-containerd/runtime
sudo cp hello-rootfs.ext4 hello-vmlinux.bin /var/lib/firecracker-containerd/runtime
sudo mkdir -p /etc/containerd
sudo tee -a /etc/containerd/firecracker-runtime.json <<EOF
{
  "firecracker_binary_path": "/usr/local/bin/firecracker",
  "kernel_image_path": "/var/lib/firecracker-containerd/runtime/hello-vmlinux.bin",
  "root_drive": "/var/lib/firecracker-containerd/runtime/hello-rootfs.ext4",
  "cpu_count": 1,
  "cpu_template": "T2",
  "console": "stdio",
  "log_fifo": "/tmp/fc-logs.fifo",
  "log_level": "Debug",
  "metrics_fifo": "/tmp/fc-metrics.fifo"
}
EOF

# Enable vhost-vsock
sudo modprobe vhost-vsock
```

4. Open a new terminal and start the `naive_snapshotter` program in the
   foreground

```bash
sudo mkdir -p /var/run/firecracker-containerd /var/lib/firecracker-containerd/naive
sudo naive_snapshotter \
     -address /var/run/firecracker-containerd/naive-snapshotter.sock \
     -path /var/lib/firecracker-containerd/naive \
     -debug
```

5. Open a new terminal and start `containerd` in the foreground

```bash
sudo containerd
```

6. Open a new terminal, pull an image, and run a container!

```bash
sudo ctr image pull \
     --snapshotter firecracker-naive \
     docker.io/library/debian:latest
sudo ctr run \
     --snapshotter firecracker-naive \
     --runtime aws.firecracker \
     --tty \
     docker.io/library/debian:latest \
     test
```

When you're done, you can stop or terminate your i3.metal EC2 instance to avoid
incurring additional charges from EC2.
