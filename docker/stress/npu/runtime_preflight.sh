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
catmonitor_source_ascend_env >&2

if [ "${1-}" = --cases-json ]; then
    LD_PRELOAD='' python3 - <<'PY'
import hashlib, json, os, sys
sys.path.insert(0, '/opt/catmonitor/npuburn-python')
saved_stdout = os.dup(1)
os.dup2(2, 1)
import torch
import torch_npu
from ascend_npu_burn.common.const import DTYPE_MAPPING
from ascend_npu_burn.case_catalog import catalog, load_config, select_cases
generation = os.environ['NPU_BURN_CHIP_GENERATION']
items = catalog(generation, supported_dtypes=DTYPE_MAPPING.keys())
selection, error = [], ''
try:
    selection = select_cases(generation, os.getenv('NPU_BURN_RUN_CASE', ''),
                             os.getenv('NPU_BURN_GROUP', ''), supported_dtypes=DTYPE_MAPPING.keys())
except ValueError as exc:
    error = str(exc)
sys.stdout.flush()
config_hash = hashlib.sha256(json.dumps(load_config(generation), sort_keys=True).encode()).hexdigest()
# Keep native stdout redirected through interpreter teardown: CANN libraries
# may emit diagnostics from atexit/destructors after the JSON has been written.
with os.fdopen(saved_stdout, 'w') as protocol:
    print(json.dumps(dict(cases=items, selected=selection, selection_error=error, configuration_sha256=config_hash)), file=protocol)
PY
    exit $?
fi

if [ "${1-}" = --topology-json ]; then
    # The explicit portable profile supplies this module. No fallback to a
    # guessed PCI slot when a subset cannot be mapped.
    LD_PRELOAD='' python3 - <<'PY'
import json
import os
import sys
# Use the same image-owned overlay as the CLI/spawn launcher. Redirect import
# diagnostics (including native writes) while preserving a JSON-only stdout.
sys.path.insert(0, '/opt/catmonitor/npuburn-python')
saved_stdout = os.dup(1)
os.dup2(2, 1)
import torch
import torch_npu
from ascend_npu_burn.runtime_compat import runtime_topology
proof = runtime_topology()
sys.stdout.flush()
with os.fdopen(saved_stdout, 'w') as protocol:
    print(json.dumps(proof), file=protocol)
PY
    exit $?
fi

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
import os
import sys
sys.path.insert(0, '/opt/catmonitor/npuburn-python')

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

torch_npu = importlib.import_module("torch_npu")
device_count = torch_npu.npu.device_count()
expected = int(os.environ.get("CATMONITOR_NPU_DEVICE_COUNT", "0"))
if expected <= 0:
    raise RuntimeError("CATMONITOR_NPU_DEVICE_COUNT must be a positive integer")
if device_count != expected:
    raise RuntimeError(
        f"torch_npu device count {device_count} does not match expected mapped count {expected}"
    )
print(f"CATMONITOR_RUNTIME_DEVICE_COUNT={device_count}")
PY

printf 'CATMONITOR_RUNTIME_CANN_VERSION=%s\n' "$CATMONITOR_CANN_VERSION"
printf 'CATMONITOR_RUNTIME_TOPOLOGY_DEVICES=%s\n' "$topology_count"
printf 'CATMONITOR_RUNTIME_PREFLIGHT=PASS\n'
