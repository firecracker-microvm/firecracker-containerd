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

EXTRAGOARGS?=

GOMOD := $(shell go env GOMOD)
GOSUM := $(GOMOD:.mod=.sum)
SOURCES:=$(shell find . -name '*.go')
DOCKER_IMAGE_TAG?=latest
REVISION := $(shell git rev-parse HEAD)

all: agent

agent: *.go $(GOMOD) $(GOSUM)
ifneq ($(STATIC_AGENT),)
	CGO_ENABLED=0 go build -installsuffix cgo -a -ldflags "-s -X main.revision=$(REVISION)" $(EXTRAGOARGS) -o agent
else
	go build $(EXTRAGOARGS) -ldflags "-X main.revision=$(REVISION)" -o agent
endif

test:
	go test ./... $(EXTRAGOARGS)

# integ tests involving agent are currently only in runtime package
integ-test:

clean:
	- rm -f agent

distclean: clean

# Install is a noop here, for now.
install:

.PHONY: clean distclean all install test integ-test
