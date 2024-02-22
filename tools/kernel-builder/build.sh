#!/bin/bash

set -euo pipefail

OUTPUT_DIR=$PWD

function get_linux_tarball {
     local KERNEL_VERSION=$1
     echo "Downloading the latest patch version for v$KERNEL_VERSION..."
     local major_version="${KERNEL_VERSION%%.*}"
     local url_base="https://cdn.kernel.org/pub/linux/kernel"
     local LATEST_VERSION=$(
         curl -fsSL $url_base/v$major_version.x/ \
         | grep -o "linux-$KERNEL_VERSION\.[0-9]*\.tar.xz" \
         | sort -rV \
         | head -n 1 || true)
     # Fetch tarball and sha256 checksum.
     curl -fsSLO "$url_base/v$major_version.x/sha256sums.asc"
     curl -fsSLO "$url_base/v$major_version.x/$LATEST_VERSION"
     # Verify checksum.
     grep "${LATEST_VERSION}" sha256sums.asc | sha256sum -c -
     echo "Extracting the kernel source..."
     tar -xaf $LATEST_VERSION
     local DIR=$(basename $LATEST_VERSION .tar.xz)
     ln -svfT $DIR linux
 }

function cleanup {
	rm sha256sums.asc
	rm -r linux*
}

function build_linux {
     local KERNEL_CFG=$1
     # Extract the kernel version from the config file provided as parameter.
     local KERNEL_VERSION=$(grep -Po "^# Linux\/\w+ \K(\d+\.\d+)" "$KERNEL_CFG")

     get_linux_tarball $KERNEL_VERSION
     pushd linux

     arch=$(uname -m)
     if [ "$arch" = "x86_64" ]; then
         format="elf"
         target="vmlinux"
         binary_path="$target"
     elif [ "$arch" = "aarch64" ]; then
         format="pe"
         target="Image"
         binary_path="arch/arm64/boot/$target"
     else
         echo "FATAL: Unsupported architecture!"
         exit 1
     fi
     cp "$KERNEL_CFG" .config

     make olddefconfig
     make -j $(nproc) $target
     LATEST_VERSION=$(cat include/config/kernel.release)
     OUTPUT_FILE=$OUTPUT_DIR/vmlinux-$KERNEL_VERSION-$arch.bin
     cp -v $binary_path $OUTPUT_FILE
     cp -v .config $OUTPUT_FILE.config
     popd &>/dev/null
     cleanup
 }

build_linux $1

