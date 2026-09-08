#!/usr/bin/env bash
set -euo pipefail

: "${HOME:?HOME is required}"
export NPU_BURN_OUTPUT_DIR=${NPU_BURN_OUTPUT_DIR:-$HOME/.ascend_npu_burn/output}
export NPU_BURN_LOG_DIR=${NPU_BURN_LOG_DIR:-$HOME/.ascend_npu_burn/log}
export XDG_CACHE_HOME=${XDG_CACHE_HOME:-$HOME/.cache}
export ASCEND_PROCESS_LOG_PATH=${ASCEND_PROCESS_LOG_PATH:-$NPU_BURN_LOG_DIR/ascend}

for runtime_dir in \
    "$NPU_BURN_OUTPUT_DIR" \
    "$NPU_BURN_LOG_DIR" \
    "$ASCEND_PROCESS_LOG_PATH" \
    "$HOME/.tvm_test_data" \
    "$XDG_CACHE_HOME"; do
    case "$runtime_dir" in
        /*) ;;
        *) printf 'ERROR: NPU runtime directory must be absolute: %s\n' "$runtime_dir" >&2; exit 1 ;;
    esac
    install -d -m 0750 "$runtime_dir"
done

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
