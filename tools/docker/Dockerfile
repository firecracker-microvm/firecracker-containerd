# syntax=docker/dockerfile:experimental
# Copyright 2018-2019 Amazon.com, Inc. or its affiliates. All Rights Reserved.
#
# Licensed under the Apache License, Version 2.0 (the "License"). You may
# not use this file except in compliance with the License. A copy of the
# License is located at
#
# 	http://aws.amazon.com/apache2.0/
#
# or in the "license" file accompanying this file. This file is distributed
# on an "AS IS" BASIS, WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either
# express or implied. See the License for the specific language governing
# permissions and limitations under the License.

#########################################
#
# COMMON IMAGES
#
#########################################
FROM debian:stretch as base
# Set up a non-root user for running builds and some tests in later stages
# Buildkit caches don't support anything like a "--chown" flag yet, so we need to ensure builder will have access to them
RUN useradd --create-home --uid 1001 builder \
	&& mkdir /output \
	&& chown builder /output \
	&& mkdir -p /home/builder/go/pkg/mod/cache \
	&& mkdir -p /home/builder/cargo/registry \
	&& chown -R builder /home/builder/



#########################################
#
# BUILD IMAGES
#
#########################################



# Common tools needed for the build stages ahead. The final test images do not inherit directly from here, so this bloat
# is dropped in those final end-use images.
FROM base as build-base
ENV PATH="/bin:/usr/bin:/usr/local/bin:/sbin:/usr/sbin:/usr/local/sbin:/usr/lib/go/bin" \
	DEBIAN_FRONTEND="noninteractive" \
	GO111MODULE="on"
RUN mkdir -p /etc/apt/sources.list.d \
	&& echo "deb http://ftp.debian.org/debian stretch-backports main" > /etc/apt/sources.list.d/stretch-backports.list \
	&& apt-get update \
	&& apt-get --target-release stretch-backports install --yes --no-install-recommends \
		golang-go \
	&& apt-get install --yes --no-install-recommends \
		build-essential \
		ca-certificates \
		curl \
		git \
		libdevmapper-dev \
		libseccomp-dev \
		musl-tools \
		pkg-config \
		util-linux

# Run as non-root now that the apt installs are out of the way
USER builder
WORKDIR /home/builder
SHELL ["/bin/bash", "-c"]




# Build firecracker itself
FROM build-base as firecracker-build
ENV RUSTUP_HOME="/home/builder/rustup" \
	CARGO_HOME="/home/builder/cargo" \
	PATH="/home/builder/cargo/bin:$PATH" \
	RUST_VERSION="1.32.0"

RUN curl --silent --show-error --retry 3 --max-time 30 --output rustup-init \
	"https://static.rust-lang.org/rustup/archive/1.16.0/x86_64-unknown-linux-gnu/rustup-init" \
	&& echo "2d4ddf4e53915a23dda722608ed24e5c3f29ea1688da55aa4e98765fc6223f71 rustup-init" | sha256sum -c - \
	&& chmod +x rustup-init \
	&& ./rustup-init -y --no-modify-path --default-toolchain $RUST_VERSION \
	&& source ${CARGO_HOME}/env \
	&& rustup target add x86_64-unknown-linux-musl

RUN --mount=type=cache,from=build-base,source=/home/builder/cargo/registry,target=/home/builder/cargo/registry \
	source ${CARGO_HOME}/env \
	&& git clone https://github.com/firecracker-microvm/firecracker.git \
	&& cd firecracker \
	&& git checkout v0.15.2 \
	&& cargo build --release --features vsock --target x86_64-unknown-linux-musl \
	&& cp target/x86_64-unknown-linux-musl/release/firecracker /output \
	&& cp target/x86_64-unknown-linux-musl/release/jailer /output




# All the build steps for Go code must first lock the go mod cache when downloading modules as Go 1.11 does not support
# concurrent access.
# TODO After upgrading to Go 1.12, we can safely concurrently access the cache and combine the download and build steps.




