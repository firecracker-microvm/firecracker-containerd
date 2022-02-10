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
EXTRAGOARGS:=

SOURCES := $(shell find ../../../config ../../../internal ../../../proto ../../ -name '*.go')
GOMOD := $(shell go env GOMOD)
GOSUM := $(GOMOD:.mod=.sum)
REVISION=$(shell git rev-parse HEAD)
VERSION_LDFLAGS="-X github.com/containerd/containerd/version.Revision=$(REVISION)"

all: build

build: firecracker-containerd firecracker-ctr

firecracker-containerd: $(SOURCES) $(GOMOD) $(GOSUM)
	go build $(EXTRAGOARGS) \
		-ldflags $(VERSION_LDFLAGS) -o firecracker-containerd

firecracker-ctr: $(GOMOD) $(GOSUM)
	GOBIN=$(CURDIR) go install $(EXTRAGOARGS) \
		-ldflags $(VERSION_LDFLAGS) github.com/containerd/containerd/cmd/ctr
	mv ctr firecracker-ctr

install: firecracker-containerd firecracker-ctr
	install -D -o root -g root -m755 -t $(INSTALLROOT)/bin firecracker-containerd
	install -D -o root -g root -m755 -t $(INSTALLROOT)/bin firecracker-ctr

test:
	go test ./... $(EXTRAGOARGS)

# integ tests involving containerd are currently only in runtime package
integ-test:

clean:
	- rm -f firecracker-containerd
	- rm -f firecracker-ctr

distclean: clean

.PHONY: all build install test integ-test clean distclean
