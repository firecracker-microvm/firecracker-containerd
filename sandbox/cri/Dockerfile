FROM golang:1.11

ADD . /firecracker-containerd/

ARG TMP_BUILD_DIR=/tmp/build
ARG FIRECRACKER_SRC_DIR="/firecracker"
ARG FIRECRACKER_BUILD_DIR="$FIRECRACKER_SRC_DIR/build"
ARG CARGO_REGISTRY_DIR="$FIRECRACKER_BUILD_DIR/cargo_registry"
ARG FIRECRACKER_CONTAINERD_SRC_DIR="/firecracker-containerd"
ARG FIRECRACKER_VERSION="0.15.0"

ENV CARGO_HOME=/usr/local/rust
ENV RUSTUP_HOME=/usr/local/rust
ENV PATH="/go/bin:$CARGO_HOME/bin:$PATH"
ENV CARGO_TARGET_DIR="$FIRECRACKER_BUILD_DIR/cargo_target"

# Install system dependecies
#
RUN apt-get update \
    && apt-get -y install --no-install-recommends \
        binutils-dev \
        cmake \
        curl \
        file \
        g++ \
        gcc \
        git \
        iperf3 \
        iproute2 \
        jq \
        libcurl4-openssl-dev \
        libdw-dev \
        libiberty-dev \
        libssl-dev \
        lsof \
        make \
        musl-tools \
        net-tools \
        openssh-client \
        pkgconf \
        zlib1g-dev \
        libseccomp-dev \
        btrfs-tools \
        socat \
	sudo \
	iptables \
    && rm -rf /var/lib/apt/lists/*

# Install the Rust toolchain
#
# Install 1.32.0 due to Firecracker crashing with 148 code
RUN mkdir "$TMP_BUILD_DIR" \
    && curl https://sh.rustup.rs -sSf | sh -s -- -y \
	&& rustup install 1.32.0 \
	&& rustup override set 1.32.0 \
        && rustup target add x86_64-unknown-linux-musl \
    && cd /

# Install Firecracker and set binaries to TMP_BUILD_DIR
RUN git clone https://github.com/firecracker-microvm/firecracker.git
WORKDIR firecracker
RUN git checkout tags/v"$FIRECRACKER_VERSION"
RUN cargo build --release --features vsock --target-dir "$TMP_BUILD_DIR"
RUN cp "$TMP_BUILD_DIR"/x86_64-unknown-linux-musl/release/firecracker /usr/local/bin
RUN cp "$TMP_BUILD_DIR"/x86_64-unknown-linux-musl/release/jailer /usr/local/bin
RUN mkdir /etc/containerd && touch /etc/containerd/config.toml

# Install containerd
RUN mkdir -p /go/src/github.com/containerd/containerd
RUN git clone https://github.com/containerd/containerd.git /go/src/github.com/containerd/containerd
WORKDIR /go/src/github.com/containerd/containerd
RUN git checkout v1.2.1
RUN make
RUN cp bin/* /usr/local/bin

# Install runc
RUN mkdir -p /go/src/github.com/opencontainers/runc
RUN git clone https://github.com/opencontainers/runc /go/src/github.com/opencontainers/runc
WORKDIR /go/src/github.com/opencontainers/runc
RUN git checkout v1.0.0-rc6
RUN make static BUILDTAGS='seccomp'
RUN make BINDIR='/usr/local/bin' install

# Build and install firecracker-containerd
WORKDIR /firecracker-containerd/
COPY sandbox/cri/config.toml /etc/containerd/config.toml
COPY sandbox/cri/cri.sh /usr/local/bin
COPY sandbox/cri/firecracker-runtime.json /etc/containerd/
RUN mkdir -p /var/lib/firecracker-containerd/runtime
# Retrieve kernel image and root drive
RUN curl -fsSL -o /var/lib/firecracker-containerd/runtime/hello-vmlinux.bin https://s3.amazonaws.com/spec.ccfc.min/img/hello/kernel/hello-vmlinux.bin
RUN curl -fsSL -o /var/lib/firecracker-containerd/runtime/hello-rootfs.ext4 https://s3.amazonaws.com/spec.ccfc.min/img/hello/fsfiles/hello-rootfs.ext4
# Resize rootfs
RUN e2fsck -f -y /var/lib/firecracker-containerd/runtime/hello-rootfs.ext4
RUN resize2fs /var/lib/firecracker-containerd/runtime/hello-rootfs.ext4 1g
RUN make all && make install
RUN mkdir /var/run/firecracker-containerd

# Install CRI
WORKDIR /
# CRI does not have support for Go modules yet.
ENV GOPATH="/go"
RUN go get github.com/containerd/cri
RUN go get -u github.com/onsi/ginkgo/ginkgo
WORKDIR /go/src/github.com/containerd/cri
RUN git checkout tags/v1.11.1
RUN make install.deps
CMD ["cri.sh"]
