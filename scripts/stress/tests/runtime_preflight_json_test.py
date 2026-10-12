"""Run the real shell/JSON protocol with synthetic vendor import/exit noise."""
import json
import os
from pathlib import Path
import subprocess
import tempfile
import unittest


PREFLIGHT = Path(__file__).resolve().parents[3] / "docker/stress/npu/runtime_preflight.sh"


class PreflightJSONTest(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory()
        self.addCleanup(self.temp.cleanup)
        root = Path(self.temp.name)
        helper = root / "env.sh"
        helper.write_text("catmonitor_source_ascend_env() { echo fixture-env; }\n")
        # sitecustomize injects only test modules; no torch/NPU installation or
        # device access is required, and the production shell is left unchanged.
        (root / "sitecustomize.py").write_text('''
import atexit, os, sys, types
atexit.register(lambda: os.write(1, b"native teardown diagnostic\\n"))
class Vendor(types.ModuleType):
    def __getattribute__(self, key):
        return super().__getattribute__(key)
for name in ["torch", "torch_npu", "ascend_npu_burn", "ascend_npu_burn.common",
             "ascend_npu_burn.common.const", "ascend_npu_burn.case_catalog",
             "ascend_npu_burn.runtime_compat"]:
    sys.modules[name] = Vendor(name)
sys.modules["ascend_npu_burn.common.const"].DTYPE_MAPPING = {"float16": 1}
catalog = sys.modules["ascend_npu_burn.case_catalog"]
catalog.catalog = lambda *a, **kw: [{"name": "matmul", "selectable": True}]
catalog.select_cases = lambda *a, **kw: ["matmul"]
catalog.load_config = lambda *a: {"fixture": True}
def topology():
    os.write(1, b"native runtime diagnostic\\n")
    if os.getenv("FAIL_TOPOLOGY"):
        raise RuntimeError("fixture topology failure")
    return {"logical_ids": [0, 1]}
sys.modules["ascend_npu_burn.runtime_compat"].runtime_topology = topology
''')
        self.env = dict(os.environ, PYTHONPATH=str(root),
                        CATMONITOR_ASCEND_ENV_HELPER=str(helper),
                        NPU_BURN_CHIP_GENERATION="A2")

    def test_json_stays_clean_during_native_teardown(self):
        for mode, field in [("--cases-json", "selected"), ("--topology-json", "logical_ids")]:
            with self.subTest(mode=mode):
                result = subprocess.run(["bash", str(PREFLIGHT), mode], env=self.env,
                                        capture_output=True, text=True, timeout=15)
                self.assertEqual(result.returncode, 0, result.stderr)
                self.assertIn(field, json.loads(result.stdout))
                self.assertIn("native teardown diagnostic", result.stderr)
                self.assertIn("fixture-env", result.stderr)

    def test_failure_is_nonzero_without_success_json(self):
        result = subprocess.run(["bash", str(PREFLIGHT), "--topology-json"],
                                env=dict(self.env, FAIL_TOPOLOGY="1"),
                                capture_output=True, text=True, timeout=15)
        self.assertNotEqual(result.returncode, 0)
        self.assertEqual(result.stdout, "")
        self.assertIn("fixture topology failure", result.stderr)


if __name__ == "__main__":
    unittest.main()
