# Copyright Amazon.com, Inc. or its affiliates. All Rights Reserved.
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

SUBDIRS:=agent runtime examples firecracker-control/cmd/containerd snapshotter docker-credential-mmds volume
TEST_SUBDIRS:=$(addprefix test-,$(SUBDIRS))
INTEG_TEST_SUBDIRS:=$(addprefix integ-test-,$(SUBDIRS))

export INSTALLROOT?=/usr/local
export STATIC_AGENT

export DOCKER_IMAGE_TAG?=latest

GOMOD := $(shell go env GOMOD)
GOSUM := $(GOMOD:.mod=.sum)
GOPATH:=$(shell go env GOPATH)
BINPATH:=$(abspath ./bin)
SUBMODULES=_submodules
UID:=$(shell id -u)
GID:=$(shell id -g)

FIRECRACKER_CONTAINERD_BUILDER_IMAGE?=golang:1.17-bullseye
export FIRECRACKER_CONTAINERD_TEST_IMAGE?=localhost/firecracker-containerd-test
export GO_CACHE_VOLUME_NAME?=gocache

# This Makefile uses Firecracker's pre-build Linux kernels for x86_64 and aarch64.
host_arch=$(shell uname -m)
ifeq ($(host_arch),x86_64)
	kernel_sha256sum="ea5e7d5cf494a8c4ba043259812fc018b44880d70bcbbfc4d57d2760631b1cd6"
	kernel_config_pattern=x86
else ifeq ($(host_arch),aarch64)
	kernel_sha256sum="e2d7c3d6cc123de9e6052d1f434ca7b47635a1f630d63f7fcc1b7164958375e4"
	kernel_config_pattern=arm64
else
$(error "$(host_arch) is not supported by Firecracker")
endif
FIRECRACKER_TARGET?=$(host_arch)-unknown-linux-musl

FIRECRACKER_DIR=$(SUBMODULES)/firecracker
FIRECRACKER_BIN=bin/firecracker
FIRECRACKER_BUILDER_NAME?=firecracker-builder
CARGO_CACHE_VOLUME_NAME?=cargocache

KERNEL_VERSIONS=4.14 5.10
KERNEL_VERSION?=4.14
ifeq ($(filter $(KERNEL_VERSION),$(KERNEL_VERSIONS)),)
$(error "Kernel version $(KERNEL_VERSION) is not supported. Supported versions are $(KERNEL_VERSIONS)")
endif

KERNEL_CONFIG=$(abspath ./tools/kernel-configs/microvm-kernel-$(host_arch)-$(KERNEL_VERSION).config)
# Copied from https://github.com/firecracker-microvm/firecracker/blob/v1.1.0/tools/devtool#L2082
# This allows us to specify a kernel without the patch version, but still get the correct build path to reference the kernel binary
KERNEL_FULL_VERSION=$(shell cat "$(KERNEL_CONFIG)" | grep -Po "^\# Linux\/$(kernel_config_pattern) (([0-9]+.)[0-9]+)" | cut -d ' ' -f 3)
KERNEL_BIN=$(FIRECRACKER_DIR)/build/kernel/linux-$(KERNEL_FULL_VERSION)/vmlinux-$(KERNEL_FULL_VERSION)-$(host_arch).bin

RUNC_DIR=$(SUBMODULES)/runc
RUNC_BIN=$(RUNC_DIR)/runc
RUNC_BUILDER_NAME?=runc-builder

STARGZ_DIR=$(SUBMODULES)/stargz-snapshotter
STARGZ_BIN=$(STARGZ_DIR)/out/containerd-stargz-grpc
STARGZ_BUILDER_NAME?=stargz-builder

PROTO_BUILDER_NAME?=proto-builder

# Set this to pass additional commandline flags to the go compiler, e.g. "make test EXTRAGOARGS=-v"
export EXTRAGOARGS?=

all: $(SUBDIRS) $(GOMOD) $(GOSUM) cni-bins test-cni-bins

