#!/bin/bash

source ./.buildkite/al2env.sh

export PATH=$bin_path:$PATH
$bin_path/firecracker-containerd --config $dir/config.toml &
containerd_pid=$!
$bin_path/firecracker-ctr --address $dir/containerd.sock content fetch docker.io/library/alpine:3.10.1
sudo -E env "$PATH=$PATH" /usr/local/bin/go test -run TestMultipleVMs_Isolated ./...

# cleanup
sudo kill -9 $containerd_pid
sudo rm -rf $dir
thinpool remove $uuid
