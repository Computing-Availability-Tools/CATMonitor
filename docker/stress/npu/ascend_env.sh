#!/usr/bin/env bash
# Shared Ascend/CANN environment discovery for NPU Burn image build and runtime.

catmonitor_find_ascend_env() {
    local root=${CATMONITOR_ASCEND_ENV_ROOT:-/usr/local/Ascend}
    local override=${ASCEND_ENV_SCRIPT:-}
    local canonical
    local candidate
    local -a discovered=()

    if [ -n "$override" ]; then
        case "$override" in
            /*) ;;
            *)
                printf 'ERROR: ASCEND_ENV_SCRIPT must be an absolute container path: %s\n' "$override" >&2
                return 1
                ;;
        esac
        if [ ! -f "$override" ]; then
            printf 'ERROR: explicit Ascend environment script is not a regular file: %s\n' "$override" >&2
            return 1
        fi
        printf '%s\n' "$override"
        return 0
    fi

    for canonical in \
        "$root/ascend-toolkit/set_env.sh" \
        "$root/ascend-toolkit/latest/bin/setenv.bash"
    do
        if [ -f "$canonical" ]; then
            printf '%s\n' "$canonical"
            return 0
        fi
    done

    shopt -s nullglob
    for candidate in "$root"/cann-*/set_env.sh; do
        if [ -f "$candidate" ]; then
            discovered+=("$candidate")
        fi
    done
    shopt -u nullglob

    if [ "${#discovered[@]}" -eq 1 ]; then
        printf '%s\n' "${discovered[0]}"
        return 0
    fi

    if [ "${#discovered[@]}" -gt 1 ]; then
        printf 'ERROR: multiple Ascend CANN environment scripts were found; set ASCEND_ENV_SCRIPT explicitly:\n' >&2
        printf '  %s\n' "${discovered[@]}" >&2
        return 1
    fi

    cat >&2 <<EOF
ERROR: no supported Ascend environment initialization script found

Checked:
  $root/ascend-toolkit/set_env.sh
  $root/ascend-toolkit/latest/bin/setenv.bash
  $root/cann-*/set_env.sh
EOF
    return 1
}

catmonitor_cann_version_from_env() {
    local selected=$1
    local version=unknown
    local install_info
    local detected

    # Canonical toolkit layouts often expose ASCEND_TOOLKIT_HOME as a `latest`
    # symlink. Prefer the installed package metadata so manifests and ABI checks
    # record 8.3.RC2/9.0.1 instead of the non-version identity `latest`.
    if [ -n "${ASCEND_TOOLKIT_HOME:-}" ]; then
        shopt -s nullglob
        for install_info in "$ASCEND_TOOLKIT_HOME"/*-linux/ascend_toolkit_install.info; do
            [ -f "$install_info" ] || continue
            detected=$(awk -F= '$1 == "version" { print $2; exit }' "$install_info")
            if [ -n "$detected" ]; then
                version=$detected
                break
            fi
        done
        shopt -u nullglob
        if [ "$version" != unknown ]; then
            printf '%s\n' "$version"
            return 0
        fi
    fi

    case "$selected" in
        */cann-*/set_env.sh)
            version=${selected%/set_env.sh}
            version=${version##*/}
            version=${version#cann-}
            ;;
        *)
            if [ -n "${ASCEND_TOOLKIT_HOME:-}" ]; then
                version=${ASCEND_TOOLKIT_HOME%/}
                version=${version##*/}
            fi
            ;;
    esac
    printf '%s\n' "$version"
}

catmonitor_source_ascend_env() {
    local selected
    local nounset_was_enabled=false
    local source_status=0

    selected=$(catmonitor_find_ascend_env) || return 1
    printf 'Using Ascend environment:\n%s\n' "$selected"

    # Vendor environment scripts commonly append an unset LD_LIBRARY_PATH or
    # PYTHONPATH. Keep CATMonitor strict mode, but do not impose nounset on a
    # script that is designed for a regular interactive Bash environment.
    case $- in
        *u*) nounset_was_enabled=true; set +u ;;
    esac
    # shellcheck disable=SC1090
    if source "$selected"; then
        source_status=0
    else
        source_status=$?
    fi
    if [ "$nounset_was_enabled" = true ]; then
        set -u
    fi
    if [ "$source_status" -ne 0 ]; then
        printf 'ERROR: failed to source Ascend environment script:\n%s\n' "$selected" >&2
        return "$source_status"
    fi

    export CATMONITOR_ASCEND_ENV_SCRIPT_SELECTED=$selected
    CATMONITOR_CANN_VERSION=$(catmonitor_cann_version_from_env "$selected")
    export CATMONITOR_CANN_VERSION

    printf 'CATMONITOR_ASCEND_ENV_SCRIPT=%s\n' "$CATMONITOR_ASCEND_ENV_SCRIPT_SELECTED"
    printf 'CATMONITOR_CANN_VERSION=%s\n' "$CATMONITOR_CANN_VERSION"
}

catmonitor_ascend_build_preflight() {
    local root=${CATMONITOR_ASCEND_ENV_ROOT:-/usr/local/Ascend}
    local driver_present=false
    if [ -d "$root/driver" ]; then
        driver_present=true
    fi
    printf 'CATMONITOR_DRIVER_MOUNT_PRESENT_AT_BUILD=%s\n' "$driver_present"

    python3 - "$CATMONITOR_ASCEND_ENV_SCRIPT_SELECTED" <<'PY'
import ctypes
import importlib
import sys

selected = sys.argv[1]


def fail(component, error):
    print("ERROR: Ascend build environment preflight failed", file=sys.stderr)
    print("", file=sys.stderr)
    print("selected_env_script:", file=sys.stderr)
    print(f"  {selected}", file=sys.stderr)
    print(f"{component}:", file=sys.stderr)
    print(f"  {error}", file=sys.stderr)
    raise SystemExit(1)


try:
    ctypes.CDLL("libascend_hal.so")
except Exception as error:
    fail("libascend_hal", f"unresolved: {error}")
print("CATMONITOR_PREFLIGHT_LIBASCEND_HAL=PASS")

for module_name, marker in (
    ("torch", "TORCH"),
    ("torch_npu", "TORCH_NPU"),
    ("tbe", "TBE"),
):
    try:
        module = importlib.import_module(module_name)
    except Exception as error:
        fail(module_name, f"failed to import: {error}")
    version = getattr(module, "__version__", "unknown")
    print(f"CATMONITOR_PREFLIGHT_{marker}=PASS")
    print(f"CATMONITOR_PREFLIGHT_{marker}_VERSION={version}")
PY
}
