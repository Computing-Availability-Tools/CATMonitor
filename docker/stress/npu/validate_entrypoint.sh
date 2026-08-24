#!/usr/bin/env bash
set -euo pipefail

entrypoint=${1-}
if [ "$#" -ne 1 ] || [ -z "$entrypoint" ]; then
    printf 'ERROR: exactly one entrypoint path is required\n' >&2
    exit 2
fi
if [ ! -f "$entrypoint" ]; then
    printf 'ERROR: NPU Burn entrypoint is not a regular file: %s\n' "$entrypoint" >&2
    exit 1
fi
if [ ! -s "$entrypoint" ]; then
    printf 'ERROR: NPU Burn entrypoint is empty: %s\n' "$entrypoint" >&2
    exit 1
fi

mode=$(stat -c '%a' -- "$entrypoint") || {
    printf 'ERROR: cannot read NPU Burn entrypoint mode: %s\n' "$entrypoint" >&2
    exit 1
}
case "$mode" in
    ""|*[!0-7]*)
        printf 'ERROR: invalid NPU Burn entrypoint mode %s: %s\n' "$mode" "$entrypoint" >&2
        exit 1
        ;;
esac
mode_value=$((8#$mode))
if (( (mode_value & 0111) == 0 )); then
    printf 'ERROR: NPU Burn entrypoint has no execute mode bit: %s (mode %s)\n' \
        "$entrypoint" "$mode" >&2
    exit 1
fi
