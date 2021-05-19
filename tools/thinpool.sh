#! /bin/bash
#
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

FICD_DM_VOLUME_GROUP="$FICD_DM_VOLUME_GROUP"

# The tmp/devmapper/ directory will be created on this project's
# root directory.
DIR=$(dirname $BASH_SOURCE)/../tmp/devmapper

set -euo pipefail

subcommand="$1"
name="$2"

if [ -z "$name" ]; then
    exit 0
fi

create_loopback_device() {
    local path=$1
    local size=$2

    if [[ ! -f "$path" ]]; then
        touch "$path"
        truncate -s "$size" "$path"
    fi

    local dev=$(sudo losetup --output NAME --noheadings --associated "$path")
    if [[ -z "$dev" ]]; then
        dev=$(sudo losetup --find --show $path)
    fi
    echo $dev
}

if [[ -z "$FICD_DM_VOLUME_GROUP" ]]; then
    pool_create() {
        mkdir -p $DIR

        local datadev=$(create_loopback_device "$DIR/data" '10G')
        local metadev=$(create_loopback_device "$DIR/metadata" '1G')

        local sectorsize=512
        local datasize="$(sudo blockdev --getsize64 -q ${datadev})"
        local length_sectors=$(bc <<< "${datasize}/${sectorsize}")
        local thinp_table="0 ${length_sectors} thin-pool ${metadev} ${datadev} 128 32768 1 skip_block_zeroing"
        sudo dmsetup create "$name" --table "${thinp_table}"
    }

    pool_remove() {
        for snapshot in $(sudo dmsetup ls | awk "/^$name-snap-/ { print \$1 }"); do
            sudo dmsetup remove $snapshot
        done

        local dev_no=1
        while true; do
            sudo dmsetup message "$name" 0 "delete $dev_no" || break
            dev_no=$(($dev_no + 1))
        done

        sudo dmsetup remove "$name"
    }

    pool_reset() {
        if sudo dmsetup info "$name"; then
            pool_remove
        fi
        pool_create
    }
else
    dm_device="/dev/mapper/$(echo ${FICD_DM_VOLUME_GROUP} | sed -e s/-/--/g)-$name"

    pool_create() {
        echo sudo lvcreate --type thin-pool \
             --poolmetadatasize 16MiB \
             --size 1GiB \
             -n "$name" "$FICD_DM_VOLUME_GROUP"
        sudo lvcreate --type thin-pool \
             --poolmetadatasize 16MiB \
             --size 1GiB \
             -n "$name" "$FICD_DM_VOLUME_GROUP"
    }

    pool_remove() {
        sudo dmsetup remove "${dm_device}-snap-"* || true
        sudo dmsetup remove \
         "${dm_device}" \
         "${dm_device}_tdata" "${dm_device}_tmeta" || true
        sudo lvremove -f "$dm_device"
    }

    pool_reset() {
        if [ -e "${dm_device}" ]; then
            pool_remove
        fi
        pool_create
    }
fi

case "$subcommand" in
    'create')
        pool_create
        ;;
    'remove')
        pool_remove
        ;;
    'reset')
        pool_reset
        ;;
    *)
        echo "This script doesn't support $subcommand"
        exit 1
esac
