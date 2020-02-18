#!/bin/bash

shim_base=/tmp/shim-base
unique_id=$BUILDKITE_BUILD_NUMBER
dir=$shim_base/$unique_id
bin_path=$dir/bin
devmapper_path=$dir/devmapper
state_path=$dir/state
runtime_config_path=$dir/firecracker-runtime.json
