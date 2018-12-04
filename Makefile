# Copyright 2018 Amazon.com, Inc. or its affiliates. All Rights Reserved.
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

lint:
	golangci-lint run

.PHONY: all $(SUBDIRS) clean proto deps lint
