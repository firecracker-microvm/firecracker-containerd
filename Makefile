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

SUBDIRS:=agent runtime internal examples firecracker-control/cmd/containerd eventbridge
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

FIRECRACKER_CONTAINERD_BUILDER_IMAGE?=golang:1.13-stretch
export FIRECRACKER_CONTAINERD_TEST_IMAGE?=localhost/firecracker-containerd-test
export GO_CACHE_VOLUME_NAME?=gocache

FIRECRACKER_DIR=$(SUBMODULES)/firecracker
FIRECRACKER_TARGET?=x86_64-unknown-linux-musl
FIRECRACKER_BIN=$(FIRECRACKER_DIR)/target/$(FIRECRACKER_TARGET)/release/firecracker
JAILER_BIN=$(FIRECRACKER_DIR)/target/$(FIRECRACKER_TARGET)/release/jailer
FIRECRACKER_BUILDER_NAME?=firecracker-builder
CARGO_CACHE_VOLUME_NAME?=cargocache

RUNC_DIR=$(SUBMODULES)/runc
RUNC_BIN=$(RUNC_DIR)/runc
RUNC_BUILDER_NAME?=runc-builder

PROTO_BUILDER_NAME?=proto-builder

# Set this to pass additional commandline flags to the go compiler, e.g. "make test EXTRAGOARGS=-v"
export EXTRAGOARGS?=

all: $(SUBDIRS) $(GOMOD) $(GOSUM) cni-bins test-cni-bins

$(SUBDIRS):
	$(MAKE) -C $@ EXTRAGOARGS=$(EXTRAGOARGS)

%-in-docker:
	docker run --rm -it \
		--user $(UID):$(GID) \
		--volume $(CURDIR):/src \
		--volume $(GO_CACHE_VOLUME_NAME):/go \
		--env HOME=/tmp \
		--env GOPATH=/go \
		--env GO111MODULES=on \
		--env STATIC_AGENT=on \
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
	docker volume rm -f $(CARGO_CACHE_VOLUME_NAME)
	docker volume rm -f $(GO_CACHE_VOLUME_NAME)
	$(call rmi-if-exists,$(FIRECRACKER_CONTAINERD_TEST_IMAGE):$(DOCKER_IMAGE_TAG))
	$(call rmi-if-exists,localhost/$(PROTO_BUILDER_NAME):$(DOCKER_IMAGE_TAG))

lint:
	$(BINPATH)/ltag -t ./.headers -excludes "tools $(SUBMODULES)" -check -v
	$(BINPATH)/git-validation -run DCO,short-subject -range HEAD~20..HEAD
	$(BINPATH)/golangci-lint run

tidy:
	go mod tidy

deps:
	curl -sfL https://raw.githubusercontent.com/golangci/golangci-lint/master/install.sh| sh -s -- -b $(BINPATH) v1.21.0
	$(BINPATH)/golangci-lint --version
	GOBIN=$(BINPATH) GO111MODULE=off go get -u github.com/vbatts/git-validation
	GOBIN=$(BINPATH) GO111MODULE=off go get -u github.com/kunalkushwaha/ltag

install:
	for d in $(SUBDIRS); do $(MAKE) -C $$d install; done

