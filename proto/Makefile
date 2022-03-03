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

PROTO_SRC := $(wildcard *.proto)
PROTO_GEN_SRC := $(PROTO_SRC:.proto=.pb.go)
DOCKER_IMAGE_TAG?=latest
UID?=$(shell id -u)
GID?=$(shell id -g)

$(PROTO_GEN_SRC): $(PROTO_SRC)
	protoc \
		--gogo_out=Mgoogle/protobuf/any.proto=github.com/gogo/protobuf/types,Mgoogle/protobuf/empty.proto=github.com/gogo/protobuf/types:. \
		$^

proto: $(PROTO_GEN_SRC)
	PROTOPATH=$(CURDIR) $(MAKE) -C service/fccontrol proto
	PROTOPATH=$(CURDIR) $(MAKE) -C service/drivemount proto
	PROTOPATH=$(CURDIR) $(MAKE) -C service/ioproxy proto

proto-docker:
	docker run --rm \
		-v $(CURDIR):/protobuf \
		--user $(UID):$(GID) \
		localhost/proto-builder:${DOCKER_IMAGE_TAG}

clean:
	- rm -f $(PROTO_GEN_SRC)
	- $(MAKE) -C service/fccontrol clean
	- $(MAKE) -C service/drivemount clean
	- $(MAKE) -C service/ioproxy clean

.PHONY: clean proto proto-docker
