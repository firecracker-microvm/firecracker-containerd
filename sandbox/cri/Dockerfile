FROM rust:1.32 as base-build

ENV TMP_BUILD_DIR=/tmp/build
ENV CARGO_HOME=/usr/local/rust
ENV RUSTUP_HOME=/usr/local/rust
ENV RUST_VERSION=1.32.0
ENV PATH="$CARGO_HOME/bin:$PATH"

ARG FIRECRACKER_VERSION="0.15.0"

# Install system dependecies
#
RUN apt-get update \
    && apt-get -y install --no-install-recommends \
        curl \
        make \
        musl-tools \
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
    && curl https://sh.rustup.rs -sSf | sh -s -- --verbose -y --default-toolchain "$RUST_VERSION" \
    && rustup target add x86_64-unknown-linux-musl \
    && git clone https://github.com/firecracker-microvm/firecracker.git

WORKDIR firecracker
RUN git checkout tags/v"$FIRECRACKER_VERSION" \
    && cargo build --release --features vsock --target-dir "$TMP_BUILD_DIR" \
    && cp "$TMP_BUILD_DIR"/x86_64-unknown-linux-musl/release/firecracker /usr/local/bin \
    && cp "$TMP_BUILD_DIR"/x86_64-unknown-linux-musl/release/jailer /usr/local/bin \
    && mkdir /etc/containerd && touch /etc/containerd/config.toml \
    && mkdir -p /go/src/github.com/containerd/containerd

from golang:1.12 as cri-build

RUN apt-get update \
    && apt-get -y install --no-install-recommends \
        cmake \
        curl \
        make \
        musl-tools \
        libseccomp-dev \
        btrfs-tools \
        socat \
        sudo \
        iptables \
    && rm -rf /var/lib/apt/lists/*

# Install CRI
WORKDIR /
# CRI does not have support for Go modules yet.
ENV GOPATH="/go"
RUN go get github.com/containerd/cri \
    && go get -u github.com/onsi/ginkgo/ginkgo

WORKDIR /go/src/github.com/containerd/cri
RUN git checkout tags/v1.11.1 \
    && make install.deps

from cri-build as firecracker-containerd-build
ENV PATH="/go/bin:$PATH"

# Build and install firecracker-containerd
WORKDIR /firecracker-containerd/
COPY --from=base-build /usr/local/bin/firecracker /usr/local/bin/
COPY --from=base-build /usr/local/bin/jailer /usr/local/bin/
COPY --from=base-build /etc/containerd /etc/

RUN mkdir -p /var/lib/firecracker-containerd/runtime \
    && curl -fsSL -o /var/lib/firecracker-containerd/runtime/hello-vmlinux.bin https://s3.amazonaws.com/spec.ccfc.min/img/hello/kernel/hello-vmlinux.bin \
    && curl -fsSL -o /var/lib/firecracker-containerd/runtime/hello-rootfs.ext4 https://s3.amazonaws.com/spec.ccfc.min/img/hello/fsfiles/hello-rootfs.ext4 \
    && e2fsck -f -y /var/lib/firecracker-containerd/runtime/hello-rootfs.ext4 \
    && resize2fs /var/lib/firecracker-containerd/runtime/hello-rootfs.ext4 70m \
    && mkdir /var/run/firecracker-containerd

COPY . /firecracker-containerd/
COPY sandbox/cri/config.toml /etc/containerd/config.toml
COPY sandbox/cri/cri.sh /usr/local/bin
COPY sandbox/cri/firecracker-runtime.json /etc/containerd/

RUN make all \
    && make install

from firecracker-containerd-build as cri-test

WORKDIR /go/src/github.com/containerd/cri
ENTRYPOINT cri.sh
