#!/bin/bash
source .buildkite/al2env.sh

sudo rm -rf $dir
./tools/thinpool.sh remove $unique_id
