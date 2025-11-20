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
        # Find and remove individual snapshots with retry logic
        for snapshot in $(sudo dmsetup ls | grep "^$(basename ${dm_device})-snap-" | awk '{print $1}' | sort -r); do
            echo "Attempting to remove snapshot: $snapshot"
            local retries=5
            while [ $retries -gt 0 ]; do
                if sudo dmsetup remove "$snapshot" 2>/dev/null; then
                    echo "Successfully removed snapshot: $snapshot"
                    break
                else
                    echo "Snapshot $snapshot busy, waiting... (retries left: $retries)"
                    sleep 2
                    retries=$((retries - 1))
                fi
            done
            
            if [ $retries -eq 0 ]; then
                echo "Warning: Failed to remove snapshot $snapshot after retries"
                # Force remove by checking what's using it
                sudo dmsetup info "$snapshot" 2>/dev/null || true
                sudo lsof "$snapshot" 2>/dev/null || true
                sudo dmsetup remove --force "$snapshot" || true
            fi
        done

        # Remove the thin pool with retries
        local pool_retries=5
        while [ $pool_retries -gt 0 ]; do
            if sudo dmsetup remove "${dm_device}" 2>/dev/null; then
                echo "Successfully removed thin pool: ${dm_device}"
                break
            else
                echo "Thin pool ${dm_device} busy, waiting... (retries left: $pool_retries)"
                sleep 2
                pool_retries=$((pool_retries - 1))
            fi
        done
        
        if [ $pool_retries -eq 0 ]; then
            echo "Warning: Failed to remove thin pool after retries"
            sudo dmsetup info "${dm_device}" 2>/dev/null || true
            sudo lsof "${dm_device}" 2>/dev/null || true
        fi

        # Clean up metadata and data devices
        sudo dmsetup remove "${dm_device}_tdata" 2>/dev/null || true
        sudo dmsetup remove "${dm_device}_tmeta" 2>/dev/null || true
        
        # Finally remove the logical volume
        sudo lvremove -f "$dm_device" 2>/dev/null || true
    }

    pool_reset() {
        if [ -e "${dm_device}" ]; then
            # Wait for containerd to finish any pending cleanup operations
            echo "Waiting for containerd to finish cleanup operations..."
            
            # Use containerd client to clean up any remaining snapshots
            if command -v ctr >/dev/null 2>&1; then
                echo "Cleaning up containerd snapshots..."
                # List and remove any active snapshots using containerd API
                for snap in $(ctr --address /run/firecracker-containerd/containerd.sock snapshots list 2>/dev/null | tail -n +2 | awk '{print $1}' || true); do
                    if [ -n "$snap" ] && [ "$snap" != "KEY" ]; then
                        echo "Removing containerd snapshot: $snap"
                        ctr --address /run/firecracker-containerd/containerd.sock snapshots remove "$snap" 2>/dev/null || true
                    fi
                done
                
                # Give containerd time to release device references
                sleep 2
            fi
            
            # Sync filesystem and drop caches to help release any remaining references
            sync
            sudo bash -c 'echo 1 > /proc/sys/vm/drop_caches' 2>/dev/null || true
            
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
