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

EXTRAGOARGS?=
NUMBER_OF_VMS?=

SOURCES := $(shell find . -name '*.go')
GOMOD := $(shell go env GOMOD)
GOSUM := $(GOMOD:.mod=.sum)
DOCKER_IMAGE_TAG?=latest
GO_CACHE_VOLUME_NAME?=gocache
FIRECRACKER_CONTAINERD_TEST_IMAGE?=localhost/firecracker-containerd-test
FICD_DM_POOL?=fc-test-thinpool

REVISION := $(shell git rev-parse HEAD)

INTEG_TEST_SUFFIX := _Isolated
INTEG_TESTNAMES=$(shell docker run --rm \
	--network=none \
	--volume $(CURDIR)/..:/src \
	--volume $(GO_CACHE_VOLUME_NAME):/go \
	--entrypoint=/bin/bash \
	--workdir=/src/snapshotter \
	$(FIRECRACKER_CONTAINERD_TEST_IMAGE):$(DOCKER_IMAGE_TAG) \
	-c "go test -list . | sed '$$d' | grep $(INTEG_TEST_SUFFIX)")

all: demux-snapshotter http-address-resolver

demux-snapshotter: $(SOURCES) $(GOMOD) $(GOSUM)
	go build $(EXTRAGOARGS) -ldflags "-X main.revision=$(REVISION)" -o $@

http-address-resolver: $(SOURCES) $(GOMOD) $(GOSUM)
	go build $(EXTRAGOARGS) -ldflags "-X main.revision=$(REVISION)" -o $@ internal/http_address_resolver.go

install: demux-snapshotter http-address-resolver
	install -D -o root -g root -m755 -t $(INSTALLROOT)/bin $^

test:
	go test ./... $(EXTRAGOARGS)

integ-test:
	$(MAKE) $(addprefix integ-test-,$(INTEG_TESTNAMES))

integ-test-%: logs
	$(CURDIR)/../tools/thinpool.sh reset "$(FICD_DM_POOL)"
	docker run --rm -it \
		--privileged \
		--ipc=host \
		--network=bridge \
		--volume /dev:/dev \
		--volume /run/udev/control:/run/udev/control \
		--volume $(CURDIR)/logs:/var/log/firecracker-containerd-test \
		--volume $(CURDIR)/..:/src \
		--volume $(GO_CACHE_VOLUME_NAME):/go \
		--env ENABLE_ISOLATED_TESTS=1 \
		--env GOPROXY=direct \
		--env GOSUMDB=off \
		--env GO111MODULE=on \
		--env NUMBER_OF_VMS=$(NUMBER_OF_VMS) \
		--env FICD_DM_VOLUME_GROUP=$(FICD_DM_VOLUME_GROUP) \
		--env FICD_DM_POOL=$(FICD_DM_POOL) \
		--workdir="/src/snapshotter" \
		--init \
		$(FIRECRACKER_CONTAINERD_TEST_IMAGE):$(DOCKER_IMAGE_TAG) \
		"go test $(EXTRAGOARGS) -run '^$(subst integ-test-,,$@)$$'"

logs:
	mkdir logs

clean:
	- rm -f demux-snapshotter
	- rm -f http-address-resolver

distclean: clean
	- rm -rf logs

.PHONY: all clean distclean install test integ-test
