#!/bin/bash
set -eu

source ./.buildkite/al2env.sh

export PATH=$bin_path:$PATH
export FIRECRACKER_CONTAINERD_RUNTIME_CONFIG_PATH=$runtime_config_path
export ENABLE_ISOLATED_TESTS=true
export CONTAINERD_SOCKET=$dir/containerd.sock

export SHIM_BASE_DIR=$dir

mkdir -p runtime/logs

sudo -E PATH=$PATH \
     $bin_path/firecracker-containerd \
     --config $dir/config.toml &>> runtime/logs/containerd.out &
containerd_pid=$!

sudo $bin_path/firecracker-ctr --address $dir/containerd.sock content fetch docker.io/library/alpine:3.10.1

TAP_PREFIX=build$BUILDKITE_BUILD_NUMBER \
     sudo -E PATH=$bin_path:$PATH /usr/local/bin/go test -count=1 -run TestMultipleVMs_Isolated ./... -v

sudo kill $containerd_pid