image: $(RUNC_BIN) agent-in-docker
	mkdir -p tools/image-builder/files_ephemeral/usr/local/bin
	mkdir -p tools/image-builder/files_ephemeral/var/firecracker-containerd-test/scripts
	for f in tools/docker/scripts/*; do test -f $$f && install -m 755 $$f tools/image-builder/files_ephemeral/var/firecracker-containerd-test/scripts; done
	cp $(RUNC_BIN) tools/image-builder/files_ephemeral/usr/local/bin
	cp agent/agent tools/image-builder/files_ephemeral/usr/local/bin
	touch tools/image-builder/files_ephemeral
	$(MAKE) -C tools/image-builder all-in-docker

test: $(TEST_SUBDIRS)

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

test-images-stamp: | image firecracker-containerd-test-image
	touch $@

firecracker-containerd-test-image: all-in-docker firecracker runc test-cni-bins cni-bins default-vmlinux
	DOCKER_BUILDKIT=1 docker build \
		--progress=plain \
		--file tools/docker/Dockerfile.integ-test \
		--build-arg FIRECRACKER_TARGET=$(FIRECRACKER_TARGET) \
		--tag $(FIRECRACKER_CONTAINERD_TEST_IMAGE):${DOCKER_IMAGE_TAG} .

.PHONY: all $(SUBDIRS) clean proto deps lint install image test-images firecracker-container-test-image firecracker-containerd-integ-test-image test test-in-docker $(TEST_SUBDIRS) integ-test $(INTEG_TEST_SUBDIRS) tidy

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
		"https://s3.amazonaws.com/spec.ccfc.min/img/hello/kernel/hello-vmlinux.bin"
	echo "882fa465c43ab7d92e31bd4167da3ad6a82cb9230f9b0016176df597c6014cef $@" | sha256sum -c -
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

ROOTFS_SLOW_REBOOT_INSTALLPATH=$(FIRECRACKER_CONTAINERD_RUNTIME_DIR)/rootfs-slow-reboot.img
$(ROOTFS_SLOW_REBOOT_INSTALLPATH): tools/image-builder/rootfs-slow-reboot.img $(FIRECRACKER_CONTAINERD_RUNTIME_DIR)
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
install-test-rootfs: $(ROOTFS_SLOW_REBOOT_INSTALLPATH)

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
	GOBIN=$(dir $@) go install github.com/firecracker-microvm/firecracker-go-sdk/cni/cmd/tc-redirect-tap

TEST_BRIDGED_TAP_BIN?=$(BINPATH)/test-bridged-tap
$(TEST_BRIDGED_TAP_BIN): $(shell find internal/cmd/test-bridged-tap -name *.go) $(GOMOD) $(GOSUM)
	go build -o $@ $(CURDIR)/internal/cmd/test-bridged-tap

.PHONY: cni-bins
cni-bins: $(BRIDGE_BIN) $(PTP_BIN) $(HOSTLOCAL_BIN) $(FIREWALL_BIN) $(TC_REDIRECT_TAP_BIN)

.PHONY: test-cni-bins
test-cni-bins: $(TEST_BRIDGED_TAP_BIN)

.PHONY: install-cni-bins
install-cni-bins: cni-bins $(CNI_BIN_ROOT)
	install -D -o root -g root -m755 -t $(CNI_BIN_ROOT) $(BRIDGE_BIN)
	install -D -o root -g root -m755 -t $(CNI_BIN_ROOT) $(PTP_BIN)
	install -D -o root -g root -m755 -t $(CNI_BIN_ROOT) $(HOSTLOCAL_BIN)
	install -D -o root -g root -m755 -t $(CNI_BIN_ROOT) $(FIREWALL_BIN)
	install -D -o root -g root -m755 -t $(CNI_BIN_ROOT) $(TC_REDIRECT_TAP_BIN)

.PHONY: install-test-cni-bins
install-test-cni-bins: test-cni-bins $(CNI_BIN_ROOT)
	install -D -o root -g root -m755 -t $(CNI_BIN_ROOT) $(TEST_BRIDGED_TAP_BIN)

FCNET_CONFIG?=/etc/cni/conf.d/fcnet.conflist
$(FCNET_CONFIG):
	mkdir -p $(dir $(FCNET_CONFIG))
	install -o root -g root -m644 tools/demo/fcnet.conflist $(FCNET_CONFIG)

FCNET_BRIDGE_CONFIG?=/etc/network/interfaces.d/fc-br0
$(FCNET_BRIDGE_CONFIG):
	mkdir -p $(dir $(FCNET_BRIDGE_CONFIG))
	install -o root -g root -m644 tools/demo/fc-br0.interface $(FCNET_BRIDGE_CONFIG)

.PHONY: demo-network
demo-network: install-cni-bins $(FCNET_CONFIG)

##########################
# Firecracker submodule
##########################
.PHONY: firecracker
firecracker: $(FIRECRACKER_BIN) $(JAILER_BIN)

.PHONY: install-firecracker
install-firecracker: firecracker
	install -D -o root -g root -m755 -t $(INSTALLROOT)/bin $(FIRECRACKER_BIN)
	install -D -o root -g root -m755 -t $(INSTALLROOT)/bin $(JAILER_BIN)

$(FIRECRACKER_DIR)/Cargo.toml:
	git submodule update --init --recursive $(FIRECRACKER_DIR)

tools/firecracker-builder-stamp: tools/docker/Dockerfile.firecracker-builder
	docker build \
		-t localhost/$(FIRECRACKER_BUILDER_NAME):$(DOCKER_IMAGE_TAG) \
		-f tools/docker/Dockerfile.firecracker-builder \
		tools/docker
	touch $@

$(FIRECRACKER_BIN) $(JAILER_BIN): $(FIRECRACKER_DIR)/Cargo.toml tools/firecracker-builder-stamp
	docker run --rm -it --user $(UID) \
		--volume $(CURDIR)/$(FIRECRACKER_DIR):/src \
		--volume $(CARGO_CACHE_VOLUME_NAME):/usr/local/cargo/registry \
		-e HOME=/tmp \
		--workdir /src \
		localhost/$(FIRECRACKER_BUILDER_NAME):$(DOCKER_IMAGE_TAG) \
		cargo build --release --target $(FIRECRACKER_TARGET)

.PHONY: firecracker-clean
firecracker-clean:
	rm -f $(FIRECRACKER_BIN) $(JAILER_BIN)
	- docker run --rm -it --user $(UID) \
		--volume $(CURDIR)/$(FIRECRACKER_DIR):/src \
		-e HOME=/tmp \
		--workdir /src \
		localhost/$(FIRECRACKER_BUILDER_NAME):$(DOCKER_IMAGE_TAG) \
		cargo clean

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
	docker run --rm -it --user $(UID) \
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
