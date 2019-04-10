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

GOPATH:=$(shell go env GOPATH)

all: $(SUBDIRS)

$(SUBDIRS):
	$(MAKE) -C $@

proto:
	$(MAKE) -C proto/ proto

clean:
	for d in $(SUBDIRS); do $(MAKE) -C $$d clean; done
	- rm -rf bin/

GOLANGCI:=./bin/golangci-lint

lint:
	ltag -t ./.headers -check -v
	git-validation -run DCO,dangling-whitespace,short-subject -range HEAD~20..HEAD
	$(GOLANGCI) run

deps:
	curl -sfL https://raw.githubusercontent.com/golangci/golangci-lint/master/install.sh| sh -s -- -b ./bin v1.16.0
	$(GOLANGCI) --version
	GO111MODULE=off go get -u github.com/vbatts/git-validation
	GO111MODULE=off go get -u github.com/kunalkushwaha/ltag

install:
	for d in $(SUBDIRS); do $(MAKE) -C $$d install; done

.PHONY: all $(SUBDIRS) clean proto deps lint install
