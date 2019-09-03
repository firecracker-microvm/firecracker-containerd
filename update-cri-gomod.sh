#!/bin/sh
# Copied from https://github.com/kubernetes-sigs/aws-ebs-csi-driver/tree/v0.4.0/hack, which is based on https://github.com/kubernetes/kubernetes/issues/79384#issuecomment-521493597

set -euo pipefail

VERSION=${1#"v"}
if [ -z "$VERSION" ]; then
    echo "Must specify version!"
    exit 1
fi
MODS=($(
    curl -sS https://raw.githubusercontent.com/kubernetes/kubernetes/v${VERSION}/go.mod |
    sed -n 's|.*k8s.io/\(.*\) => ./staging/src/k8s.io/.*|k8s.io/\1|p'
))
for MOD in "${MODS[@]}"; do
    echo $MOD
    V=$(
        go mod download -json "${MOD}@kubernetes-${VERSION}" |
        sed -n 's|.*"Version": "\(.*\)".*|\1|p'
    )
    go mod edit "-replace=${MOD}=${MOD}@${V}"
done
