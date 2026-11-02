#!/usr/bin/env python3
import importlib.util
import json
from pathlib import Path
import re
import subprocess
import sys
import tempfile
import unittest

ROOT = Path(__file__).resolve().parents[3]
spec = importlib.util.spec_from_file_location("npu_config_profile", ROOT / "scripts/stress/npu_config_profile.py")
profile = importlib.util.module_from_spec(spec)
spec.loader.exec_module(profile)
SOURCE = ROOT / "third_party/ascend_npu_burn/source/ascend_npu_burn/config"
PRESET = ROOT / "configs/stress/npu-model/a2-model-b32-checkpointed-36steps.json"


class ProfileTests(unittest.TestCase):
    def test_named_a2_preset_preserves_full_source(self):
        original = profile.read_directory(SOURCE)
        with tempfile.TemporaryDirectory() as temporary:
            output = Path(temporary) / "preset"
            result = profile.create_from_preset(SOURCE, output, PRESET)
            actual = profile.read_directory(output)
            expected = profile.read_directory(SOURCE)
            for settings in profile.model_settings(expected["base_config.json"]).values():
                settings.update(batch_size=32, num_warmup=4, run_count=32)
            self.assertEqual(actual, expected)
            self.assertEqual(result["model_memory_mode"], "checkpointed")
            self.assertEqual(result["preset"], "a2-model-b32-checkpointed-36steps")
            self.assertEqual((output / "a3_config.json").read_bytes(), (SOURCE / "a3_config.json").read_bytes())
            self.assertEqual(profile.read_directory(SOURCE), original)
            for model in profile.MODELS:
                self.assertEqual(profile.model_settings(actual["base_config.json"])[model]["num_warmup"] +
                                 profile.model_settings(actual["base_config.json"])[model]["run_count"], 36)

    def test_preset_rejects_invalid_or_ambiguous_parameters(self):
        for key, value in (("schema_version", True), ("models", ["gpt3"]),
                           ("model_memory_mode", "auto"), ("required_model_parameters", {"seq_len": 1024}),
                           ("model_overrides", {"batch_size": 32, "num_warmup": 4, "run_count": 32, "hidden_size": 64}),
                           ("model_overrides", {"batch_size": 32, "num_warmup": -1, "run_count": 32})):
            with self.subTest(key=key, value=value), tempfile.TemporaryDirectory() as temporary:
                invalid = json.loads(PRESET.read_text())
                invalid[key] = value
                path = Path(temporary) / "preset.json"
                path.write_text(json.dumps(invalid))
                output = Path(temporary) / "output"
                with self.assertRaises(ValueError):
                    profile.create_from_preset(SOURCE, output, path)
                self.assertFalse(output.exists())

    def test_preset_cli_and_no_override(self):
        with tempfile.TemporaryDirectory() as temporary:
            output = Path(temporary) / "profile"
            command = [sys.executable, str(ROOT / "scripts/stress/npu_config_profile.py"),
                       "--source-dir", str(SOURCE), "--output-dir", str(output), "--preset", str(PRESET)]
            response = subprocess.run(command, check=True, capture_output=True, text=True)
            self.assertEqual(json.loads(response.stdout)["model_memory_mode"], "checkpointed")
            response = subprocess.run(command + ["--batch-size", "4"], capture_output=True, text=True)
            self.assertNotEqual(response.returncode, 0)
            self.assertIn("cannot be combined", response.stderr)

    def test_preset_validation_is_bounded_not_hardware_certification(self):
        value = json.loads(PRESET.read_text())
        self.assertEqual(value["model_overrides"], {"batch_size": 32, "num_warmup": 4, "run_count": 32})
        self.assertEqual(value["validation"]["logical_devices"], [0, 1])
        for model in profile.MODELS:
            self.assertEqual(value["validation"][model], {"result": "PASS", "checked_steps_per_device": 36, "errors": 0})
        self.assertEqual(value["validation"]["a3_hardware"], "NOT_RUN")
        self.assertEqual(value["validation"]["full_64_warmup_2048_measured_steps"], "NOT_RUN")
        self.assertEqual(value["validation"]["sustained_saturation"], "NOT_PROVEN")

    def test_preset_document_links_and_shell_syntax(self):
        path = PRESET.parent / "README.md"
        text = path.read_text(encoding="utf-8")
        self.assertEqual(len(re.findall(r"^```[^\n]*$", text, re.M)) % 2, 0)
        for target in re.findall(r"\]\(([^)]+)\)", text):
            if "://" not in target and not target.startswith("#"):
                self.assertTrue((path.parent / target.split("#")[0]).exists(), target)
        for shell in re.findall(r"```bash\n(.*?)\n```", text, re.S):
            subprocess.run(["bash", "-n"], input=shell, text=True, check=True, capture_output=True)

    def test_smoke_preserves_models_operators_a3_and_source(self):
        original = profile.read_directory(SOURCE)
        hashes = profile.identity(SOURCE)
        with tempfile.TemporaryDirectory() as temporary:
            output = Path(temporary) / "batch4"
            profile.create_profile(SOURCE, output, 4, 4, 32)
            actual = profile.read_directory(output)
            for model in profile.MODELS:
                settings = profile.model_settings(actual["base_config.json"])[model]
                expected = dict(profile.model_settings(original["base_config.json"])[model], batch_size=4, num_warmup=4, run_count=32)
                self.assertEqual(settings, expected)
            original["base_config.json"]["cases"][0].pop("gpt3")
            original["base_config.json"]["cases"][0].pop("llama2")
            actual["base_config.json"]["cases"][0].pop("gpt3")
            actual["base_config.json"]["cases"][0].pop("llama2")
            self.assertEqual(actual, original)
            self.assertEqual((output / "a3_config.json").read_bytes(), (SOURCE / "a3_config.json").read_bytes())
            self.assertEqual(profile.identity(SOURCE), hashes)

    def test_full_steps_preserved_and_no_overwrite(self):
        with tempfile.TemporaryDirectory() as temporary:
            output = Path(temporary) / "full"
            profile.create_profile(SOURCE, output, 4)
            for settings in profile.model_settings(profile.read_directory(output)["base_config.json"]).values():
                self.assertEqual((settings["num_warmup"], settings["run_count"]), (64, 2048))
            with self.assertRaises(ValueError):
                profile.create_profile(SOURCE, output, 2)

    def test_incomplete_directory_rejected(self):
        with tempfile.TemporaryDirectory() as temporary:
            output = Path(temporary)
            (output / "base_config.json").write_bytes((SOURCE / "base_config.json").read_bytes())
            with self.assertRaises(ValueError):
                profile.read_directory(output)

    def test_invalid_models_and_dtype_rejected(self):
        for key, value in (("batch_size", 0), ("run_count", True), ("num_warmup", -1), ("dtype", "float16")):
            data = profile.read_directory(SOURCE)["base_config.json"]
            profile.model_settings(data)["gpt3"][key] = value
            with self.assertRaises(ValueError):
                profile.validate_models(data)

    def test_invalid_counts_do_not_create_directory(self):
        with tempfile.TemporaryDirectory() as temporary:
            for counts in ((0, 4, 32), (4, -1, 32), (4, 4, 0)):
                output = Path(temporary) / "invalid"
                with self.assertRaises(ValueError):
                    profile.create_profile(SOURCE, output, *counts)
                self.assertFalse(output.exists())

    def test_malformed_json_rejected_and_identity_tracks_edits(self):
        with tempfile.TemporaryDirectory() as temporary:
            output = Path(temporary) / "profile"
            profile.create_profile(SOURCE, output, 4)
            before = profile.identity(output)
            path = output / "base_config.json"
            data = json.loads(path.read_text())
            profile.model_settings(data)["gpt3"]["batch_size"] = 2
            path.write_text(json.dumps(data))
            self.assertNotEqual(profile.identity(output)["base_config.json"], before["base_config.json"])
            self.assertEqual(profile.identity(output)["a3_config.json"], before["a3_config.json"])
            path.write_text("not json")
            with self.assertRaises(ValueError):
                profile.read_directory(output)
            path.write_text('{"cases":[{"gpt3":{"learning_rate":NaN}}]}')
            with self.assertRaises(ValueError):
                profile.read_directory(output)

    def test_symlink_config_rejected(self):
        with tempfile.TemporaryDirectory() as temporary:
            output = Path(temporary)
            (output / "base_config.json").symlink_to(SOURCE / "base_config.json")
            (output / "a3_config.json").write_bytes((SOURCE / "a3_config.json").read_bytes())
            with self.assertRaises(ValueError):
                profile.read_directory(output)


if __name__ == "__main__":
    unittest.main()