$(SUBDIRS):
	$(MAKE) -C $@ EXTRAGOARGS=$(EXTRAGOARGS)

%-in-docker:
	docker run --rm \
		--user $(UID):$(GID) \
		--volume $(CURDIR):/src \
		--volume $(GO_CACHE_VOLUME_NAME):/go \
		--env HOME=/tmp \
		--env GOPATH=/go \
		--env GO111MODULES=on \
		--env STATIC_AGENT=on \
		--env GOPROXY=$(shell go env GOPROXY) \
		--workdir /src \
		$(FIRECRACKER_CONTAINERD_BUILDER_IMAGE) \
		$(MAKE) $(subst -in-docker,,$@)

proto:
	DOCKER_BUILDKIT=1 docker build \
		--file tools/docker/Dockerfile.proto-builder \
		--tag localhost/$(PROTO_BUILDER_NAME):${DOCKER_IMAGE_TAG} \
		$(CURDIR)/tools/docker
	PATH=$(BINPATH):$(PATH) $(MAKE) -C proto/ proto-docker

clean:
	for d in $(SUBDIRS); do $(MAKE) -C $$d clean; done
	- rm -rf $(BINPATH)/
	$(MAKE) -C $(RUNC_DIR) clean
	$(MAKE) firecracker-clean
	rm -f tools/*stamp *stamp
	$(MAKE) -C tools/image-builder clean-in-docker
	rm -f $(DEFAULT_VMLINUX_NAME)

rmi-if-exists = $(if $(shell docker images -q $(1)),docker rmi $(1),true)
distclean: clean
	for d in $(SUBDIRS); do $(MAKE) -C $$d distclean; done
	$(MAKE) -C tools/image-builder distclean
	$(call rmi-if-exists,localhost/$(RUNC_BUILDER_NAME):$(DOCKER_IMAGE_TAG))
	$(call rmi-if-exists,localhost/$(FIRECRACKER_BUILDER_NAME):$(DOCKER_IMAGE_TAG))
	$(call rmi-if-exists,localhost/$(STARGZ_BUILDER_NAME):$(DOCKER_IMAGE_TAG))
	docker volume rm -f $(CARGO_CACHE_VOLUME_NAME)
	docker volume rm -f $(GO_CACHE_VOLUME_NAME)
	$(call rmi-if-exists,$(FIRECRACKER_CONTAINERD_TEST_IMAGE):$(DOCKER_IMAGE_TAG))
	$(call rmi-if-exists,localhost/$(PROTO_BUILDER_NAME):$(DOCKER_IMAGE_TAG))

deps = \
	$(BINPATH)/golangci-lint \
	$(BINPATH)/git-validation \
	$(BINPATH)/ltag

lint: $(deps)
	$(BINPATH)/ltag -t ./.headers -excludes "tools $(SUBMODULES)" -check -v
	$(BINPATH)/git-validation -run DCO,short-subject -range HEAD~20..HEAD
	$(BINPATH)/golangci-lint run

tidy:
	./tools/tidy.sh

$(BINPATH)/golangci-lint:
	curl -sfL https://raw.githubusercontent.com/golangci/golangci-lint/master/install.sh| sh -s -- -b $(BINPATH) v1.48.0
	$(BINPATH)/golangci-lint --version

$(BINPATH)/git-validation:
	GOBIN=$(BINPATH) GO111MODULE=off go get -u github.com/vbatts/git-validation

$(BINPATH)/ltag:
	GOBIN=$(BINPATH) GO111MODULE=off go get -u github.com/kunalkushwaha/ltag

install:
	for d in $(SUBDIRS); do $(MAKE) -C $$d install; done

files_ephemeral: $(RUNC_BIN) agent-in-docker
	mkdir -p tools/image-builder/files_ephemeral/usr/local/bin
	mkdir -p tools/image-builder/files_ephemeral/var/firecracker-containerd-test/scripts
	for f in tools/docker/scripts/*; do test -f $$f && install -m 755 $$f tools/image-builder/files_ephemeral/var/firecracker-containerd-test/scripts; done
	cp $(RUNC_BIN) tools/image-builder/files_ephemeral/usr/local/bin
	cp agent/agent tools/image-builder/files_ephemeral/usr/local/bin
	touch tools/image-builder/files_ephemeral

files_ephemeral_stargz: $(STARGZ_BIN) docker-credential-mmds-in-docker volume-in-docker
	mkdir -p tools/image-builder/files_ephemeral_stargz/usr/local/bin
	cp docker-credential-mmds/docker-credential-mmds tools/image-builder/files_ephemeral_stargz/usr/local/bin
	cp volume/volume-init tools/image-builder/files_ephemeral_stargz/usr/local/bin
	cp $(STARGZ_BIN) tools/image-builder/files_ephemeral_stargz/usr/local/bin

image: files_ephemeral
	$(MAKE) -C tools/image-builder all-in-docker

image-stargz: files_ephemeral files_ephemeral_stargz
	$(MAKE) -C tools/image-builder stargz-in-docker

test: $(TEST_SUBDIRS)
	go test ./... $(EXTRAGOARGS)

# test-in-docker runs all unit tests inside a docker container. Use "integ-test" to
# run the integ-tests inside containers.
test-in-docker:
	docker run --rm -it \
		--user $(UID):$(GID) \
		--volume $(CURDIR):/src \
		--volume $(GO_CACHE_VOLUME_NAME):/go \
		--env HOME=/tmp \
		--env GOPATH=/go \
		--env EXTRAGOARGS="$(EXTRAGOARGS)" \
		--env DISABLE_ROOT_TESTS=$(DISABLE_ROOT_TESTS) \
		--entrypoint=/bin/bash \
		--workdir /src \
		$(FIRECRACKER_CONTAINERD_BUILDER_IMAGE) \
		-c "make test"

$(TEST_SUBDIRS):
	$(MAKE) -C $(patsubst test-%,%,$@) test

integ-test: $(INTEG_TEST_SUBDIRS)

$(INTEG_TEST_SUBDIRS): test-images
	$(MAKE) -C $(patsubst integ-test-%,%,$@) integ-test

test-images: test-images-stamp

test-images-stamp: | image image-stargz firecracker-containerd-test-image
	touch $@

firecracker-containerd-test-image: all-in-docker firecracker runc test-cni-bins cni-bins default-vmlinux kernel
	DOCKER_BUILDKIT=1 docker build \
		--progress=plain \
		--file tools/docker/Dockerfile.integ-test \
		--build-arg FIRECRACKER_TARGET=$(FIRECRACKER_TARGET) \
		--tag $(FIRECRACKER_CONTAINERD_TEST_IMAGE):${DOCKER_IMAGE_TAG} .

.PHONY: all $(SUBDIRS) clean proto lint install image test-images firecracker-container-test-image firecracker-containerd-integ-test-image test test-in-docker $(TEST_SUBDIRS) integ-test $(INTEG_TEST_SUBDIRS) tidy

##########################
# Runtime config
##########################

FIRECRACKER_CONTAINERD_RUNTIME_DIR?=/var/lib/firecracker-containerd/runtime
ETC_CONTAINERD?=/etc/containerd
$(FIRECRACKER_CONTAINERD_RUNTIME_DIR) $(ETC_CONTAINERD):
	mkdir --mode 0700 --parents $@

DEFAULT_VMLINUX_NAME?=default-vmlinux.bin
$(DEFAULT_VMLINUX_NAME):
	curl --silent --show-error --retry 3 --max-time 30 --output $@ \
		"https://s3.amazonaws.com/spec.ccfc.min/img/quickstart_guide/$(host_arch)/kernels/vmlinux.bin"
	echo "$(kernel_sha256sum) $@" | sha256sum -c -
	chmod 0400 $@

DEFAULT_VMLINUX_INSTALLPATH=$(FIRECRACKER_CONTAINERD_RUNTIME_DIR)/$(DEFAULT_VMLINUX_NAME)
$(DEFAULT_VMLINUX_INSTALLPATH): $(DEFAULT_VMLINUX_NAME) $(FIRECRACKER_CONTAINERD_RUNTIME_DIR)
	install -D -o root -g root -m400 $(DEFAULT_VMLINUX_NAME) $@

DEFAULT_ROOTFS_INSTALLPATH=$(FIRECRACKER_CONTAINERD_RUNTIME_DIR)/default-rootfs.img
$(DEFAULT_ROOTFS_INSTALLPATH): tools/image-builder/rootfs.img $(FIRECRACKER_CONTAINERD_RUNTIME_DIR)
	install -D -o root -g root -m400 tools/image-builder/rootfs.img $@

DEFAULT_RUNC_JAILER_CONFIG_INSTALLPATH?=/etc/containerd/firecracker-runc-config.json
$(DEFAULT_RUNC_JAILER_CONFIG_INSTALLPATH): $(ETC_CONTAINERD) runtime/firecracker-runc-config.json.example
	install -D -o root -g root -m400 runtime/firecracker-runc-config.json.example $@

ROOTFS_DEBUG_INSTALLPATH=$(FIRECRACKER_CONTAINERD_RUNTIME_DIR)/rootfs-debug.img
$(ROOTFS_DEBUG_INSTALLPATH): tools/image-builder/rootfs-debug.img $(FIRECRACKER_CONTAINERD_RUNTIME_DIR)
	install -D -o root -g root -m400 $< $@

ROOTFS_STARGZ_INSTALLPATH=$(FIRECRACKER_CONTAINERD_RUNTIME_DIR)/rootfs-stargz.img
$(ROOTFS_STARGZ_INSTALLPATH): tools/image-builder/rootfs-stargz.img $(FIRECRACKER_CONTAINERD_RUNTIME_DIR)
	install -D -o root -g root -m400 $< $@

.PHONY: default-vmlinux
default-vmlinux: $(DEFAULT_VMLINUX_NAME)

.PHONY: install-default-vmlinux
install-default-vmlinux: $(DEFAULT_VMLINUX_INSTALLPATH)

.PHONY: install-default-rootfs
install-default-rootfs: $(DEFAULT_ROOTFS_INSTALLPATH)

.PHONY: install-default-runc-jailer-config
install-default-runc-jailer-config: $(DEFAULT_RUNC_JAILER_CONFIG_INSTALLPATH)

.PHONY: install-test-rootfs
install-test-rootfs: $(ROOTFS_DEBUG_INSTALLPATH)

.PHONY: install-stargz-rootfs
install-stargz-rootfs: $(ROOTFS_STARGZ_INSTALLPATH)

##########################
# CNI Network
##########################

CNI_BIN_ROOT?=/opt/cni/bin
$(CNI_BIN_ROOT):
	mkdir --mode 0755 --parents $@

BRIDGE_BIN?=$(BINPATH)/bridge
$(BRIDGE_BIN):
	GOBIN=$(dir $@) GO111MODULE=off go get -u github.com/containernetworking/plugins/plugins/main/bridge

PTP_BIN?=$(BINPATH)/ptp
$(PTP_BIN):
	GOBIN=$(dir $@) GO111MODULE=off go get -u github.com/containernetworking/plugins/plugins/main/ptp

HOSTLOCAL_BIN?=$(BINPATH)/host-local
$(HOSTLOCAL_BIN):
	GOBIN=$(dir $@) GO111MODULE=off go get -u github.com/containernetworking/plugins/plugins/ipam/host-local

FIREWALL_BIN?=$(BINPATH)/firewall
$(FIREWALL_BIN):
	GOBIN=$(dir $@) GO111MODULE=off go get -u github.com/containernetworking/plugins/plugins/meta/firewall

TC_REDIRECT_TAP_BIN?=$(BINPATH)/tc-redirect-tap
$(TC_REDIRECT_TAP_BIN):
	GOBIN=$(dir $@) go install github.com/awslabs/tc-redirect-tap/cmd/tc-redirect-tap

TEST_BRIDGED_TAP_BIN?=$(BINPATH)/test-bridged-tap
$(TEST_BRIDGED_TAP_BIN): $(shell find internal/cmd/test-bridged-tap -name *.go) $(GOMOD) $(GOSUM)
	go build -o $@ $(CURDIR)/internal/cmd/test-bridged-tap

LOOPBACK_BIN?=$(BINPATH)/loopback
$(LOOPBACK_BIN):
	GOBIN=$(dir $@) GO111MODULE=off go get -u github.com/containernetworking/plugins/plugins/main/loopback

.PHONY: cni-bins
cni-bins: $(BRIDGE_BIN) $(PTP_BIN) $(HOSTLOCAL_BIN) $(FIREWALL_BIN) $(TC_REDIRECT_TAP_BIN) $(LOOPBACK_BIN)

.PHONY: test-cni-bins
test-cni-bins: $(TEST_BRIDGED_TAP_BIN)

.PHONY: install-cni-bins
install-cni-bins: cni-bins $(CNI_BIN_ROOT)
	install -D -o root -g root -m755 -t $(CNI_BIN_ROOT) $(BRIDGE_BIN)
	install -D -o root -g root -m755 -t $(CNI_BIN_ROOT) $(PTP_BIN)
	install -D -o root -g root -m755 -t $(CNI_BIN_ROOT) $(HOSTLOCAL_BIN)
	install -D -o root -g root -m755 -t $(CNI_BIN_ROOT) $(FIREWALL_BIN)
	install -D -o root -g root -m755 -t $(CNI_BIN_ROOT) $(TC_REDIRECT_TAP_BIN)
	install -D -o root -g root -m755 -t $(CNI_BIN_ROOT) $(LOOPBACK_BIN)

.PHONY: install-test-cni-bins
install-test-cni-bins: test-cni-bins $(CNI_BIN_ROOT)
	install -D -o root -g root -m755 -t $(CNI_BIN_ROOT) $(TEST_BRIDGED_TAP_BIN)

FCNET_CONFIG?=/etc/cni/conf.d/fcnet.conflist
$(FCNET_CONFIG): tools/demo/fcnet.conflist
	mkdir -p $(dir $(FCNET_CONFIG))
	install -o root -g root -m644 tools/demo/fcnet.conflist $(FCNET_CONFIG)

FCNET_BRIDGE_CONFIG?=/etc/network/interfaces.d/fc-br0
$(FCNET_BRIDGE_CONFIG): tools/demo/fc-br0.interface
	mkdir -p $(dir $(FCNET_BRIDGE_CONFIG))
	install -o root -g root -m644 tools/demo/fc-br0.interface $(FCNET_BRIDGE_CONFIG)

.PHONY: demo-network
demo-network: install-cni-bins $(FCNET_CONFIG)

##########################
# Firecracker submodule
##########################
.PHONY: firecracker
firecracker: $(FIRECRACKER_BIN)

.PHONY: install-firecracker
install-firecracker: firecracker
	install -D -o root -g root -m755 -t $(INSTALLROOT)/bin $(FIRECRACKER_BIN)

$(FIRECRACKER_DIR)/Cargo.toml:
	git submodule update --init --recursive $(FIRECRACKER_DIR)

$(FIRECRACKER_BIN): $(FIRECRACKER_DIR)/Cargo.toml
	$(FIRECRACKER_DIR)/tools/devtool -y build --release
	cp $(FIRECRACKER_DIR)/build/cargo_target/$(FIRECRACKER_TARGET)/release/firecracker $@

.PHONY: firecracker-clean
firecracker-clean:
	- $(FIRECRACKER_DIR)/tools/devtool -y distclean
	- rm $(FIRECRACKER_BIN)
	- rm $(KERNEL_BIN)

.PHONY: kernel
kernel: $(KERNEL_BIN)

$(KERNEL_BIN): $(KERNEL_CONFIG)
	cp $(KERNEL_CONFIG) $(FIRECRACKER_DIR)
	$(FIRECRACKER_DIR)/tools/devtool -y build_kernel --config $(KERNEL_CONFIG)

.PHONY: install-kernel
install-kernel: $(KERNEL_BIN)
	install -D -o root -g root -m400 $(KERNEL_BIN) $(DEFAULT_VMLINUX_INSTALLPATH)

##########################
# RunC submodule
##########################
.PHONY: runc
runc: $(RUNC_BIN)

$(RUNC_DIR)/VERSION:
	git submodule update --init --recursive $(RUNC_DIR)

tools/runc-builder-stamp: tools/docker/Dockerfile.runc-builder
	docker build \
		-t localhost/$(RUNC_BUILDER_NAME):$(DOCKER_IMAGE_TAG) \
		-f tools/docker/Dockerfile.runc-builder \
		tools/
	touch $@

$(RUNC_BIN): $(RUNC_DIR)/VERSION tools/runc-builder-stamp
	docker run --rm --user $(UID) \
		--volume $(CURDIR)/$(RUNC_DIR):/gopath/src/github.com/opencontainers/runc \
		--volume $(CURDIR)/deps:/target \
		-e HOME=/tmp \
		-e GOPATH=/gopath \
		--workdir /gopath/src/github.com/opencontainers/runc \
		localhost/$(RUNC_BUILDER_NAME):$(DOCKER_IMAGE_TAG) \
		make static

.PHONY: install-runc
install-runc: $(RUNC_BIN)
	install -D -o root -g root -m755 -t $(INSTALLROOT)/bin $(RUNC_BIN)

##########################
# Stargz submodule
##########################
.PHONY: stargz-snapshotter
stargz-snapshotter: $(STARGZ_BIN)

$(STARGZ_DIR)/go.mod:
	git submodule update --init --recursive $(STARGZ_DIR)

tools/stargz-builder-stamp: tools/docker/Dockerfile.stargz-builder
	docker build \
		-t localhost/$(STARGZ_BUILDER_NAME):$(DOCKER_IMAGE_TAG) \
		-f tools/docker/Dockerfile.stargz-builder \
		tools/
	touch $@

$(STARGZ_BIN): $(STARGZ_DIR)/go.mod tools/stargz-builder-stamp
	docker run --rm -it \
	--user $(UID):$(GID) \
	--volume $(GO_CACHE_VOLUME_NAME):/go \
	--volume $(CURDIR):/src \
	-e HOME=/tmp \
	-e GOPATH=/go \
	-e GOPROXY=$(shell go env GOPROXY) \
	--workdir /src/$(STARGZ_DIR) \
	localhost/$(STARGZ_BUILDER_NAME):$(DOCKER_IMAGE_TAG) \
	make

##########################
# eStargz formatted image 
##########################
CTR_REMOTE_BIN=$(STARGZ_DIR)/out/ctr-remote
DEFAULT_BASE_IMAGE=public.ecr.aws/amazonlinux/amazonlinux:latest
DEFAULT_ESGZ_IMAGE=ghcr.io/firecracker-microvm/firecracker-containerd/amazonlinux:latest-esgz
.PHONY: esgz-test-image push-esgz-test-image
esgz-test-image: stargz-snapshotter
	$(CTR_REMOTE_BIN) image pull --all-platforms $(DEFAULT_BASE_IMAGE)
	$(CTR_REMOTE_BIN) image optimize --all-platforms --oci $(DEFAULT_BASE_IMAGE) $(DEFAULT_ESGZ_IMAGE)

push-esgz-test-image:
	$(CTR_REMOTE_BIN) image push -u $(GH_USER):$(GH_PERSONAL_ACCESS_TOKEN) $(DEFAULT_ESGZ_IMAGE) $(DEFAULT_ESGZ_IMAGE)
