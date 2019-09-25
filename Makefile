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

SUBDIRS:=agent runtime snapshotter internal examples firecracker-control/cmd/containerd eventbridge
TEST_SUBDIRS:=$(addprefix test-,$(SUBDIRS))
INTEG_TEST_SUBDIRS:=$(addprefix integ-test-,$(SUBDIRS))

export INSTALLROOT?=/usr/local
export STATIC_AGENT

export DOCKER_IMAGE_TAG?=latest

GOPATH:=$(shell go env GOPATH)
BINPATH:=$(abspath ./bin)
SUBMODULES=_submodules
UID:=$(shell id -u)

FIRECRACKER_DIR=$(SUBMODULES)/firecracker
FIRECRACKER_TARGET?=x86_64-unknown-linux-musl
FIRECRACKER_BIN=$(FIRECRACKER_DIR)/target/$(FIRECRACKER_TARGET)/release/firecracker
JAILER_BIN=$(FIRECRACKER_DIR)/target/$(FIRECRACKER_TARGET)/release/jailer
FIRECRACKER_BUILDER_NAME?=firecracker-builder
CARGO_CACHE_VOLUME_NAME?=cargocache

RUNC_DIR=$(SUBMODULES)/runc
RUNC_BIN=$(RUNC_DIR)/runc
RUNC_BUILDER_NAME?=runc-builder

# Set this to pass additional commandline flags to the go compiler, e.g. "make test EXTRAGOARGS=-v"
export EXTRAGOARGS?=

all: $(SUBDIRS)

$(SUBDIRS):
	$(MAKE) -C $@

proto:
	PATH=$(BINPATH):$(PATH) $(MAKE) -C proto/ proto

