#! /bin/sh
set -eu

subcommand="$1"
name="$2"

case "$subcommand" in
    'create')
        lvcreate --type thin-pool \
                 --poolmetadatasize 16GiB \
                 --extents '50%FREE' \
                 -n "$name" fcci-vg
        ;;
    'remove')
        dm_device="/dev/mapper/fcci--vg-$name"
        dmsetup remove \
                ${dm_device}-snap-* \
                ${dm_device} \
                ${dm_device}_tdata ${dm_device}_tmeta || true
        lvremove -f "$dm_device"
        ;;
    *)
        echo "This script doesn't support $subcommand"
        exit 1
esac
