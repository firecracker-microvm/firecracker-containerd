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
	proto/generate.sh

clean:
	for d in $(SUBDIRS); do $(MAKE) -C $$d clean; done
	# --force is used to squash errors that pertain to file not existing
	rm --force sandbox-test-cri-build-stamp

deps:
	curl -sfL https://install.goreleaser.com/github.com/golangci/golangci-lint.sh | sh -s -- -b $(GOPATH)/bin v1.12.3
	GO111MODULE=off go get -u github.com/vbatts/git-validation
	GO111MODULE=off go get -u github.com/kunalkushwaha/ltag

lint:
	ltag -t ./.headers -check -v
	git-validation -run DCO,dangling-whitespace,short-subject -range HEAD~20..HEAD
	golangci-lint run

install:
	for d in $(SUBDIRS); do $(MAKE) -C $$d install; done

sandbox-test-cri-build: sandbox-test-cri-build-stamp

sandbox-test-cri-build-stamp:
	docker build -f sandbox/cri/Dockerfile -t "localhost/sandbox-test-cri" .
	@touch sandbox-test-cri-build-stamp

sandbox-test-cri-run: sandbox-test-cri-build
	test "$(shell id --user)" -eq 0
	docker run \
		--init \
		--rm \
		--privileged \
		-v /tmp:/foo \
		--security-opt seccomp=unconfined \
		--ulimit core=0 \
		localhost/sandbox-test-cri

sandbox-test-cri: sandbox-test-cri-run

.PHONY: all $(SUBDIRS) clean proto deps lint install sandbox-test-cri-run sandbox-test-cri-build sandbox-test-cri
