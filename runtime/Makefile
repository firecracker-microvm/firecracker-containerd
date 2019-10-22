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

INTEG_TEST_SUFFIX := _Isolated
INTEG_TESTNAMES=$(shell docker run --rm \
		--workdir="/firecracker-containerd/runtime" \
		localhost/firecracker-containerd-test:$(DOCKER_IMAGE_TAG) \
		"go test -list . | sed '$$d' | grep $(INTEG_TEST_SUFFIX)")


all: runtime

runtime: containerd-shim-aws-firecracker

containerd-shim-aws-firecracker: $(SOURCES) $(GOMOD) $(GOSUM)
	go build -o containerd-shim-aws-firecracker

install: containerd-shim-aws-firecracker
	install -D -o root -g root -m755 -t $(INSTALLROOT)/bin containerd-shim-aws-firecracker

test:
	go test ./... $(EXTRAGOARGS)

integ-test:
	mkdir -p $(CURDIR)/logs

	$(foreach TESTNAME,$(INTEG_TESTNAMES),\
		docker run --rm -it \
			--privileged \
			--ipc=host \
			--network=none \
			--volume /dev:/dev \
			--volume /run/udev/control:/run/udev/control \
			--volume $(CURDIR)/logs:/var/log/firecracker-containerd-test \
			--env ENABLE_ISOLATED_TESTS=1 \
			--workdir="/firecracker-containerd/runtime" \
			--init \
			localhost/firecracker-containerd-integ-test:$(DOCKER_IMAGE_TAG) \
			"go test $(EXTRAGOARGS) -run \"^$(TESTNAME)$$\"" || exit 1; \
	)

PERF_TESTNAME?=TestCNIPlugin_Performance
PERF_VMCOUNT?=25
PERF_RUNTIME_SECONDS?=600
PERF_VM_MEMSIZE_MB?=1024
PERF_TARGET_BANDWIDTH?=1G

tc-redirect-tap-perf:
	$(MAKE) PERF_PLUGIN_NAME=tc-redirect-tap perf-test

test-bridged-tap-perf:
	$(MAKE) PERF_PLUGIN_NAME=test-bridged-tap perf-test

perf-test:
	$(if $(PERF_PLUGIN_NAME),true,$(error PERF_PLUGIN_NAME must be set for perf-test target))
	mkdir -p $(CURDIR)/logs

	docker run --rm -it \
		--privileged \
		--ipc=host \
		--network=none \
		--volume /dev:/dev \
		--volume /run/udev/control:/run/udev/control \
		--volume $(CURDIR)/logs:/var/log/firecracker-containerd-test \
		--env ENABLE_ISOLATED_TESTS=1 \
		--env PERF_PLUGIN_NAME=$(PERF_PLUGIN_NAME) \
		--env PERF_VMCOUNT=$(PERF_VMCOUNT) \
		--env PERF_RUNTIME_SECONDS=$(PERF_RUNTIME_SECONDS) \
		--env PERF_VM_MEMSIZE_MB=$(PERF_VM_MEMSIZE_MB) \
		--env PERF_TARGET_BANDWIDTH=$(PERF_TARGET_BANDWIDTH) \
		--workdir="/firecracker-containerd/runtime" \
		--init \
		localhost/firecracker-containerd-integ-test:$(DOCKER_IMAGE_TAG) \
		"go test -timeout 0 -v -count=1 -run \"^$(PERF_TESTNAME)$$\""

clean:
	- rm -f containerd-shim-aws-firecracker

distclean: clean
	- rm -rf logs

.PHONY: all runtime clean distclean install test integ-test
