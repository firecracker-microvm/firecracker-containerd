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

EXTRAGOARGS:=

GOMOD := $(shell go env GOMOD)
GOSUM := $(GOMOD:.mod=.sum)
SOURCES:=$(shell find . -name '*.go')
DOCKER_IMAGE_TAG?=latest

all:

test: $(SOURCES) $(GOMOD) $(GOSUM)
	go test ./... $(EXTRAGOARGS)

test-docker-unit: $(SOURCES) $(GOMOD) $(GOSUM)
	docker run --rm \
		--env EXTRAGOARGS="${EXTRAGOARGS}" \
		--workdir="/firecracker-containerd/internal/vm" \
		localhost/firecracker-containerd-unittest-nonroot:${DOCKER_IMAGE_TAG} \
		"make test"

clean:

install:

.PHONY: clean all install test
