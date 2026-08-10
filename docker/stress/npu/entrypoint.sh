#!/usr/bin/env bash
set -euo pipefail

for candidate in \
    /usr/local/Ascend/ascend-toolkit/set_env.sh \
    /usr/local/Ascend/ascend-toolkit/latest/bin/setenv.bash
do
    if [ -r "$candidate" ]; then
        # shellcheck disable=SC1090
        source "$candidate"
        break
    fi
done

if [ "${1-}" = "__serve__" ]; then
    exec sleep infinity
fi

exec npu-burn "$@"
