#!/usr/bin/env bash
# Read-only fixed-container preflight for a slim NPU Burn runtime image.

set -euo pipefail

ASCEND_ENV_HELPER=${CATMONITOR_ASCEND_ENV_HELPER:-/usr/local/libexec/catmonitor/ascend_env.sh}
[ -f "$ASCEND_ENV_HELPER" ] || {
    printf 'ERROR: Ascend environment helper is missing: %s\n' "$ASCEND_ENV_HELPER" >&2
    exit 1
}
# shellcheck disable=SC1090
source "$ASCEND_ENV_HELPER"
catmonitor_source_ascend_env

lspci_path=$(command -v lspci) || {
    printf 'ERROR: lspci is unavailable in the NPU Burn runtime\n' >&2
    exit 1
}
topology_count=$(
    "$lspci_path" -D -d 19e5: 2>/dev/null |
        grep -ci 'Processing accelerators' || true
)
[ "$topology_count" -gt 0 ] || {
    printf 'ERROR: no Ascend Processing accelerators were found by lspci\n' >&2
    exit 1
}

python3 - <<'PY'
import ctypes
import importlib

ctypes.CDLL("libascend_hal.so")
for name in (
    "torch",
    "torch_npu",
    "ascend_npu_burn",
    "ascend_npu_burn.custom_ops.custom_ops_lib",
):
    module = importlib.import_module(name)
    version = getattr(module, "__version__", "unknown")
    print(f"CATMONITOR_RUNTIME_IMPORT_{name.upper().replace('.', '_')}={version}")
PY

printf 'CATMONITOR_RUNTIME_CANN_VERSION=%s\n' "$CATMONITOR_CANN_VERSION"
printf 'CATMONITOR_RUNTIME_TOPOLOGY_DEVICES=%s\n' "$topology_count"
printf 'CATMONITOR_RUNTIME_PREFLIGHT=PASS\n'
