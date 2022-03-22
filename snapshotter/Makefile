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

SOURCES := $(shell find . -name '*.go')
GOMOD := $(shell go env GOMOD)
GOSUM := $(GOMOD:.mod=.sum)

REVISION := $(shell git rev-parse HEAD)

all: demux-snapshotter

demux-snapshotter: $(SOURCES) $(GOMOD) $(GOSUM)
	go build $(EXTRAGOARGS) -ldflags "-X main.revision=$(REVISION)" -o $@

http-address-resolver: $(SOURCES) $(GOMOD) $(GOSUM)
	go build $(EXTRAGOARGS) -ldflags "-X main.revision=$(REVISION)" -o $@ internal/http_address_resolver.go

test:
	go test ./... $(EXTRAGOARGS)

clean:
	- rm -f demux-snapshotter
	- rm -f http-resolver

distclean: clean

# Install is a noop here, for now.
install:

.PHONY: all install test clean distclean
