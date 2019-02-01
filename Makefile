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

SUBDIRS:=agent runtime snapshotter
export INSTALLROOT?=/usr/local
export STATIC_AGENT

all: $(SUBDIRS)

$(SUBDIRS):
	$(MAKE) -C $@

proto:
	proto/generate.sh

clean:
	for d in $(SUBDIRS); do $(MAKE) -C $$d clean; done

deps:
	test -n "$(GOPATH)" # Make sure GOPATH defined
	curl -sfL https://install.goreleaser.com/github.com/golangci/golangci-lint.sh | sh -s -- -b ${GOPATH}/bin v1.12.3
	GO111MODULE=off go get -u github.com/vbatts/git-validation
	GO111MODULE=off go get -u github.com/kunalkushwaha/ltag

lint:
	ltag -t ./.headers -check -v
	git-validation -run DCO,dangling-whitespace,short-subject -range dd78e4f0182dfff8ef33e5eb7dc925d5f6e84301..HEAD
	golangci-lint run

install:
	for d in $(SUBDIRS); do $(MAKE) -C $$d install; done

.PHONY: all $(SUBDIRS) clean proto deps lint install
