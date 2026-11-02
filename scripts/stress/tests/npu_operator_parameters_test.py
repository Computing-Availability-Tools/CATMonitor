"""Hardware-free regression of the image-owned launcher's execution receipt."""
import importlib.util
import json
import os
from pathlib import Path
import tempfile
from types import SimpleNamespace
import unittest
from unittest.mock import patch

ROOT = Path(__file__).resolve().parents[3]
spec = importlib.util.spec_from_file_location("npu_launch", ROOT / "docker/stress/npu/launch.py")
launch = importlib.util.module_from_spec(spec)
spec.loader.exec_module(launch)


class OperatorParameterTests(unittest.TestCase):
    def environment(self, root):
        return patch.dict(os.environ, {"CATMONITOR_NPU_OPERATOR_PARAMETERS_PATH": str(root / "operator-parameters.json"),
                                       "CATMONITOR_NPU_JOB_ID": "aabb", "NPU_BURN_OUTPUT_DIR": str(root)})

    def test_loaded_cases_not_later_config_are_recorded_without_mutation(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            source = root / "input.json"
            config = dict(dtype=["float16"], shape=[[[4, 8], [8, 16]]], pattern=["gauss_random"], run_count=13)
            source.write_text(json.dumps(config))
            loaded = []
            def generate(args):
                loaded.append(SimpleNamespace(name="matmul", data=json.loads(source.read_text())))
                source.write_text(json.dumps({**config, "run_count": 999}))
                return loaded
            module = SimpleNamespace(generate_run_cases=generate)
            with self.environment(root):
                launch.capture_operator_parameters(module)
                result = module.generate_run_cases(SimpleNamespace(chip_generation="A3"))
            self.assertIs(result, loaded)
            self.assertEqual(result[0].data, config)
            receipt = root / "operator-parameters.json"
            self.assertEqual(json.loads(receipt.read_text()), dict(schema_version=1, job_id="aabb", chip_generation="A3",
                                         operators=[dict(name="matmul", config=config)]))
            source.unlink()
            self.assertEqual(json.loads(receipt.read_text())["operators"][0]["config"], config)

    def test_no_capture_environment_preserves_ordinary_cli(self):
        original = lambda args: []
        module = SimpleNamespace(generate_run_cases=original)
        with patch.dict(os.environ, {}, clear=True):
            launch.capture_operator_parameters(module)
        self.assertIs(module.generate_run_cases, original)

    def test_invalid_or_reused_destination_rejected(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            target = root / "operator-parameters.json"
            module = SimpleNamespace(generate_run_cases=lambda args: [])
            for path, job in (("relative.json", "ab"), (str(root.parent / "other.json"), "ab"), (str(target), "../bad")):
                with self.subTest(path=path, job=job), self.environment(root), patch.dict(os.environ, {
                        "CATMONITOR_NPU_OPERATOR_PARAMETERS_PATH": path, "CATMONITOR_NPU_JOB_ID": job}):
                    with self.assertRaises(ValueError):
                        launch.capture_operator_parameters(module)
            target.write_text("existing")
            with self.environment(root), self.assertRaises(ValueError):
                launch.capture_operator_parameters(module)
            self.assertEqual(target.read_text(), "existing")

    def test_empty_failed_or_invalid_loader_cannot_claim_actual_parameters(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            target = root / "operator-parameters.json"
            with self.environment(root):
                module = SimpleNamespace(generate_run_cases=lambda args: [])
                launch.capture_operator_parameters(module)
                self.assertEqual(module.generate_run_cases(SimpleNamespace(chip_generation="A2")), [])
                self.assertFalse(target.exists())
                def fail(args):
                    raise RuntimeError("loader failed")
                module = SimpleNamespace(generate_run_cases=fail)
                launch.capture_operator_parameters(module)
                with self.assertRaises(RuntimeError):
                    module.generate_run_cases(SimpleNamespace(chip_generation="A2"))
                self.assertFalse(target.exists())
                module = SimpleNamespace(generate_run_cases=lambda args: [SimpleNamespace(name="matmul",data={"run_count":float("nan")})])
                launch.capture_operator_parameters(module)
                with self.assertRaises(ValueError):
                    module.generate_run_cases(SimpleNamespace(chip_generation="A2"))
                self.assertFalse(target.exists())


if __name__ == "__main__":
    unittest.main()