# Build firecracker-containerd
FROM build-base as firecracker-containerd-build
ENV STATIC_AGENT='true'
# Normally, it would be simplest to just "ADD --chown=builder" the firecracker-containerd source in, but that results in
# permission denied here because "ADD --chown" does not set owner recursively (so when "go build" tries to create
# binaries, it doesn't have write permission on all directories). Instead, we bind mount the firecracker-containerd src
# directory to a tmp location and copy to one we will actually use (giving ourselves permission to it in the process).
RUN --mount=type=cache,from=build-base,source=/home/builder/go/pkg/mod,target=/home/builder/go/pkg/mod,sharing=locked \
	--mount=type=bind,target=_firecracker-containerd \
	cp -R _firecracker-containerd firecracker-containerd \
	&& cd firecracker-containerd \
	&& go mod verify || go mod download
RUN --mount=type=cache,from=build-base,source=/home/builder/go/pkg/mod,target=/home/builder/go/pkg/mod \
	cd firecracker-containerd \
	&& make \
	&& cp \
		agent/agent \
		runtime/containerd-shim-aws-firecracker \
		snapshotter/cmd/devmapper/devmapper_snapshotter \
		snapshotter/cmd/naive/naive_snapshotter \
		/output
RUN --mount=type=cache,from=build-base,source=/home/builder/go/pkg/mod,target=/home/builder/go/pkg/mod \
  cd firecracker-containerd/firecracker-control/cmd/containerd/ \
  && make \
  && cp \
  firecracker-containerd \
  /output/containerd




#########################################
#
# VM IMAGES
#
#########################################



# Build a rootfs for the microVM, including runc and firecracker-containerd's agent
FROM alpine:3.8 as firecracker-vm-root
COPY _submodules/runc/runc /usr/local/bin
COPY --from=firecracker-containerd-build /output/agent /usr/local/bin/
ADD tools/docker/fc-agent.start /etc/local.d/fc-agent.start
RUN apk add openrc \
	&& ln -s /etc/init.d/local /etc/runlevels/default/local \
	&& ln -s /etc/init.d/cgroups /etc/runlevels/default/cgroups \
	&& ln -s /etc/init.d/devfs /etc/runlevels/boot/devfs \
	&& ln -s /etc/init.d/hostname /etc/runlevels/boot/hostname \
	&& ln -s /etc/init.d/procfs /etc/runlevels/boot/procfs \
	&& ln -s /etc/init.d/sysfs /etc/runlevels/boot/sysfs

# Convert the VM rootfs into an ext4 file. This step must run as root.
FROM debian:stretch as firecracker-vm-root-builder
COPY --from=firecracker-vm-root / /vm
RUN mkdir -p /output \
	&& cd /output \
	&& mkfs.ext4 -d /vm vm.ext4 65536




#########################################
#
# TEST IMAGES
#
#########################################



# Base image for running tests, including the ability to start firecracker, containerd, firecracker-containerd and our
# snapshotters.
# Derived images should include containerd/config.toml, other configuration needed to start a full
# firecracker-containerd stack and an entrypoint that starts containerd plus one of our snapshotters.
FROM base as firecracker-containerd-unittest
ENV PATH="/bin:/usr/bin:/usr/local/bin:/sbin:/usr/sbin:/usr/local/sbin:/usr/lib/go/bin" \
	DEBIAN_FRONTEND="noninteractive" \
	FICD_LOG_DIR="/var/log/firecracker-containerd-test"
ENV FICD_SNAPSHOTTER_OUTFILE="${FICD_LOG_DIR}/snapshotter.out" \
	FICD_CONTAINERD_OUTFILE="${FICD_LOG_DIR}/containerd.out"
