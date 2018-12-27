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
2. Launch an i3.metal instance running Amazon Linux 2 (you can find it in the
   EC2 console Quickstart wizard, or by running
   `aws ssm get-parameters --names /aws/service/ami-amazon-linux-latest/amzn2-ami-hvm-x86_64-gp2`
   in your chosen region).  If you need help launching an EC2 instance, see the
   [EC2 getting started guide](https://docs.aws.amazon.com/AWSEC2/latest/UserGuide/EC2_GetStarted.html).
3. If you have an older kernel, update the kernel to
   `kernel-4.14.88-88.76.amzn2` (there's a bugfix in this that we need) and
   reboot.  The latest AMIs for Amazon Linux 2 (which you can discover from the
   instructions above) already have a new-enough kernel.
   <details><summary>Click here for instructions on updating your kernel</summary>
   ```bash
   if [[ $(rpm --eval "%{lua: print(rpm.vercmp('$(uname -r)', '4.14.88-88.76.amzn2.x86_64'))}") -lt 0 ]]; then
     echo "You need to install a kernel >= 4.14.88-88.76.amzn2.  You can do so by running the following commands:"
     echo "sudo yum -y upgrade kernel && sudo reboot"
   else
     echo 'You are already up to date!'
   fi
   ```
   </details>
3. Run the script below to download and install all the required components.
   This script expects to be run from your `$HOME` directory.

```bash
#!/bin/bash

if [[ $(rpm --eval "%{lua: print(rpm.vercmp('$(uname -r)', '4.14.88-88.76.amzn2.x86_64'))}") -lt 0 ]]; then
  echo "You need to install a kernel >= 4.14.88-88.76.amzn2.  You can do so by running the following commands:"
  echo "sudo yum -y upgrade kernel && sudo reboot"
fi

cd ~

# Install git
sudo yum install -y git

# Install Rust and Go 1.11
sudo amazon-linux-extras install -y rust1
sudo amazon-linux-extras install -y golang1.11

# Check out Firecracker and build it from the v0.12.0 tag
git clone https://github.com/firecracker-microvm/firecracker.git
cd firecracker
git checkout v0.12.0
cargo build --release --features vsock --target x86_64-unknown-linux-gnu
sudo cp target/x86_64-unknown-linux-gnu/release/{firecracker,jailer} /usr/local/bin

cd ~

# Check out containerd and build it from the v1.2.1 tag
mkdir -p ~/go/src/github.com/containerd/containerd
git clone https://github.com/containerd/containerd.git ~/go/src/github.com/containerd/containerd
cd ~/go/src/github.com/containerd/containerd
git checkout v1.2.1
sudo yum install -y libseccomp-devel btrfs-progs-devel
make
sudo cp bin/* /usr/local/bin

cd ~

# Check out runc and build it from the 96ec2177ae841256168fcf76954f7177af9446eb
# commit.  Note that this is the version described in
# https://github.com/containerd/containerd/blob/v1.2.1/RUNC.md and
# https://github.com/containerd/containerd/blob/v1.2.1/vendor.conf#L23
mkdir -p ~/go/src/github.com/opencontainers/runc
git clone https://github.com/opencontainers/runc ~/go/src/github.com/opencontainers/runc
cd ~/go/src/github.com/opencontainers/runc
git checkout 96ec2177ae841256168fcf76954f7177af9446eb
sudo yum install -y libseccomp-static glibc-static
make static BUILDTAGS='seccomp'
sudo make BINDIR='/usr/local/bin' install

cd ~

# Check out firecracker-containerd and build it
git clone https://github.com/firecracker-microvm/firecracker-containerd.git
cd firecracker-containerd
sudo yum install -y device-mapper
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
e2fsck -f hello-rootfs.ext4
resize2fs hello-rootfs.ext4
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
  "socket_path": "./firecracker.sock",
  "kernel_image_path": "/var/lib/firecracker-containerd/runtime/hello-vmlinux.bin",
  "kernel_args": "console=ttyS0 noapic reboot=k panic=1 pci=off nomodules rw",
  "root_drive": "/var/lib/firecracker-containerd/runtime/hello-rootfs.ext4",
  "cpu_count": 1,
  "cpu_template": "T2",
  "console": "stdio",
  "log_fifo": "/tmp/fc-logs.fifo",
  "log_level": "Debug",
  "metrics_fifo": "/tmp/fc-metrics.fifo"
}
EOF
```

4. Open a new terminal and start the `naive_snapshotter` program in the
   foreground

```bash
sudo mkdir -p /var/run/firecracker-containerd /var/lib/firecracker-containerd/naive
sudo /usr/local/bin/naive_snapshotter \
     -address /var/run/firecracker-containerd/naive-snapshotter.sock \
     -path /var/lib/firecracker-containerd/naive \
     -debug
```

5. Open a new terminal and start `containerd` in the foreground

```bash
sudo PATH=$PATH /usr/local/bin/containerd
```

6. Open a new terminal, pull an image, and run a container!

```bash
sudo /usr/local/bin/ctr image pull \
     --snapshotter firecracker-naive \
     docker.io/library/debian:latest
sudo /usr/local/bin/ctr run \
     --snapshotter firecracker-naive \
     --runtime aws.firecracker \
     --tty \
     docker.io/library/debian:latest \
     test
```

When you're done, you can stop or terminate your i3.metal EC2 instance to avoid
incurring additional charges from EC2.