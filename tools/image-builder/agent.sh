#! /bin/bash
# This script adds multiple failure modes to firecracker-containerd's in-VM agent.
set -euo pipefail

agent=/usr/local/bin/agent.real

if [[ "$(cat /proc/cmdline)" =~ failure=([a-z-]+) ]];
then
    failure="${BASH_REMATCH[1]}"
else
    failure='none'
fi

case "$failure" in 
slow-boot)
    sleep 30
    $agent
    ;;
slow-reboot)
    $agent
    sleep 30
    ;;
no-agent)
    exit 1
    ;;
none)
    $agent
    ;;
*)
    echo "unknown failure mode: $failure"
    exit 1
esac

