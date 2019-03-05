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

GOMOD := $(shell go env GOMOD)
GOSUM := $(GOMOD:.mod=.sum)

all: devmapper_snapshotter

devmapper_snapshotter: *.go $(GOMOD) $(GOSUM)
	go build -o devmapper_snapshotter

install: devmapper_snapshotter
	install -D -o root -g root -m755 -t $(INSTALLROOT)/bin devmapper_snapshotter

clean:
	- rm -f devmapper_snapshotter

.PHONY: all clean install