RUN mkdir -p /etc/apt/sources.list.d \
	&& echo "deb http://ftp.debian.org/debian stretch-backports main" > /etc/apt/sources.list.d/stretch-backports.list \
	&& apt-get update \
	&& apt-get --target-release stretch-backports install --yes --no-install-recommends \
		golang-go \
	&& apt-get install --yes --no-install-recommends \
		build-essential \
		ca-certificates \
		curl \
		git \
		libdevmapper-dev \
		libseccomp-dev

COPY --from=firecracker-containerd-build /home/builder/firecracker-containerd /firecracker-containerd
COPY --from=firecracker-build /output/* /usr/local/bin/
COPY --from=firecracker-vm-root-builder /output/vm.ext4 /var/lib/firecracker-containerd/runtime/default-rootfs.img
COPY --from=firecracker-containerd-build /output/* /usr/local/bin/
COPY _submodules/runc/runc /usr/local/bin
COPY tools/docker/firecracker-runtime.json /etc/containerd/firecracker-runtime.json

RUN curl --silent --show-error --retry 3 --max-time 30 --output default-vmlinux.bin \
	"https://s3.amazonaws.com/spec.ccfc.min/img/hello/kernel/hello-vmlinux.bin" \
	&& echo "882fa465c43ab7d92e31bd4167da3ad6a82cb9230f9b0016176df597c6014cef default-vmlinux.bin" | sha256sum -c - \
	&& mv default-vmlinux.bin /var/lib/firecracker-containerd/runtime/default-vmlinux.bin

RUN --mount=type=cache,from=build-base,source=/home/builder/go/pkg/mod,target=/tmp/go/pkg/mod,readonly \
	mkdir -p /root/go/pkg/mod \
	&& cp -R /tmp/go/pkg/mod/* /root/go/pkg/mod \
	&& cp -R /tmp/go/pkg/mod/* /home/builder/go/pkg/mod \
	&& chown -R builder /home/builder/go/pkg/mod

RUN mkdir -p /var/run/firecracker-containerd \
	&& mkdir -p ${FICD_LOG_DIR}

ENTRYPOINT ["/bin/bash", "-c"]




# Test image for running unittests as a non-root user
FROM firecracker-containerd-unittest as firecracker-containerd-unittest-nonroot
USER builder
WORKDIR /home/builder
SHELL ["/bin/bash", "-c"]




# Test image that starts up containerd and the naive snapshotter. The default CMD will drop to a bash shell. Overrides
# to CMD will be provided appended to /bin/bash -c
FROM firecracker-containerd-unittest as firecracker-containerd-e2etest-naive
# Install tini init to handle zombies created by our double-forked shims.
# Unfortunately, we need to do this rather than only use docker's "--init" flag because docker
# currently puts its default init binary at /dev/init, but we need to bind-mount /dev from
# the host in order for the snapshotter to work.
# This should no longer be an issue after github.com/moby/moby #37665 is released.
RUN curl --silent --show-error --retry 3 --max-time 30 --location --output tini \
  "https://github.com/krallin/tini/releases/download/v0.18.0/tini" \
  && echo "12d20136605531b09a2c2dac02ccee85e1b874eb322ef6baf7561cd93f93c855 tini" | sha256sum -c - \
  && mv tini /sbin/tini \
  && chmod +x /sbin/tini

COPY tools/docker/naive-snapshotter/config.toml /etc/containerd/config.toml
COPY tools/docker/naive-snapshotter/entrypoint.sh /entrypoint
RUN mkdir -p /var/lib/firecracker-containerd/naive

ENTRYPOINT ["/sbin/tini", "--", "/entrypoint"]
CMD ["exec /bin/bash"]




# TODO Add a stage for the devmapper snapshotter implementation (as opposed to naive implementation)




# Debugging image that starts up containerd and the naive snapshotter and includes some additional basic debugging tools.
# TODO add firectl here
FROM firecracker-containerd-e2etest-naive as firecracker-containerd-dev
RUN apt-get update \
	&& apt-get install -y \
		strace \
		less \
		procps \
		util-linux
