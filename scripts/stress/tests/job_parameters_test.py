"""Exercise public patch/config code; no torch, Docker or NPU required."""
import ast
import copy
import importlib.util
import json
import os
from pathlib import Path
import shutil
import subprocess
import tempfile
import unittest
from unittest.mock import patch

ROOT = Path(__file__).resolve().parents[3]

def module(name, path):
    spec = importlib.util.spec_from_file_location(name, path)
    value = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(value)
    return value

resolver = module("job_config", ROOT / "docker/stress/npu/job_config.py")
sdc = module("model_sdc_job", ROOT / "docker/stress/npu/model_sdc.py")

class JobParametersTests(unittest.TestCase):
    @classmethod
    def setUpClass(cls):
        cls.temp = tempfile.TemporaryDirectory()
        cls.source = Path(cls.temp.name) / "source"
        shutil.copytree(ROOT / "third_party/ascend_npu_burn/source", cls.source)
        for path in cls.source.rglob("*.py"):
            path.write_bytes(path.read_bytes().replace(b"\r\n", b"\n"))
        for name in ("portable-runtime-v1.patch", "a2-upstream-base.patch", "model-sdc-v1.patch", "model-memory-v1.patch", "job-parameters-v1.patch"):
            with (ROOT / "scripts/stress/patches/ascend_npu_burn" / name).open("rb") as source:
                subprocess.run(["patch", "--batch", "--forward", "-p1", "-d", str(cls.source)], stdin=source, capture_output=True, check=True)
        cls.catalog = module("patched_job_catalog", cls.source / "ascend_npu_burn/case_catalog.py")
        cls.base = cls.catalog.load_config("A2")

    @classmethod
    def tearDownClass(cls):
        cls.temp.cleanup()

    def test_operator_partial_inherit_and_config_not_modified(self):
        pristine = copy.deepcopy(self.base)
        result = resolver.resolve(self.base, self.base, "A2", ["matmul"], {"operators": {"matmul": {"dtype": ["float16"], "shape": [[[16, 8], [8, 32]]], "run_count": 2}}})
        self.assertEqual(self.base, pristine)
        self.assertEqual(result["effective"]["operators"]["matmul"]["run_count"], 2)
        self.assertEqual(resolver.cases(result["config"])["conv2d"], resolver.cases(pristine)["conv2d"])
        self.assertEqual(len(resolver.cases(result["config"])), 20)

    def test_bad_dtype_shape_selection_and_model_mix_rejected(self):
        for op in ({"dtype": ["not_supported"]}, {"shape": [[[4, 8], [7, 16]]]}, {"pattern": []}, {"run_count": 0}, {"dimensions": {"path": "/tmp"}}):
            with self.subTest(op=op), self.assertRaises((ValueError, TypeError)):
                resolver.resolve(self.base, self.base, "A2", ["matmul"], {"operators": {"matmul": op}})
        with self.assertRaises(ValueError):
            resolver.resolve(self.base, self.base, "A2", ["quant_matmul"])
        with self.assertRaises(ValueError):
            resolver.resolve(self.base, self.base, "A2", ["matmul"], {"operators": {"conv2d": {"run_count": 1}}})

    def test_conv_and_attention_contract(self):
        for name, dims in [("conv2d", {"input_data": [1, 2, 8, 8], "weight_data": [4, 3, 3, 3]}), ("fusion_attention", {"BSHN": [[1, 16, 33, 4]]})]:
            with self.assertRaises(ValueError):
                resolver.resolve(self.base, self.base, "A2", [name], {"operators": {name: {"dimensions": dims}}})

    def test_model_and_threshold_identity_changes_require_reference(self):
        result = resolver.resolve(self.base, self.base, "A2", [], {"model": {"hidden_size": 32, "num_attention_heads": 4, "run_count": 2}, "sdc_tolerances": {"loss": 0.02}}, "gpt3")
        self.assertEqual(result["effective"]["model"]["hidden_size"], 32)
        self.assertEqual(result["effective"]["sdc_tolerances"], dict(loss=0.02, grad=0.1, checksum=0.1))
        for training in ({"hidden_size": 33, "num_attention_heads": 4}, {"dtype": "float16"}, {"learning_rate": float("nan")}):
            with self.assertRaises(ValueError):
                resolver.resolve(self.base, self.base, "A2", [], {"model": training}, "gpt3")
        identity = dict(model="gpt3", config=result["config"], sdc_tolerances=result["effective"]["sdc_tolerances"])
        with tempfile.TemporaryDirectory() as path:
            store = sdc.ReferenceStore(Path(path))
            # Use the real immutable store API; no training success is claimed.
            directory = Path(path) / ".stage"
            (directory / "gpt3").mkdir(parents=True)
            (directory / "gpt3/0123456789abcdef.json").write_text(json.dumps({"step_1": {"loss": 1.0, "grad_norm": 1.0, "checksum": 1.0}}))
            store.publish("ref1", directory, identity, 1, "fixture-job")
            changed = copy.deepcopy(identity)
            changed["sdc_tolerances"]["loss"] = 0.1
            with self.assertRaises(ValueError):
                store.load("ref1", changed, 1)

    def test_frozen_config_survives_shared_directory_change(self):
        config = resolver.resolve(self.base, self.base, "A2", ["matmul"], {"operators": {"matmul": {"run_count": 3}}})["config"]
        with tempfile.TemporaryDirectory() as path:
            frozen = Path(path) / "frozen.json"
            frozen.write_text(json.dumps(dict(generation="A2", config=config)))
            with patch.dict(os.environ, {"CATMONITOR_NPU_FROZEN_CONFIG_FILE": str(frozen)}):
                self.assertEqual(self.catalog.load_config("A2", Path(path)/"no-longer-present"), config)
                with self.assertRaises(ValueError):
                    self.catalog.load_config("A3")
            actual = ast.parse((self.source / "ascend_npu_burn/common/utils.py").read_text())
            manager = next(n for n in actual.body if isinstance(n, ast.ClassDef) and n.name == "ConfigManager")
            init = next(n for n in manager.body if isinstance(n, ast.FunctionDef) and n.name == "__init__")
            source = ast.unparse(init)
            self.assertLess(source.index("frozen_config()"), source.index("isinstance(json_data, str)"))

    def test_complete_a3_schema_is_preserved(self):
        a3 = self.catalog.load_config("A3")
        result = resolver.resolve(a3, a3, "A3", ["quant_matmul"], {"operators": {"quant_matmul": {"dtype": ["int8"], "run_count": 1}}})
        self.assertEqual(len(resolver.cases(result["config"])), 20)
        self.assertEqual(resolver.cases(result["config"])["gpt3"], resolver.cases(a3)["gpt3"])
        for name in ("base_config.json", "a3_config.json"):
            self.assertEqual((self.source / "ascend_npu_burn/config" / name).read_bytes(), (ROOT / "third_party/ascend_npu_burn/source/ascend_npu_burn/config" / name).read_bytes())

    def test_a3_only_release_patch_chain_without_a2_admission(self):
        with tempfile.TemporaryDirectory() as folder:
            target = Path(folder) / "source"
            shutil.copytree(ROOT / "third_party/ascend_npu_burn/source", target)
            for path in target.rglob("*.py"):
                path.write_bytes(path.read_bytes().replace(b"\r\n", b"\n"))
            for name in ("portable-runtime-v1.patch", "model-sdc-v1.patch",
                         "model-memory-v1.patch", "job-parameters-v1.patch"):
                with (ROOT / "scripts/stress/patches/ascend_npu_burn" / name).open("rb") as source:
                    subprocess.run(["patch", "--batch", "--forward", "-p1", "-d", str(target)],
                                   stdin=source, capture_output=True, check=True)
            catalog = module("a3_only_job_catalog", target / "ascend_npu_burn/case_catalog.py")
            config = catalog.load_config("A3")
            result = resolver.resolve(config, config, "A3", ["quant_matmul"],
                                      {"operators": {"quant_matmul": {"dtype": ["int8"], "run_count": 2}}})
            self.assertEqual(len(resolver.cases(result["config"])), 20)
            for name in ("base_config.json", "a3_config.json"):
                self.assertEqual((target / "ascend_npu_burn/config" / name).read_bytes(),
                                 (ROOT / "third_party/ascend_npu_burn/source/ascend_npu_burn/config" / name).read_bytes())
            self.assertFalse((target / "ascend_npu_burn/config/a2_config.json").exists())

if __name__ == "__main__":
    unittest.main()
