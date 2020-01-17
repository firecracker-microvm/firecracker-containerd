#!/bin/bash

shim_base=/tmp/shim-base
uuid=$BUILDKITE_BUILD_NUMBER
dir=$shim_base/$uuid
bin_path=$dir/bin
devmapper_path=$dir/devmapper
state_path=$dir/state
runtime_config_path=$dir/firecracker-runtime.json
firecracker_bin=firecracker-v0.19.0
