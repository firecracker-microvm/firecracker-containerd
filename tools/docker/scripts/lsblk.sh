#! /bin/sh
# tiny lsblk(8) equivalent to make integration tests distro-agnostic
set -eu

echo 'NAME MAJ:MIN RM      SIZE RO | MAGIC'

for name in $(ls /sys/block)
do
    # Ignore loop devices
    case "$name" in
	loop*)
	    continue
	    ;;
    esac

    # The size entry returns the number of sectors,
    # not the number of bytes.
    # https://unix.stackexchange.com/a/301403
    bytes=$(($(cat /sys/block/$name/size) * 512))

    # Show the first 8 bytes, where our stub device files have own signatures
    # https://github.com/firecracker-microvm/firecracker-containerd/blob/2578f3df9d899aa48decb39c9f7f23fa41635ede/internal/common.go#L67
    magic=$(head -c 8 /dev/$name | od -A n -t u1)

    printf "%-4s %-7s %2d %10dB %2d | %s\n" \
	   "$name" \
	   $(cat /sys/block/$name/dev) \
	   $(cat /sys/block/$name/removable) \
	   "$bytes" \
	   $(cat /sys/block/$name/ro) \
	   "$magic"
done
