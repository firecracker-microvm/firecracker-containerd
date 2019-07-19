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

all: examples

examples: taskworkflow

taskworkflow: taskworkflow.go $(GOMOD) $(GOSUM)
	go build -o taskworkflow taskworkflow.go

test:
	go test ./... $(EXTRAGOARGS)

integ-test:
	mkdir -p $(CURDIR)/logs
	docker run --rm -it \
		--privileged \
		--ipc=host \
		--volume /dev:/dev \
		--volume /sys:/sys \
		--volume /run/udev/control:/run/udev/control \
		--volume $(CURDIR)/logs:/var/log/firecracker-containerd-test \
		--env EXTRAGOARGS="${EXTRAGOARGS}" \
		--workdir="/firecracker-containerd/examples" \
		localhost/firecracker-containerd-naive-integ-test:${DOCKER_IMAGE_TAG} \
		"make examples && ./taskworkflow"

clean:
	- rm -f taskworkflow

install:

.PHONY: all examples clean install test integ-test
