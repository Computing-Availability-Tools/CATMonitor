#!/usr/bin/env bash
set -euo pipefail
ROOT=$(cd "$(dirname "$0")/../../.." && pwd -P)
TEST_ROOT=$(mktemp -d)
trap 'rm -rf -- "$TEST_ROOT"' EXIT
cp -a "$ROOT/third_party/ascend_npu_burn/source/ascend_npu_burn" "$TEST_ROOT/"
# Windows checkouts can have CRLF; patch the temporary Linux source copy only.
find "$TEST_ROOT" -type f -name '*.py' -exec sed -i 's/\r$//' {} +
patch --batch --fuzz=0 -p1 -d "$TEST_ROOT" < "$ROOT/scripts/stress/patches/ascend_npu_burn/portable-runtime-v1.patch"
python3 -m compileall -q "$TEST_ROOT/ascend_npu_burn"
python3 "$ROOT/scripts/stress/tests/npu_portable_test.py" "$TEST_ROOT"
# Full-base A2 is an alternative to sizing, never stacked with it.
patch --batch --fuzz=0 -p1 -d "$TEST_ROOT" < "$ROOT/scripts/stress/patches/ascend_npu_burn/a2-upstream-base.patch"
python3 -m compileall -q "$TEST_ROOT/ascend_npu_burn"
python3 "$ROOT/scripts/stress/tests/npu_portable_test.py" "$TEST_ROOT"
cmp "$ROOT/third_party/ascend_npu_burn/source/ascend_npu_burn/config/base_config.json" "$TEST_ROOT/ascend_npu_burn/config/base_config.json"
cmp "$ROOT/third_party/ascend_npu_burn/source/ascend_npu_burn/config/a3_config.json" "$TEST_ROOT/ascend_npu_burn/config/a3_config.json"
patch --batch --fuzz=0 -R -p1 -d "$TEST_ROOT" < "$ROOT/scripts/stress/patches/ascend_npu_burn/a2-upstream-base.patch"
patch --batch --fuzz=0 -p1 -d "$TEST_ROOT" < "$ROOT/scripts/stress/patches/ascend_npu_burn/a2-portable-sizing.patch"
python3 -m compileall -q "$TEST_ROOT/ascend_npu_burn"
python3 "$ROOT/scripts/stress/tests/npu_portable_test.py" "$TEST_ROOT"
python3 - "$TEST_ROOT" <<'PY'
import ast, json, pathlib, sys
root=pathlib.Path(sys.argv[1])/'ascend_npu_burn'
config=json.loads((root/'config/base_config.json').read_text())
text=(root/'config/a2_config.json').read_text()
assert '"run_count": 20' in text and '"dtype": ["float16"]' in text
assert 'A2 = "A2"' in (root/'common/enum.py').read_text()
assert 'choices=["A2", "A3", "A5"]' in (root/'npu_burn.py').read_text()
assert 'data_load.data_size = 1 if args.chip_generation == "A2" else 5' in (root/'runtime/engine.py').read_text()
assert config['cases'][0]['matmul']['run_count'] == 100
assert 'runtime_topology' in (root/'common/utils.py').read_text()
print('A2_PORTABLE_PROFILE=PASS (synthetic; hardware NOT_RUN)')
PY
cmp "$ROOT/third_party/ascend_npu_burn/source/ascend_npu_burn/config/base_config.json" "$TEST_ROOT/ascend_npu_burn/config/base_config.json"
cmp "$ROOT/third_party/ascend_npu_burn/source/ascend_npu_burn/config/a3_config.json" "$TEST_ROOT/ascend_npu_burn/config/a3_config.json"
printf 'PUBLIC_UPSTREAM_PORTABLE_REGRESSION=PASS\n'
