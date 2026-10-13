"""Hardware-free model/reference regressions, not NPU hardware certification."""
import ast
import csv
from contextlib import redirect_stderr, redirect_stdout
from datetime import datetime, timezone
import hashlib
import importlib.util
import io
import json
import logging
import os
from pathlib import Path
import shutil
import subprocess
import sys
import tempfile
import time
import types
import unittest
from unittest.mock import patch

ROOT = Path(__file__).resolve().parents[3]
spec = importlib.util.spec_from_file_location("model_sdc", ROOT / "docker/stress/npu/model_sdc.py")
sdc = importlib.util.module_from_spec(spec)
spec.loader.exec_module(sdc)
CONFIG = dict(run_count=2, num_warmup=1)
IDENTITY = dict(model="gpt3", generation="A2", config=CONFIG,
                stack={"torch": "fixture", "cann": "fixture"})


def records(count=3):
    return {"step_" + str(i): dict(loss=1.0, grad_norm=2.0, checksum=3.0)
            for i in range(1, count + 1)}


class MemoryTests(unittest.TestCase):
    CONFIG = dict(CONFIG, dtype="bfloat16")

    def screen(self, capacities, count=100, devices=None):
        torch = types.SimpleNamespace(npu=types.SimpleNamespace(
            mem_get_info=lambda device: capacities[device]))
        with patch.object(sdc.importlib, "import_module", return_value=torch), \
                patch.object(sdc, "model_parameter_count", return_value=count):
            return sdc.basic_memory_preflight(self.CONFIG,
                                             list(capacities) if devices is None else devices, None)

    def test_basic_minimum_and_exact_capacity_pass(self):
        proof = self.screen({1: (800, 1000)})
        self.assertEqual(proof["required_bytes"], 800)
        self.assertEqual(proof["parameter_count"], 100)
        self.assertEqual(proof["scope"], "basic_lower_bound")
        self.assertFalse(proof["full_peak_known"])
        self.assertEqual(proof["status"], "pass")
        self.assertIn("may still fail with OOM", proof["message"])

    def test_two_cards_do_not_pool_capacity(self):
        proof = self.screen({0: (500, 1000), 1: (500, 1000)})
        self.assertEqual(proof["status"], "fail")
        self.assertTrue(all(row["status"] == "fail" for row in proof["devices"]))
        self.assertIn("logical 0", proof["message"])
        self.assertIn("logical 1", proof["message"])

    def test_a2_capacity_passes_basic_screen_without_full_peak_claim(self):
        proof = self.screen({0: (29 * 2**30, 30 * 2**30)}, count=800_000_000)
        self.assertEqual(proof["status"], "pass")
        self.assertFalse(proof["full_peak_known"])
        self.assertLess(proof["required_bytes"], 29 * 2**30)

    def test_one_insufficient_selected_device_fails(self):
        proof = self.screen({0: (900, 1000), 1: (799, 1000)})
        self.assertEqual(proof["status"], "fail")
        self.assertEqual([row["status"] for row in proof["devices"]], ["pass", "fail"])

    def test_invalid_capacity_and_selection_never_pass(self):
        for capacity in ((-1, 1000), (1001, 1000), (0, 0), (True, 1000), (1.0, 1000)):
            with self.subTest(capacity=capacity), self.assertRaisesRegex(ValueError, "invalid NPU HBM"):
                self.screen({0: capacity})
        for devices in ([], [0, 0], [-1], [True]):
            with self.subTest(devices=devices), self.assertRaises(ValueError):
                self.screen({0: (900, 1000)}, devices=devices)

    def test_missing_capacity_query_and_wrong_dtype_fail(self):
        with patch.object(sdc.importlib, "import_module", return_value=types.SimpleNamespace()), \
                patch.object(sdc, "model_parameter_count", return_value=100):
            with self.assertRaisesRegex(ValueError, "HBM query is unavailable"):
                sdc.basic_memory_preflight(self.CONFIG, [0], None)
            with self.assertRaisesRegex(ValueError, "actual BF16"):
                sdc.basic_memory_preflight(dict(self.CONFIG, dtype="float32"), [0], None)

    def test_describe_failure_json_is_not_training_pass(self):
        with tempfile.TemporaryDirectory() as name:
            proof = self.screen({0: (799, 1000)})
            output = io.StringIO()
            with patch.dict(os.environ, {"NPU_BURN_CHIP_GENERATION": "A2",
                                         "CATMONITOR_MODEL_REFERENCE_ROOT": str(Path(name) / "refs")}), \
                    patch.object(sdc, "load_runtime", return_value=(self.CONFIG, IDENTITY, None)), \
                    patch.object(sdc, "basic_memory_preflight", return_value=proof), \
                    patch.object(sdc, "execute") as execute, redirect_stdout(output):
                code = sdc.main(["--model", "gpt3", "--action", "create_reference",
                                 "--reference", "test-ref", "--devices", "0", "--describe"])
            self.assertEqual(code, 0)
            described = json.loads(output.getvalue())
            self.assertEqual(described["preflight"], "fail")
            self.assertEqual(described["memory_preflight"]["status"], "fail")
            self.assertEqual(described["hardware_validation"], "NOT_RUN")
            execute.assert_not_called()
            self.assertFalse((Path(name) / "refs").exists())

    def test_run_refreshes_capacity_and_allows_basic_pass_on_a2(self):
        with tempfile.TemporaryDirectory() as name:
            passing = self.screen({0: (29 * 2**30, 30 * 2**30)}, count=800_000_000)
            failing = self.screen({0: (1, 30 * 2**30)}, count=800_000_000)
            args = ["--model", "gpt3", "--action", "create_reference",
                    "--reference", "test-ref", "--devices", "0"]
            with patch.dict(os.environ, {"NPU_BURN_CHIP_GENERATION": "A2",
                                         "CATMONITOR_MODEL_REFERENCE_ROOT": str(Path(name) / "refs"),
                                         "NPU_BURN_OUTPUT_DIR": str(Path(name) / "output")}), \
                    patch.object(sdc, "load_runtime", return_value=(self.CONFIG, IDENTITY, None)), \
                    patch.object(sdc, "basic_memory_preflight", side_effect=[passing, failing]) as screen, \
                    patch.object(sdc, "execute") as execute:
                self.assertEqual(sdc.main(args), 0)
                with self.assertRaisesRegex(ValueError, "Insufficient basic NPU HBM"):
                    sdc.main(args)
                self.assertEqual(screen.call_count, 2)
                self.assertEqual(execute.call_count, 1)

    def test_oom_classifier_does_not_relabel_cpu_or_cuda_failure(self):
        for message in ("NPU out of memory. Tried to allocate 162 MiB",
                        "NPU memory exhausted", "ACL_ERROR_RT_MEMORY_ALLOCATION"):
            self.assertTrue(sdc.is_npu_oom(RuntimeError(message)))
        for message in ("CPU out of memory", "CUDA out of memory", "worker startup failed"):
            self.assertFalse(sdc.is_npu_oom(RuntimeError(message)))

    def test_runtime_oom_cannot_publish_reference_or_success_result(self):
        class OOMModel:
            def run(self):
                raise RuntimeError("NPU out of memory. Tried to allocate 162 MiB")
        with tempfile.TemporaryDirectory() as name:
            root = Path(name)
            store = sdc.ReferenceStore(root / "refs")
            with self.assertRaisesRegex(RuntimeError,
                    "NPU_HBM_OOM: model=gpt3, logical_device=1"):
                sdc.execute("gpt3", "create_reference", "oom-ref", [1], store,
                            root / "output", self.CONFIG, IDENTITY, OOMModel, "oom-job")
            self.assertFalse((store.root / "oom-ref").exists())
            self.assertFalse((root / "output/model-result.json").exists())
            self.assertFalse((root / "output/model-results.csv").exists())

    def test_basic_pass_then_oom_exits_nonzero_with_explicit_error(self):
        class OOMModel:
            def run(self):
                raise RuntimeError("NPU out of memory. Tried to allocate 162 MiB")
        passing = self.screen({0: (29 * 2**30, 30 * 2**30)}, count=800_000_000)
        for model in ("gpt3", "llama2"):
            with self.subTest(model=model), tempfile.TemporaryDirectory() as name:
                root = Path(name)
                error = io.StringIO()
                with patch.dict(os.environ, {"NPU_BURN_CHIP_GENERATION": "A2",
                                             "CATMONITOR_MODEL_REFERENCE_ROOT": str(root / "refs"),
                                             "NPU_BURN_OUTPUT_DIR": str(root / "output"),
                                             "CATMONITOR_MODEL_JOB_ID": "oom-job"}), \
                        patch.object(sdc, "load_runtime", return_value=(self.CONFIG, IDENTITY, OOMModel)), \
                        patch.object(sdc, "basic_memory_preflight", return_value=passing), \
                        redirect_stderr(error):
                    code = sdc.cli_main(["--model", model, "--action", "create_reference",
                                         "--reference", "oom-ref", "--devices", "0"])
                self.assertEqual(code, 1)
                self.assertIn("ERROR: model SDC: NPU_HBM_OOM: model=" + model, error.getvalue())
                self.assertIn("logical_device=0", error.getvalue())
                self.assertIn("162 MiB", error.getvalue())
                self.assertFalse((root / "refs/oom-ref").exists())
                self.assertFalse((root / "output/oom-job/model-result.json").exists())
                self.assertFalse((root / "output/oom-job/model-results.csv").exists())


