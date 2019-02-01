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

all: agent

agent: *.go
ifneq ($(STATIC_AGENT),)
	CGO_ENABLED=0 go build -installsuffix cgo -a -ldflags "-s" -o agent
else
	go build -o agent
endif

clean:
	- rm -f agent

# Install is a noop here, for now.
install:

.PHONY: clean all install
