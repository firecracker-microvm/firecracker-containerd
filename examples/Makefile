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

# Set this to pass additional commandline flags to the go compiler, e.g. "make test EXTRAGOARGS=-v"
EXTRAGOARGS?=

SOURCES:=$(shell find . -name '*.go')
GOMOD := $(shell go env GOMOD)
GOSUM := $(GOMOD:.mod=.sum)
DOCKER_IMAGE_TAG?=latest
FIRECRACKER_CONTAINERD_TEST_IMAGE?=localhost/firecracker-containerd-test

all: examples

examples: taskworkflow remote-snapshotter

taskworkflow: taskworkflow.go $(GOMOD) $(GOSUM)
	go build -o taskworkflow taskworkflow.go

remote-snapshotter:
	make -C cmd/remote-snapshotter $@

test:
	go test ./... $(EXTRAGOARGS)

integ-test:
	mkdir -p $(CURDIR)/logs
	$(CURDIR)/../tools/thinpool.sh reset "$(TEST_POOL)"
	docker run --rm -it \
		--privileged \
		--ipc=host \
		--volume /dev:/dev \
		--volume /run/udev/control:/run/udev/control \
		--volume $(CURDIR)/etc/containerd/firecracker-runtime.json:/etc/containerd/firecracker-runtime.json \
		--volume $(CURDIR)/logs:/var/log/firecracker-containerd-test \
		--volume $(CURDIR)/..:/src \
		--env FICD_DM_VOLUME_GROUP=$(FICD_DM_VOLUME_GROUP) \
		--env FICD_DM_POOL=$(TEST_POOL) \
		--env EXTRAGOARGS="${EXTRAGOARGS}" \
		--workdir="/src/examples" \
		$(FIRECRACKER_CONTAINERD_TEST_IMAGE):${DOCKER_IMAGE_TAG} \
		"make examples && make testtap && sleep 3 && ./taskworkflow -ip $(TEST_IP)$(TEST_SUBNET) -gw $(TEST_GATEWAY) -ss devmapper"

TEST_GATEWAY?=172.16.0.1
TEST_IP?=172.16.0.2
TEST_SUBNET?=/24
FICD_DM_VOLUME_GROUP?=
TEST_POOL?=fc-test-thinpool
testtap:
	ip link add br0 type bridge
	ip tuntap add tap0 mode tap
	ip link set tap0 master br0
	ip link set dev tap0 up
	ip link set dev br0 up
	ip addr add dev br0 $(TEST_GATEWAY)$(TEST_SUBNET)

clean:
	- rm -f taskworkflow
	- make -C cmd/remote-snapshotter $@

distclean: clean
	- rm -rf logs

install:

.PHONY: all examples clean distclean install test integ-test testtap remote-snapshotter
