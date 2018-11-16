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

all: agent runtime snapshotter

.PHONY: agent
agent:
	$(MAKE) -C agent

.PHONY: runtime
runtime:
	$(MAKE) -C runtime

.PHONY: snapshotter
snapshotter:
	$(MAKE) -C snapshotter

.PHONY: clean
clean:
	$(MAKE) -C agent clean
	$(MAKE) -C runtime clean
	$(MAKE) -C snapshotter clean