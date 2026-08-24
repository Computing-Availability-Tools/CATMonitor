#!/usr/bin/env bash

set -euo pipefail

RUNNER_UID=65532
RUNNER_GID=65532
STATE_ROOT=/var/lib/catmonitor/stress
SOCKET_ROOT=/run/catmonitor-stress
ADAPTER=/etc/catmonitor-stress/benchmark_check.sh

[ "$#" -gt 0 ] || { echo "ERROR: CPU runner command is empty" >&2; exit 1; }
[ -x "$ADAPTER" ] || {
    echo "ERROR: runner-local adapter is unavailable or not executable: $ADAPTER" >&2
    exit 1
}

install -d -o "$RUNNER_UID" -g "$RUNNER_GID" -m 0750 \
    "$STATE_ROOT" \
    "$STATE_ROOT/work" \
    "$STATE_ROOT/work/hpl" \
    "$STATE_ROOT/work/hpcg"
install -d -o "$RUNNER_UID" -g "$RUNNER_GID" -m 0750 "$SOCKET_ROOT"

# HPL and HPCG write result files in their current working directories. Keep
# immutable benchmark assets in the image and initialize only the writable
# per-node inputs under the shared state volume.
install -o "$RUNNER_UID" -g "$RUNNER_GID" -m 0640 \
    /opt/catmonitor/stress/runtime/hpl/HPL.dat \
    "$STATE_ROOT/work/hpl/HPL.dat"
install -o "$RUNNER_UID" -g "$RUNNER_GID" -m 0640 \
    /opt/catmonitor/stress/runtime/hpcg/hpcg.dat \
    "$STATE_ROOT/work/hpcg/hpcg.dat"

exec setpriv \
    --bounding-set=-all \
    --inh-caps=-all \
    --ambient-caps=-all \
    --reuid="$RUNNER_UID" \
    --regid="$RUNNER_GID" \
    --init-groups \
    --no-new-privs \
    "$@"