clean:
	for d in $(SUBDIRS); do $(MAKE) -C $$d clean; done
	- rm -rf $(BINPATH)/
	$(MAKE) -C $(RUNC_DIR) clean
	$(MAKE) firecracker-clean
	rm -f tools/*stamp *stamp
	$(MAKE) -C tools/image-builder clean-in-docker

rmi-if-exists = $(if $(shell docker images -q $(1)),docker rmi $(1),true)
distclean: clean
	for d in $(SUBDIRS); do $(MAKE) -C $$d distclean; done
	$(MAKE) -C tools/image-builder distclean
	$(call rmi-if-exists,localhost/$(RUNC_BUILDER_NAME):$(DOCKER_IMAGE_TAG))
	$(call rmi-if-exists,localhost/$(FIRECRACKER_BUILDER_NAME):$(DOCKER_IMAGE_TAG))
	docker volume rm -f $(CARGO_CACHE_VOLUME_NAME)
	$(call rmi-if-exists,localhost/firecracker-containerd-naive-integ-test:$(DOCKER_IMAGE_TAG))
	$(call rmi-if-exists,localhost/firecracker-containerd-test:$(DOCKER_IMAGE_TAG))

lint:
	$(BINPATH)/ltag -t ./.headers -excludes "tools $(SUBMODULES)" -check -v
	$(BINPATH)/git-validation -run DCO,short-subject -range HEAD~20..HEAD
	$(BINPATH)/golangci-lint run

deps:
	curl -sfL https://raw.githubusercontent.com/golangci/golangci-lint/master/install.sh| sh -s -- -b $(BINPATH) v1.16.0
	$(BINPATH)/golangci-lint --version
	GOBIN=$(BINPATH) GO111MODULE=off go get -u github.com/vbatts/git-validation
	GOBIN=$(BINPATH) GO111MODULE=off go get -u github.com/kunalkushwaha/ltag
	GOBIN=$(BINPATH) GO111MODULE=off go get -u github.com/containerd/ttrpc/cmd/protoc-gen-gogottrpc
	GOBIN=$(BINPATH) GO111MODULE=off go get -u github.com/gogo/protobuf/protoc-gen-gogo

install:
	for d in $(SUBDIRS); do $(MAKE) -C $$d install; done

test: $(TEST_SUBDIRS)

test-in-docker: firecracker-containerd-test-image
	docker run --rm -it --user builder \
		--env HOME=/home/builder \
		--env GOPATH=/home/builder/go \
		--env EXTRAGOARGS="$(EXTRAGOARGS)" \
		--workdir /firecracker-containerd \
		localhost/firecracker-containerd-test:$(DOCKER_IMAGE_TAG) \
		"make test"

$(TEST_SUBDIRS):
	$(MAKE) -C $(patsubst test-%,%,$@) test

integ-test: $(INTEG_TEST_SUBDIRS)

$(INTEG_TEST_SUBDIRS): test-images
	$(MAKE) -C $(patsubst integ-test-%,%,$@) integ-test

image: $(RUNC_BIN) agent
	mkdir -p tools/image-builder/files_ephemeral/usr/local/bin
	mkdir -p tools/image-builder/files_ephemeral/var/firecracker-containerd-test/scripts
	for f in tools/docker/scripts/*; do test -f $$f && install -m 755 $$f tools/image-builder/files_ephemeral/var/firecracker-containerd-test/scripts; done
	cp $(RUNC_BIN) tools/image-builder/files_ephemeral/usr/local/bin
	cp agent/agent tools/image-builder/files_ephemeral/usr/local/bin
	touch tools/image-builder/files_ephemeral
	$(MAKE) -C tools/image-builder all-in-docker

test-images: test-images-stamp

test-images-stamp: | image firecracker-containerd-naive-integ-test-image firecracker-containerd-test-image
	touch $@

firecracker-containerd-test-image:
	DOCKER_BUILDKIT=1 docker build \
		--progress=plain \
		--file tools/docker/Dockerfile \
		--target firecracker-containerd-test \
		--tag localhost/firecracker-containerd-test:${DOCKER_IMAGE_TAG} .

firecracker-containerd-naive-integ-test-image: $(RUNC_BIN) $(FIRECRACKER_BIN) $(JAILER_BIN)
	DOCKER_BUILDKIT=1 docker build \
		--progress=plain \
		--file tools/docker/Dockerfile \
		--target firecracker-containerd-naive-integ-test \
		--build-arg FIRECRACKER_TARGET=$(FIRECRACKER_TARGET) \
		--tag localhost/firecracker-containerd-naive-integ-test:${DOCKER_IMAGE_TAG} .

.PHONY: all $(SUBDIRS) clean proto deps lint install image test-images firecracker-container-test-image firecracker-containerd-naive-integ-test-image test test-in-docker $(TEST_SUBDIRS) integ-test $(INTEG_TEST_SUBDIRS)

##########################
# CNI Network
##########################

CNI_BIN_ROOT?=/opt/cni/bin
$(CNI_BIN_ROOT):
	mkdir -p $(CNI_BIN_ROOT)

PTP_BIN?=$(CNI_BIN_ROOT)/ptp
$(PTP_BIN): $(CNI_BIN_ROOT)
	GOBIN=$(CNI_BIN_ROOT) GO111MODULE=off go get -u github.com/containernetworking/plugins/plugins/main/ptp

HOSTLOCAL_BIN?=$(CNI_BIN_ROOT)/host-local
$(HOSTLOCAL_BIN): $(CNI_BIN_ROOT)
	GOBIN=$(CNI_BIN_ROOT) GO111MODULE=off go get -u github.com/containernetworking/plugins/plugins/ipam/host-local

TC_REDIRECT_TAP_BIN?=$(CNI_BIN_ROOT)/tc-redirect-tap
$(TC_REDIRECT_TAP_BIN): $(CNI_BIN_ROOT)
	GOBIN=$(CNI_BIN_ROOT) go install github.com/firecracker-microvm/firecracker-go-sdk/cni/cmd/tc-redirect-tap

FCNET_CONFIG?=/etc/cni/conf.d/fcnet.conflist
$(FCNET_CONFIG):
	mkdir -p $(dir $(FCNET_CONFIG))
	cp tools/demo/fcnet.conflist $(FCNET_CONFIG)

.PHONY: demo-network
demo-network: $(PTP_BIN) $(HOSTLOCAL_BIN) $(TC_REDIRECT_TAP_BIN) $(FCNET_CONFIG)

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
		--volume $(PWD)/$(FIRECRACKER_DIR):/src \
		--volume $(CARGO_CACHE_VOLUME_NAME):/usr/local/cargo/registry \
		-e HOME=/tmp \
		--workdir /src \
		localhost/$(FIRECRACKER_BUILDER_NAME):$(DOCKER_IMAGE_TAG) \
		cargo build --release --target $(FIRECRACKER_TARGET)

.PHONY: firecracker-clean
firecracker-clean:
	rm -f $(FIRECRACKER_BIN) $(JAILER_BIN)
	- docker run --rm -it --user $(UID) \
		--volume $(PWD)/$(FIRECRACKER_DIR):/src \
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
		--volume $(PWD)/$(RUNC_DIR):/gopath/src/github.com/opencontainers/runc \
		--volume $(PWD)/deps:/target \
		-e HOME=/tmp \
		-e GOPATH=/gopath \
		--workdir /gopath/src/github.com/opencontainers/runc \
		localhost/$(RUNC_BUILDER_NAME):$(DOCKER_IMAGE_TAG) \
		make static
