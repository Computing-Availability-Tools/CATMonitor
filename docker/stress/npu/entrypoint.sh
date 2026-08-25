#!/usr/bin/env bash
set -euo pipefail

ASCEND_ENV_HELPER=${CATMONITOR_ASCEND_ENV_HELPER:-/usr/local/libexec/catmonitor/ascend_env.sh}
[ -f "$ASCEND_ENV_HELPER" ] || {
    printf 'ERROR: Ascend environment helper is missing: %s\n' "$ASCEND_ENV_HELPER" >&2
    exit 1
}
# shellcheck disable=SC1090
if source "$ASCEND_ENV_HELPER"; then
    :
else
    helper_status=$?
    printf 'ERROR: failed to source Ascend environment helper:\n%s\n' "$ASCEND_ENV_HELPER" >&2
    exit "$helper_status"
fi
catmonitor_source_ascend_env

if [ "${1-}" = "__serve__" ]; then
    exec sleep infinity
fi

exec npu-burn "$@"
