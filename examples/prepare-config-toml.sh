#! /bin/sh
set -eu

config_toml="$(mktemp)"

snapshotter="$1"
pool_name="$2"

case "$snapshotter" in
    'naive')
        cat > "$config_toml" <<EOF
[proxy_plugins]
  [proxy_plugins.firecracker-naive]
    type = "snapshot"
    address = "/var/run/firecracker-containerd/naive-snapshotter.sock"
EOF
        ;;
    'devmapper')
        cat > "$config_toml" <<EOF
[plugins]
  [plugins.devmapper]
    pool_name = "fcci--vg-$pool_name"
    base_image_size = "128MB"
EOF
        ;;
esac

echo "$config_toml"
