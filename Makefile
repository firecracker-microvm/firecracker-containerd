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

SUBDIRS:=agent runtime snapshotter examples
export INSTALLROOT?=/usr/local
export STATIC_AGENT

DOCKER_IMAGE_TAG?=latest

GOPATH:=$(shell go env GOPATH)
BINPATH:=$(abspath ./bin)
SUBMODULES=_submodules
RUNC_DIR=$(SUBMODULES)/runc
RUNC_BIN=$(RUNC_DIR)/runc
UID:=$(shell id -u)

all: $(SUBDIRS)

$(SUBDIRS):
	$(MAKE) -C $@

proto:
	PATH=$(BINPATH):$(PATH) $(MAKE) -C proto/ proto

clean:
	for d in $(SUBDIRS); do $(MAKE) -C $$d clean; done
	- rm -rf $(BINPATH)/
	$(MAKE) -C $(RUNC_DIR) clean
	rm -f *stamp
	$(MAKE) -C tools/image-builder clean-in-docker

distclean: clean
	docker rmi localhost/runc-builder:latest
	$(MAKE) -C tools/image-builder distclean

lint:
	$(BINPATH)/ltag -t ./.headers -excludes "tools $(SUBMODULES)" -check -v
	$(BINPATH)/git-validation -run DCO,dangling-whitespace,short-subject -range HEAD~20..HEAD
	$(BINPATH)/golangci-lint run

deps:
	curl -sfL https://raw.githubusercontent.com/golangci/golangci-lint/master/install.sh| sh -s -- -b $(BINPATH) v1.16.0
	$(BINPATH)/golangci-lint --version
	GOBIN=$(BINPATH) GO111MODULE=off go get -u github.com/vbatts/git-validation
	GOBIN=$(BINPATH) GO111MODULE=off go get -u github.com/kunalkushwaha/ltag
	GOBIN=$(BINPATH) GO111MODULE=off go get -u github.com/containerd/ttrpc/cmd/protoc-gen-gogottrpc
	GOBIN=$(BINPATH) GO111MODULE=off go get -u github.com/gogo/protobuf/protoc-gen-gogo

runc-builder: runc-builder-stamp

runc-builder-stamp: tools/docker/Dockerfile.runc-builder
	cd tools/docker && docker build -t localhost/runc-builder:latest -f Dockerfile.runc-builder .
	touch $@

$(RUNC_DIR)/VERSION:
	git submodule update --init --recursive $(RUNC_DIR)

runc: $(RUNC_BIN)

$(RUNC_BIN): $(RUNC_DIR)/VERSION runc-builder-stamp
	docker run --rm -it --user $(UID) \
		--volume $(PWD)/$(RUNC_DIR):/gopath/src/github.com/opencontainers/runc \
		--volume $(PWD)/deps:/target \
		-e HOME=/tmp \
		-e GOPATH=/gopath \
		--workdir /gopath/src/github.com/opencontainers/runc \
		localhost/runc-builder:latest \
		make runc

image: $(RUNC_BIN) agent
	mkdir -p tools/image-builder/files_ephemeral/usr/local/bin
	cp $(RUNC_BIN) tools/image-builder/files_ephemeral/usr/local/bin
	cp agent/agent tools/image-builder/files_ephemeral/usr/local/bin
	touch tools/image-builder/files_ephemeral
	$(MAKE) -C tools/image-builder all-in-docker

install:
	for d in $(SUBDIRS); do $(MAKE) -C $$d install; done

docker-image-unittest:
	docker build \
		--progress=plain \
		--file tools/docker/Dockerfile \
		--target firecracker-containerd-unittest \
		--tag localhost/firecracker-containerd-unittest:${DOCKER_IMAGE_TAG} .

docker-image-unittest-nonroot:
	docker build \
		--progress=plain \
		--file tools/docker/Dockerfile \
		--target firecracker-containerd-unittest-nonroot \
		--tag localhost/firecracker-containerd-unittest-nonroot:${DOCKER_IMAGE_TAG} .

docker-image-e2etest-naive:
	docker build \
		--progress=plain \
		--file tools/docker/Dockerfile \
		--target firecracker-containerd-e2etest-naive \
		--tag localhost/firecracker-containerd-e2etest-naive:${DOCKER_IMAGE_TAG} .

docker-images: | docker-image-e2etest-naive docker-image-unittest-nonroot docker-image-unittest

.PHONY: all $(SUBDIRS) clean proto deps lint install docker-images docker-image-e2etest-naive docker-image-unittest-nonroot docker-image-unittest runc-builder runc
