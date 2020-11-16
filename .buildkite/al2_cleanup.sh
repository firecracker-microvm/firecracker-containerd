#!/bin/bash
source .buildkite/al2env.sh

sudo rm -rf $dir
FICD_DM_VOLUME_GROUP=fcci-vg ./tools/thinpool.sh remove $unique_id