class ReferenceTests(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory()
        self.root = Path(self.temp.name)
        self.store = sdc.ReferenceStore(self.root / "references")

    def tearDown(self):
        self.temp.cleanup()

    def test_memory_mode_is_explicit_and_reference_bound(self):
        with patch.dict(os.environ, {"CATMONITOR_MODEL_MEMORY_MODE":"invalid"}):
            with self.assertRaises(ValueError):
                sdc.memory_mode()
        self.publish()
        with self.assertRaisesRegex(ValueError, "does not match"):
            self.store.load("ref1", {**IDENTITY, "execution":{"memory_mode":"checkpointed"}}, 3)

    def test_checkpoint_dependency_requires_real_api_capability(self):
        class Model:
            supports_gradient_checkpointing = True
            def gradient_checkpointing_enable(self, **kwargs):
                pass
        modules = {"torch":types.SimpleNamespace(bfloat16=object()), "torch_npu":object(),
                   "transformers":types.SimpleNamespace(GPT2Config=type, GPT2Model=Model)}
        with patch.dict(os.environ, {"CATMONITOR_MODEL_MEMORY_MODE":"checkpointed"}), \
                patch.object(sdc.importlib, "import_module", side_effect=modules.__getitem__):
            sdc.model_dependencies("gpt3")
            Model.supports_gradient_checkpointing = False
            with self.assertRaisesRegex(ValueError, "no fallback"):
                sdc.model_dependencies("gpt3")

    def publish(self, name="ref1"):
        stage = Path(tempfile.mkdtemp(dir=self.store.root))
        (stage / "gpt3").mkdir()
        sdc.write_json(stage / "gpt3/0123456789abcdef.json", records())
        return self.store.publish(name, stage, IDENTITY, 3, "generation-job")

    def test_readonly_describe_emits_typed_reference_failure_without_writes(self):
        final = self.publish()
        manifest_path = final / "manifest.json"
        original = manifest_path.read_bytes()
        for scenario, action, reference, identity, want in (
                ("missing", "compare", "missing", IDENTITY, "reference_missing"),
                ("mismatch", "compare", "ref1", {**IDENTITY, "config": {"run_count": 3}}, "reference_mismatch"),
                ("exists", "create_reference", "ref1", IDENTITY, "reference_exists"),
                ("incomplete", "compare", "ref1", IDENTITY, "reference_incomplete")):
            with self.subTest(scenario=scenario):
                manifest_path.write_bytes(original)
                if scenario == "incomplete":
                    manifest = json.loads(original)
                    manifest["complete"] = False
                    manifest_path.write_text(json.dumps(manifest))
                before = {p.relative_to(self.root): p.read_bytes()
                          for p in self.root.rglob("*") if p.is_file()}
                output, error = io.StringIO(), io.StringIO()
                with patch.dict(os.environ, {"NPU_BURN_CHIP_GENERATION": "A2",
                                             "CATMONITOR_MODEL_REFERENCE_ROOT": str(self.store.root)}), \
                        patch.object(sdc, "load_runtime", return_value=(CONFIG, identity, None)), \
                        patch.object(sdc, "basic_memory_preflight", return_value={"status": "pass"}), \
                        patch.object(sdc, "execute") as execute, redirect_stdout(output), redirect_stderr(error):
                    rc = sdc.cli_main(["--model", "gpt3", "--action", action,
                                       "--reference", reference, "--devices", "0", "--describe"])
                self.assertEqual(rc, 1)
                proof = json.loads(output.getvalue())
                self.assertEqual(proof["preflight"], "fail")
                self.assertEqual(proof["code"], want)
                self.assertEqual(proof["hardware_validation"], "NOT_RUN")
                execute.assert_not_called()
                after = {p.relative_to(self.root): p.read_bytes()
                         for p in self.root.rglob("*") if p.is_file()}
                self.assertEqual(before, after, "metadata probe mutated immutable references")

    def test_readonly_import_failure_is_not_missing_reference_or_hardware_failure(self):
        output, error = io.StringIO(), io.StringIO()
        with patch.dict(os.environ, {"NPU_BURN_CHIP_GENERATION": "A2"}), \
                patch.object(sdc, "load_runtime", side_effect=ImportError("transformers unavailable")), \
                patch.object(sdc, "execute") as execute, redirect_stdout(output), redirect_stderr(error):
            rc = sdc.cli_main(["--model", "llama2", "--action", "compare",
                               "--reference", "ref1", "--devices", "0", "--describe"])
        self.assertEqual(rc, 1)
        proof = json.loads(output.getvalue())
        self.assertEqual(proof["code"], "dependency_unavailable")
        self.assertEqual(proof["hardware_validation"], "NOT_RUN")
        execute.assert_not_called()

    def test_complete_reference_and_stack_match(self):
        self.publish()
        self.assertEqual(self.store.load("ref1", IDENTITY, 3), self.store.root / "ref1")
        for change in ({**IDENTITY, "generation": "A3"},
                       {**IDENTITY, "config": {"run_count": 3}},
                       {**IDENTITY, "stack": {"torch": "changed"}},
                       {**IDENTITY, "model": "llama2"}):
            with self.assertRaises(ValueError):
                self.store.load("ref1", change, 3)

    def test_missing_partial_stale_and_corrupt_reference_rejected(self):
        with self.assertRaises(OSError):
            self.store.load("missing", IDENTITY, 3)
        final = self.publish()
        manifest = sdc.read_json(final / "manifest.json")
        data = records()
        data["step_1"]["loss"] = 5
        (final / manifest["baseline_file"]).write_text(json.dumps(data))
        with self.assertRaisesRegex(ValueError, "changed"):
            self.store.load("ref1", IDENTITY, 3)
        for data in (records(2), {**records(), "step_4": records()["step_1"]},
                     {"step_1": {"loss": 0}},
                     {**records(), "step_1": {"loss": float("nan"), "grad_norm": 2, "checksum": 3}}):
            with self.assertRaises(ValueError):
                sdc.validate_records(data, 3)

    def test_path_traversal_and_overwrite_rejected(self):
        for name in ("../ref", "/root/ref", "", "ref/a", "a" * 65):
            with self.assertRaises(ValueError):
                self.store.path(name)
        self.publish()
        with self.assertRaises(ValueError):
            self.publish()

    def test_device_selection_is_explicit(self):
        sdc.validate_request("llama2", "compare", "ref1", [0, 1])
        for devices in ([], [0, 0], [-1], [True], [0, 1]):
            with self.assertRaises(ValueError):
                sdc.validate_request("gpt3", "create_reference", "ref1", devices)

    def test_transformers_api_contract_with_synthetic_modules(self):
        torch = types.SimpleNamespace(bfloat16=object())
        transformers = types.SimpleNamespace(GPT2Config=type, GPT2Model=type, LlamaConfig=type, LlamaModel=type)
        modules = {"torch":torch, "torch_npu":object(), "transformers":transformers}
        with patch.object(sdc.importlib, "import_module", side_effect=modules.__getitem__):
            sdc.model_dependencies("gpt3")
            sdc.model_dependencies("llama2")
            del transformers.GPT2Model
            with self.assertRaisesRegex(ValueError, "GPT2Model"):
                sdc.model_dependencies("gpt3")
            del torch.bfloat16
            with self.assertRaisesRegex(ValueError, "bfloat16"):
                sdc.model_dependencies("llama2")
        with patch.object(sdc.importlib, "import_module", side_effect=ImportError("transformers unavailable")):
            with self.assertRaises(ImportError):
                sdc.model_dependencies("gpt3")

    def test_result_contract_rejects_missing_checked_steps(self):
        row = dict(task="gpt3", result=True, err_count=0, run_count=2, exetime=0.5,
                   metrics=dict(sdc_baseline_mode="compare", sdc_baseline_checked_steps=3,
                                sdc_failure_count=0, trace_steps=3))
        result = dict(error_records=[], case_statistics=[row])
        sdc.validate_model_result(result, "gpt3", "compare", CONFIG)
        for altered in ({**row, "err_count": 1}, {**row, "result": False},
                        {**row, "metrics": {**row["metrics"], "sdc_baseline_checked_steps": 0}},
                        {**row, "run_count": 1}, {**row, "task": "llama2"},
                        {**row, "exetime": float("inf")}):
            with self.assertRaises(ValueError):
                sdc.validate_model_result(dict(error_records=[], case_statistics=[altered]),
                                          "gpt3", "compare", CONFIG)


class AppliedUpstreamTests(unittest.TestCase):
    @classmethod
    def setUpClass(cls):
        cls.temp = tempfile.TemporaryDirectory()
        cls.source = Path(cls.temp.name) / "source"
        shutil.copytree(ROOT / "third_party/ascend_npu_burn/source", cls.source)
        # Match the existing Linux patch fixture: normalize only the copied
        # Python files, never the checked-in upstream source.
        for path in cls.source.rglob("*.py"):
            path.write_bytes(path.read_bytes().replace(b"\r\n", b"\n"))
        # A real patch application, not a hand-reimplemented copy of the code.
        for name in ("portable-runtime-v1.patch", "a2-upstream-base.patch", "model-sdc-v1.patch", "model-memory-v1.patch"):
            subprocess.run(["git", "apply", "--unsafe-paths", "--unidiff-zero",
                            str(ROOT / "scripts/stress/patches/ascend_npu_burn" / name)],
                           cwd=cls.source, check=True, capture_output=True)
        directory = cls.source / "ascend_npu_burn/benchmarks/model"
        scope = dict(os=os, Path=Path, json=json, sha256=hashlib.sha256,
                     datetime=datetime, timezone=timezone, time=time,
                     logger=logging.getLogger("fixture"), logging=logging, torch=None,
                     TrainingRuntime=type("TrainingRuntime", (), {}))
        # Run actual model/SDC classes without importing torch or probing NPU.
        for filename in ("run_config.py", "sdc_guard.py", "trace_writer.py", "result_reporter.py", "model_base.py"):
            source = ast.parse((directory / filename).read_text(encoding="utf-8"))
            source.body = [node for node in source.body if not isinstance(node, (ast.Import, ast.ImportFrom))]
            scope["__file__"] = str(directory / filename)
            exec(compile(source, str(directory / filename), "exec"), scope)
        base = scope["ModelBase"]

        class SyntheticModel(base):
            MODEL_KEY = "gpt3"

            def _read_config_value(self, key):
                return self.task_config.get(key, 2 if key != "dtype" else "bfloat16")

            def _train(self):
                self._run_device_name = "npu:" + str(self.device)
                for step in range(1, 4):
                    self._record_step(step, "measure", 1, 1.0, 2.0, 3.0)
                return {}

            def _extract_number(self, value):
                return float(value)

            def _ensure_finite(self, *args):
                pass

        cls.factory = SyntheticModel
        cls.scope = scope

    @classmethod
    def tearDownClass(cls):
        cls.temp.cleanup()

    def test_checkpoint_patch_preserves_shape_and_requires_api(self):
        calls = []
        class Decoder:
            supports_gradient_checkpointing = True
            config = types.SimpleNamespace(use_cache=True)
            def gradient_checkpointing_enable(self, **kwargs):
                calls.append(kwargs)
        class Module:
            def __init__(self):
                pass
        torch = types.SimpleNamespace(nn=types.SimpleNamespace(Module=Module, Linear=lambda *a: object()))
        decoder = Decoder()
        with patch.dict(os.environ, {"CATMONITOR_MODEL_MEMORY_MODE":"standard"}):
            self.scope["build_training_head"](torch, decoder, 1280, 100)
        self.assertEqual(calls, [])
        with patch.dict(os.environ, {"CATMONITOR_MODEL_MEMORY_MODE":"checkpointed"}):
            self.scope["build_training_head"](torch, decoder, 1280, 100)
            self.assertEqual(calls, [{"gradient_checkpointing_kwargs":{"use_reentrant":False,"preserve_rng_state":True}}])
            self.assertFalse(decoder.config.use_cache)
            with self.assertRaisesRegex(ValueError, "no fallback"):
                self.scope["build_training_head"](torch, object(), 1280, 100)

    def test_full_upstream_configs_preserved_for_a2_and_a3(self):
        package = self.source / "ascend_npu_burn"
        pristine = ROOT / "third_party/ascend_npu_burn/source/ascend_npu_burn/config"
        for filename in ("base_config.json", "a3_config.json"):
            self.assertEqual((package / "config" / filename).read_bytes(),
                             (pristine / filename).read_bytes())
        self.assertFalse((package / "config/a2_config.json").exists())
        spec = importlib.util.spec_from_file_location("fixture_case_catalog", package / "case_catalog.py")
        catalog = importlib.util.module_from_spec(spec)
        spec.loader.exec_module(catalog)
        cases = {key: value for group in catalog.load_config("A2")["cases"]
                 for key, value in group.items()}
        for model in ("gpt3", "llama2"):
            config = cases[model]
            for key, value in dict(run_count=2048, num_warmup=64, batch_size=32,
                                   seq_len=512, hidden_size=1280, num_hidden_layers=36,
                                   num_attention_heads=20, dtype="bfloat16").items():
                self.assertEqual(config[key], value)
            self.assertEqual(sdc.step_count(config), 2112)

    def test_a3_patch_chain_without_a2_patch(self):
        with tempfile.TemporaryDirectory() as root:
            source = Path(root) / "source"
            pristine = ROOT / "third_party/ascend_npu_burn/source"
            shutil.copytree(pristine, source)
            for path in source.rglob("*.py"):
                path.write_bytes(path.read_bytes().replace(b"\r\n", b"\n"))
            for name in ("portable-runtime-v1.patch", "model-sdc-v1.patch", "model-memory-v1.patch"):
                subprocess.run(["git","apply","--unsafe-paths","--unidiff-zero",str(ROOT / "scripts/stress/patches/ascend_npu_burn" / name)], cwd=source, check=True, capture_output=True)
            for filename in ("base_config.json", "a3_config.json"):
                relative = "ascend_npu_burn/config/" + filename
                self.assertEqual((source / relative).read_bytes(), (pristine / relative).read_bytes())
            spec = importlib.util.spec_from_file_location("fixture_a3_catalog", source / "ascend_npu_burn/case_catalog.py")
            catalog = importlib.util.module_from_spec(spec)
            spec.loader.exec_module(catalog)
            cases = {key:value for group in catalog.load_config("A3")["cases"] for key,value in group.items()}
            for model in ("gpt3","llama2"):
                self.assertEqual(cases[model]["batch_size"], 32)
                self.assertEqual(cases[model]["num_hidden_layers"], 36)
                self.assertEqual(sdc.step_count(cases[model]), 2112)

    def test_meta_count_uses_resolved_case_without_npu_or_sample_allocation(self):
        class MetaDevice:
            def __enter__(self):
                return self

            def __exit__(self, *args):
                pass

        class MetadataModel(self.factory):
            def _create_model(self):
                self.assert_case = self._required_case_int("hidden_size")
                self._prepare_runtime(torch)
                self._prime_sample_bank(torch, 100, 2)
                self._network = types.SimpleNamespace(parameters=lambda: [
                    types.SimpleNamespace(numel=lambda: 32),
                    types.SimpleNamespace(numel=lambda: 16)])

            def _prepare_runtime(self, module):
                raise AssertionError("must not initialize NPU during meta counting")

            def _prime_sample_bank(self, *args):
                raise AssertionError("must not allocate training inputs during meta counting")

        torch = types.SimpleNamespace(device=lambda name: MetaDevice(), bfloat16=object())
        self.assertEqual(sdc.model_parameter_count(dict(CONFIG, hidden_size=1280), MetadataModel, torch), 48)
        # Overrides must not leak from the disposable metadata instance.
        with self.assertRaises(AssertionError):
            MetadataModel()._prepare_runtime(torch)

    def test_generation_and_multi_device_comparison_with_real_sdc_guard(self):
        with tempfile.TemporaryDirectory() as name:
            root = Path(name)
            store = sdc.ReferenceStore(root / "refs")
            generated = sdc.execute("gpt3", "create_reference", "ref1", [0], store,
                root / "generation", CONFIG, IDENTITY, self.factory, "job-generation")
            self.assertEqual(generated["outcome"], "reference_generated")
            self.assertEqual(generated["checked_steps"], 0)
            compared = sdc.execute("gpt3", "compare", "ref1", [0, 1], store,
                root / "comparison", CONFIG, IDENTITY, self.factory, "job-comparison")
            self.assertEqual(compared["outcome"], "sdc_passed")
            self.assertEqual(compared["checked_steps"], 6)
            with Path(compared["csv"]).open(newline="") as source:
                rows = list(csv.DictReader(source))
            self.assertEqual([row["device_id"] for row in rows], ["0", "1"])
            self.assertTrue(all(row["job_id"] == "job-comparison" for row in rows))
            with self.assertRaises(ValueError):
                sdc.execute("gpt3", "compare", "ref1", [0], store,
                    root / "comparison", CONFIG, IDENTITY, self.factory, "job-comparison")

    def test_training_failure_cannot_publish_reference(self):
        class FailingModel(self.factory):
            def _train(self):
                raise RuntimeError("worker/model startup failed")
        with tempfile.TemporaryDirectory() as name:
            root = Path(name)
            store = sdc.ReferenceStore(root / "refs")
            with self.assertRaises(RuntimeError):
                sdc.execute("gpt3", "create_reference", "failed", [0], store,
                    root / "run", CONFIG, IDENTITY, FailingModel, "job-failed")
            self.assertFalse((store.root / "failed").exists())

    def test_llama2_generation_and_detection(self):
        class LlamaFixture(self.factory):
            MODEL_KEY = "llama2"
        identity = {**IDENTITY, "model":"llama2"}
        with tempfile.TemporaryDirectory() as name:
            root = Path(name)
            store = sdc.ReferenceStore(root / "refs")
            sdc.execute("llama2", "create_reference", "llama-ref", [1], store,
                root / "save", CONFIG, identity, LlamaFixture, "llama-save")
            compared = sdc.execute("llama2", "compare", "llama-ref", [0], store,
                root / "compare", CONFIG, identity, LlamaFixture, "llama-compare")
            self.assertEqual(compared["checked_steps"], 3)

    def test_real_sdc_disagreement_is_not_pass(self):
        class CorruptModel(self.factory):
            def _train(self):
                self._run_device_name = "npu:" + str(self.device)
                for step in range(1, 4):
                    self._record_step(step, "measure", 1, 99.0, 2.0, 3.0)
                return {}
        with tempfile.TemporaryDirectory() as name:
            root = Path(name)
            store = sdc.ReferenceStore(root / "refs")
            sdc.execute("gpt3", "create_reference", "ref1", [0], store,
                root / "save", CONFIG, IDENTITY, self.factory, "save")
            with self.assertRaises(ValueError):
                sdc.execute("gpt3", "compare", "ref1", [1], store,
                    root / "bad", CONFIG, IDENTITY, CorruptModel, "bad")
            self.assertFalse((root / "bad/model-result.json").exists())


if __name__ == "__main__":
    unittest.main()
