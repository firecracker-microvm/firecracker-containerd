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
FROM golang:1.12-stretch as base
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
ENV PATH="/bin:/usr/bin:/usr/local/bin:/sbin:/usr/sbin:/usr/local/sbin:/usr/local/go/bin" \
	DEBIAN_FRONTEND="noninteractive" \
	GO111MODULE="on"
RUN apt-get update && apt-get install --yes --no-install-recommends \
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
ENV GOPATH="/home/builder/go"
SHELL ["/bin/bash", "-c"]




# Build firecracker-containerd
FROM build-base as firecracker-containerd-build
ENV STATIC_AGENT='true'
# Normally, it would be simplest to just "ADD --chown=builder" the firecracker-containerd source in, but that results in
# permission denied here because "ADD --chown" does not set owner recursively (so when "go build" tries to create
# binaries, it doesn't have write permission on all directories). Instead, we bind mount the firecracker-containerd src
# directory to a tmp location and copy to one we will actually use (giving ourselves permission to it in the process).
RUN --mount=type=bind,target=_firecracker-containerd cp -R _firecracker-containerd firecracker-containerd
RUN --mount=type=cache,from=build-base,source=/home/builder/go/pkg/mod,target=/home/builder/go/pkg/mod \
	cd firecracker-containerd && make


#########################################
#
# TEST IMAGES
#
#########################################



# Base image for running tests, including the ability to start firecracker, containerd, firecracker-containerd and our
# snapshotters.
# Derived images should include containerd/config.toml, other configuration needed to start a full
# firecracker-containerd stack and an entrypoint that starts containerd plus one of our snapshotters.
FROM base as firecracker-containerd-test

ENV PATH="/bin:/usr/bin:/usr/local/bin:/sbin:/usr/sbin:/usr/local/sbin:/usr/local/go/bin" \
	DEBIAN_FRONTEND="noninteractive" \
	FICD_LOG_DIR="/var/log/firecracker-containerd-test"
ENV FICD_SNAPSHOTTER="naive" \
	FICD_SNAPSHOTTER_OUTFILE="${FICD_LOG_DIR}/snapshotter.out" \
	FICD_CONTAINERD_OUTFILE="${FICD_LOG_DIR}/containerd.out"
RUN apt-get update && apt-get install --yes --no-install-recommends \
		build-essential \
		ca-certificates \
		curl \
		git \
		iptables \
		iperf3 \
		libdevmapper-dev \
		libseccomp-dev

RUN mkdir -p /var/lib/firecracker-containerd/runtime \
        && curl --silent --show-error --retry 3 --max-time 30 --output default-vmlinux.bin \
	"https://s3.amazonaws.com/spec.ccfc.min/img/hello/kernel/hello-vmlinux.bin" \
	&& echo "882fa465c43ab7d92e31bd4167da3ad6a82cb9230f9b0016176df597c6014cef default-vmlinux.bin" | sha256sum -c - \
	&& mv default-vmlinux.bin /var/lib/firecracker-containerd/runtime/default-vmlinux.bin

COPY --from=firecracker-containerd-build /home/builder/firecracker-containerd /firecracker-containerd
COPY tools/image-builder/rootfs.img /var/lib/firecracker-containerd/runtime/default-rootfs.img
COPY --from=firecracker-containerd-build /output/* /usr/local/bin/

RUN --mount=type=cache,from=build-base,source=/home/builder/go/pkg/mod,target=/tmp/go/pkg/mod,readonly \
	mkdir -p ${GOPATH}/pkg/mod \
	&& cp -R /tmp/go/pkg/mod/* ${GOPATH}/pkg/mod \
	&& cp -R /tmp/go/pkg/mod/* /home/builder/go/pkg/mod \
	&& chown -R builder /home/builder/go/pkg/mod

COPY --from=firecracker-containerd-build /home/builder/firecracker-containerd /firecracker-containerd
RUN make -C /firecracker-containerd install
RUN chmod a+r /firecracker-containerd/go.mod /firecracker-containerd/go.sum # fix for https://github.com/golang/go/issues/31871
RUN ln -s /usr/local/bin/firecracker-containerd /usr/local/bin/containerd
RUN ln -s /usr/local/bin/firecracker-ctr /usr/local/bin/ctr

RUN mkdir -p /var/run/firecracker-containerd \
	&& mkdir -p ${FICD_LOG_DIR}

# make sure all our dependencies, including test dependencies, are pulled into the
# image so we don't need internet access during the test runs themselves
RUN cd /firecracker-containerd && go get -d ./...

ENTRYPOINT ["/bin/bash", "-c"]






# Test image that starts up containerd and the naive snapshotter. The default CMD will drop to a bash shell. Overrides
# to CMD will be provided appended to /bin/bash -c
FROM firecracker-containerd-test as firecracker-containerd-integ-test
ARG FIRECRACKER_TARGET=x86_64-unknown-linux-musl

COPY _submodules/firecracker/target/$FIRECRACKER_TARGET/release/firecracker /usr/local/bin/
COPY _submodules/firecracker/target/$FIRECRACKER_TARGET/release/jailer /usr/local/bin/
COPY _submodules/runc/runc /usr/local/bin
COPY tools/image-builder/rootfs.img /var/lib/firecracker-containerd/runtime/default-rootfs.img

# pull the images the tests need into the content store so we don't need internet
# access during the tests themselves
COPY tools/docker/config.toml /etc/containerd/config.toml
RUN containerd 2>/dev/null & \
	ctr content fetch docker.io/library/alpine:3.10.1 >/dev/null && \
	ctr content fetch docker.io/mlabbe/iperf3:3.6-r0 >/dev/null

RUN mkdir -p /var/lib/firecracker-containerd/naive
RUN make -C /firecracker-containerd demo-network
RUN make -C /firecracker-containerd/internal test-bridged-tap && \
	cp /firecracker-containerd/internal/test-bridged-tap /opt/cni/bin/ && \
	chmod a+x /firecracker-containerd/internal/test-bridged-tap

COPY tools/docker/entrypoint.sh /entrypoint

ENTRYPOINT ["/entrypoint"]
CMD ["exec /bin/bash"]




# TODO Add a stage for the devmapper snapshotter implementation (as opposed to naive implementation)




# Debugging image that starts up containerd and the naive snapshotter and includes some additional basic debugging tools.
# TODO add firectl here
FROM firecracker-containerd-integ-test as firecracker-containerd-dev
RUN apt-get update \
	&& apt-get install -y \
		strace \
		less \
		procps \
		util-linux
