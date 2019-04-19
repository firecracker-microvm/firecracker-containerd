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

# Set this to pass additional commandline flags to the go compiler, e.g. "make test EXTRAGOARGS=-v"
EXTRAGOARGS?=

SOURCES:=$(shell find . -name '*.go')
GOMOD := $(shell go env GOMOD)
GOSUM := $(GOMOD:.mod=.sum)
DOCKER_IMAGE_TAG?=latest

all: runtime

runtime: containerd-shim-aws-firecracker

containerd-shim-aws-firecracker: $(SOURCES) $(GOMOD) $(GOSUM)
	go build -o containerd-shim-aws-firecracker

install: containerd-shim-aws-firecracker
	install -D -o root -g root -m755 -t $(INSTALLROOT)/bin containerd-shim-aws-firecracker

test: $(SOURCES)
	go test ./... $(EXTRAGOARGS)

test-docker-unit: $(SOURCES) $(GOMOD) $(GOSUM)
	docker run --rm \
		--privileged \
		--device=/dev/vhost-vsock \
		--env EXTRAGOARGS="${EXTRAGOARGS}" \
		--workdir="/firecracker-containerd/runtime" \
		localhost/firecracker-containerd-unittest:${DOCKER_IMAGE_TAG} \
		"make test"

test-docker-isolated: $(SOURCES) $(GOMOD) $(GOSUM)
	mkdir -p $(PWD)/logs
	FCCD_PACKAGE_DIR=runtime \
	FCCD_TESTNAME_REGEX=_Isolated \
	FCCD_DOCKER_IMAGE=localhost/firecracker-containerd-e2etest-naive:${DOCKER_IMAGE_TAG} \
	FCCD_DOCKER_RUN_ARGS="--privileged \
		--ipc=host \
		--volume /dev:/dev \
		--volume /sys:/sys \
		--volume /run/udev/control:/run/udev/control \
		--volume $(PWD)/logs:/var/log/firecracker-containerd-test \
		--env ENABLE_ISOLATED_TESTS=1 \
	" ../tools/run-tests-by-name-regex.sh

clean:
	- rm -f containerd-shim-aws-firecracker

.PHONY: all runtime clean install
