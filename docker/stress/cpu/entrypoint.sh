#!/usr/bin/env bash

set -euo pipefail

WORKLOAD_UID=65532
WORKLOAD_GID=65532
STATE_ROOT=${CATMONITOR_STRESS_STATE_ROOT:-/var/lib/catmonitor/stress}

[ "$#" -gt 0 ] || { echo "ERROR: CPU workload command is empty" >&2; exit 1; }
install -d -o "$WORKLOAD_UID" -g "$WORKLOAD_GID" -m 0750 \
    "$STATE_ROOT" \
    "$STATE_ROOT/work" \
    "$STATE_ROOT/work/hpl" \
    "$STATE_ROOT/work/hpcg"

# HPL and HPCG write inputs and result files in their current directories.
# Immutable image assets are copied only when the writable profile is absent.
if [ ! -f "$STATE_ROOT/work/hpl/HPL.dat" ]; then
    install -o "$WORKLOAD_UID" -g "$WORKLOAD_GID" -m 0640 \
        /opt/catmonitor/stress/runtime/hpl/HPL.dat \
        "$STATE_ROOT/work/hpl/HPL.dat"
fi
if [ ! -f "$STATE_ROOT/work/hpcg/hpcg.dat" ]; then
    install -o "$WORKLOAD_UID" -g "$WORKLOAD_GID" -m 0640 \
        /opt/catmonitor/stress/runtime/hpcg/hpcg.dat \
        "$STATE_ROOT/work/hpcg/hpcg.dat"
fi

if [ "$1" = "__serve__" ]; then
    set -- sleep infinity
fi

exec setpriv \
    --bounding-set=-all \
    --inh-caps=-all \
    --ambient-caps=-all \
    --reuid="$WORKLOAD_UID" \
    --regid="$WORKLOAD_GID" \
    --init-groups \
    --no-new-privs \
    "$@"
