"""GPT3/Llama2 model SDC adapter; no hardware is probed at module import.

The image-owned adapter calls the bundled model implementation, not arbitrary
commands. References are immutable, stack/config bound and complete before use.
"""
import argparse
import csv
import hashlib
import importlib
import importlib.metadata
import json
import math
import os
from pathlib import Path
import platform
import re
import sys
import tempfile
import time
import uuid

PROTOCOL_OUT = None

MODELS = ("gpt3", "llama2")
TOKEN = re.compile(r"^[a-zA-Z0-9][a-zA-Z0-9_.-]{0,63}$")
METRICS = ("loss", "grad_norm", "checksum")


def memory_mode():
    mode = os.environ.get("CATMONITOR_MODEL_MEMORY_MODE", "standard")
    if mode not in ("standard", "checkpointed"):
        raise ValueError("model memory mode must be standard or checkpointed")
    return mode


def sdc_tolerances():
    result = {key: float(os.environ.get("CATMONITOR_MODEL_" + key.upper() + "_TOLERANCE", default))
              for key, default in (("loss", "0.01"), ("grad", "0.1"), ("checksum", "0.1"))}
    if any(not math.isfinite(value) or not 0 <= value <= 1 for value in result.values()):
        raise ValueError("SDC tolerances must be finite and in [0,1]")
    return result


def canonical_hash(value):
    return hashlib.sha256(json.dumps(value, sort_keys=True, separators=(",", ":"),
                                     allow_nan=False).encode()).hexdigest()


def validate_request(model, action, reference, devices):
    if model not in MODELS or action not in ("create_reference", "compare"):
        raise ValueError("select gpt3/llama2 and create_reference/compare")
    if not isinstance(reference, str) or not TOKEN.fullmatch(reference):
        raise ValueError("reference ID must be a safe name, not a path")
    if not devices or any(type(d) is not int or d < 0 for d in devices):
        raise ValueError("explicit non-negative logical devices are required")
    if len(set(devices)) != len(devices):
        raise ValueError("duplicate logical device")
    if action == "create_reference" and len(devices) != 1:
        raise ValueError("reference generation requires exactly one logical device")


def step_count(config):
    count, warmup = config.get("run_count"), config.get("num_warmup", 5)
    if type(count) is not int or count <= 0 or type(warmup) is not int or warmup < 0:
        raise ValueError("invalid model step counts")
    return count + warmup


def validate_records(records, expected):
    if not isinstance(records, dict) or set(records) != {
            "step_" + str(i) for i in range(1, expected + 1)}:
        raise ValueError("reference step coverage is incomplete or unexpected")
    for record in records.values():
        if not isinstance(record, dict):
            raise ValueError("reference record is not an object")
        for key in METRICS:
            value = record.get(key)
            if type(value) not in (int, float) or not math.isfinite(value):
                raise ValueError("reference has missing/non-finite " + key)


def read_json(path):
    with Path(path).open(encoding="utf-8") as source:
        return json.load(source)


def write_json(path, value):
    with Path(path).open("x", encoding="utf-8") as target:
        json.dump(value, target, sort_keys=True, allow_nan=False)
        target.flush()
        os.fsync(target.fileno())


class ModelPreflightError(ValueError):
    def __init__(self, code, message):
        super().__init__(message)
        self.preflight_code = code


class ReferenceMissingError(FileNotFoundError):
    preflight_code = "reference_missing"


class ReferenceStore:
    def __init__(self, root, create=True):
        self.root = Path(root)
        if not self.root.is_absolute():
            raise ValueError("administrator reference root must be absolute")
        if create:
            self.root.mkdir(parents=True, exist_ok=True)

    def path(self, name):
        if not isinstance(name, str) or not TOKEN.fullmatch(name):
            raise ValueError("invalid reference ID")
        path = self.root / name
        if path.is_symlink():
            raise ValueError("reference must not be a symlink")
        return path

    def load(self, name, identity, expected_steps):
        path = self.path(name)
        try:
            manifest = read_json(path / "manifest.json")
        except FileNotFoundError as exc:
            raise ReferenceMissingError("reference has not been generated") from exc
        if type(manifest.get("schema_version")) is not int or manifest.get("schema_version") != 1 or manifest.get("complete") is not True:
            raise ModelPreflightError("reference_incomplete", "reference is not complete")
        if manifest.get("identity") != identity or manifest.get("steps") != expected_steps:
            raise ModelPreflightError("reference_mismatch", "reference configuration/software stack does not match")
        filename = manifest.get("baseline_file")
        if not isinstance(filename, str) or not re.fullmatch(
                r"(?:gpt3|llama2)/[0-9a-f]{16}\.json", filename):
            raise ValueError("unsafe reference baseline path")
        baseline = path / filename
        if baseline.is_symlink() or baseline.parent.is_symlink():
            raise ValueError("reference baseline must not be a symlink")
        data = baseline.read_bytes()
        if hashlib.sha256(data).hexdigest() != manifest.get("baseline_sha256"):
            raise ValueError("reference contents changed")
        validate_records(json.loads(data), expected_steps)
        return path

    def publish(self, name, stage, identity, steps, job_id):
        final = self.path(name)
        if final.exists():
            raise ValueError("reference ID already exists; overwriting is prohibited")
        files = list(Path(stage).glob(identity["model"] + "/*.json"))
        if len(files) != 1:
            raise ValueError("expected exactly one model reference file")
        validate_records(read_json(files[0]), steps)
        write_json(Path(stage) / "manifest.json", dict(
            schema_version=1, complete=True, identity=identity, steps=steps,
            generation_job=job_id,
            baseline_file=files[0].relative_to(stage).as_posix(),
            baseline_sha256=hashlib.sha256(files[0].read_bytes()).hexdigest()))
        # Atomic publication, with a no-overwrite lock. Cancel leaves only an
        # uncommitted staging directory; it can never be selected as a reference.
        lock = self.root / (".publish-" + name)
        lock.mkdir()
        try:
            if final.exists():
                raise ValueError("reference ID was concurrently published")
            Path(stage).rename(final)
        finally:
            lock.rmdir()
        return final


def validate_model_result(result, model, action, config):
    if not isinstance(result, dict) or result.get("error_records"):
        raise ValueError("model reported errors")
    rows = result.get("case_statistics")
    if not isinstance(rows, list) or len(rows) != 1:
        raise ValueError("model must produce exactly one complete case result")
    row = rows[0]
    if row.get("task") != model or row.get("result") is not True:
        raise ValueError("model result is not PASS")
    if type(row.get("err_count")) is not int or row["err_count"] != 0:
        raise ValueError("model error count is not zero")
    if row.get("run_count") != config["run_count"]:
        raise ValueError("model run count differs from selected configuration")
    metrics = row.get("metrics", {})
    expected = step_count(config)
    mode, key = ("save", "sdc_baseline_saved_steps") if action == "create_reference" else (
        "compare", "sdc_baseline_checked_steps")
    if metrics.get("sdc_baseline_mode") != mode or metrics.get(key) != expected:
        raise ValueError("model did not save/compare every expected training step")
    if metrics.get("sdc_failure_count") != 0 or metrics.get("trace_steps") != expected:
        raise ValueError("model SDC errors or incomplete trace")
    elapsed = row.get("exetime")
    if type(elapsed) not in (int, float) or not math.isfinite(elapsed) or elapsed < 0:
        raise ValueError("invalid model elapsed time")
    return row


def model_dependencies(model):
    if model not in MODELS:
        raise ValueError("unknown model")
    torch = importlib.import_module("torch")
    importlib.import_module("torch_npu")
    transformers = importlib.import_module("transformers")
    required = ("GPT2Config", "GPT2Model") if model == "gpt3" else ("LlamaConfig", "LlamaModel")
    for name in required:
        if not callable(getattr(transformers, name, None)):
            raise ValueError("transformers cannot import " + name)
    if memory_mode() == "checkpointed":
        model_class = getattr(transformers, required[1])
        if not getattr(model_class, "supports_gradient_checkpointing", False) or not callable(
                getattr(model_class, "gradient_checkpointing_enable", None)):
            raise ValueError("requested model does not support gradient checkpointing; no fallback")
    if not hasattr(torch, "bfloat16"):
        raise ValueError("torch.bfloat16 is unavailable; no dtype substitution")
    return torch


def model_parameter_count(config, factory, torch):
    """Count the actual bundled model on meta, without allocating NPU tensors."""
    instance = factory()
    instance.task_config = dict(config)
    instance._case = instance._build_run_case()
    instance._run_device = torch.device("meta")
    # These overrides belong only to this disposable metadata instance. The
    # real training instance retains its NPU initialization and sample bank.
    instance._prepare_runtime = lambda module: module.bfloat16
    instance._prime_sample_bank = lambda *args: None
    with torch.device("meta"):
        instance._create_model()
    count = sum(parameter.numel() for parameter in instance._network.parameters())
    if type(count) is not int or count <= 0:
        raise ValueError("cannot determine model parameter count")
    return count


def basic_memory_preflight(config, devices, factory):
    """A lower-bound screen, not a prediction of the complete training peak.

    Current model training uses BF16 parameters. Count weights, gradients and
    two minimum BF16 AdamW moments (8 bytes/parameter). Higher-precision states,
    activations, runtime context and temporary workspaces are NOT included.
    No chip-generation blacklist or historical OOM gate is applied.
    """
    torch = importlib.import_module("torch")
    if not devices or any(type(d) is not int or d < 0 for d in devices) or len(set(devices)) != len(devices):
        raise ValueError("explicit unique logical devices are required for the HBM screen")
    if config.get("dtype") != "bfloat16":
        raise ValueError("basic HBM screen requires the actual BF16 model configuration")
    count = model_parameter_count(config, factory, torch)
    required = count * 8
    query = getattr(getattr(torch, "npu", None), "mem_get_info", None)
    if not callable(query):
        raise ValueError("NPU HBM query is unavailable; basic memory screen could not run")
    rows = []
    for device in devices:
        free, total = query(device)
        if (type(free) is not int or type(total) is not int or
                free < 0 or total <= 0 or free > total):
            raise ValueError("invalid NPU HBM capacity for logical device " + str(device))
        rows.append(dict(device=device, free_bytes=free, total_bytes=total,
                         status="pass" if free >= required else "fail"))
    failed = [row for row in rows if row["status"] == "fail"]
    if failed:
        message = "Insufficient basic NPU HBM: " + "; ".join(
            f"logical {row['device']} free={row['free_bytes'] / 2**30:.2f} GiB, "
            f"basic minimum={required / 2**30:.2f} GiB" for row in failed)
    else:
        message = (f"Basic HBM screen passed (minimum {required / 2**30:.2f} GiB per device); "
                   "full training peak is unknown and training may still fail with OOM")
    return dict(status="fail" if failed else "pass", message=message,
                scope="basic_lower_bound", parameter_count=count,
                required_bytes=required, full_peak_known=False, devices=rows)


def is_npu_oom(error):
    message = str(error).lower()
    return ("npu out of memory" in message or "npu memory exhausted" in message or
            "acl_error_rt_memory_allocation" in message or
            (type(error).__name__ == "OutOfMemoryError" and
             "cpu" not in message and "cuda" not in message))


def load_runtime(model, generation):
    if os.environ.get("CATMONITOR_MODEL_SDC_ENABLED") != "true":
        raise ValueError("image was not built with --enable-model-sdc")
    if generation not in ("A2", "A3", "A5"):
        raise ValueError("invalid chip generation")
    model_dependencies(model)
    from ascend_npu_burn.case_catalog import load_config
    from ascend_npu_burn.benchmarks.model.model_base import ModelFactory
    # Import the complete package so model registration is performed.
    importlib.import_module("ascend_npu_burn.benchmarks.model")
    run_config = importlib.import_module("ascend_npu_burn.benchmarks.model.run_config")
    if not callable(getattr(run_config, "_runtime_root", None)):
        raise ValueError("image is missing explicit model-sdc-v1 compatibility patch")
    mode = memory_mode()
    if mode == "checkpointed":
        model_base = importlib.import_module("ascend_npu_burn.benchmarks.model.model_base")
        helper = getattr(model_base, "_catmonitor_model_memory_mode", None)
        if not callable(helper) or helper() != mode:
            raise ValueError("checkpointed mode requires explicit model-memory-v1 patch")
    config = {k: v for group in load_config(generation)["cases"]
              for k, v in group.items()}[model]
    if config.get("dtype") != "bfloat16":
        raise ValueError("upstream model only supports bfloat16")
    step_count(config)
    factory = lambda: ModelFactory.get_model_instance(model)
    if factory() is None:
        raise ValueError("model was not packaged/registered")
    package = Path(importlib.import_module("ascend_npu_burn").__file__).resolve().parent
    source = hashlib.sha256()
    for path in sorted(package.rglob("*")):
        # A job-owned frozen config is already bound below. Later edits to
        # unrelated deployment JSON must not invalidate that job/reference.
        suffixes = (".py", ".so") if os.environ.get("CATMONITOR_NPU_FROZEN_CONFIG_FILE") else (".py", ".json", ".so")
        if path.is_file() and path.suffix in suffixes:
            source.update(path.relative_to(package).as_posix().encode())
            source.update(hashlib.sha256(path.read_bytes()).digest())
    stack = dict(python=platform.python_version(), architecture=platform.machine(),
                 cann=os.environ.get("CATMONITOR_CANN_VERSION", ""),
                 compat_profile=os.environ.get("CATMONITOR_NPU_COMPAT_PROFILE", ""),
                 adapter_sha256=hashlib.sha256(Path(__file__).read_bytes()).hexdigest(),
                 package_sha256=source.hexdigest(),
                 **{name: importlib.metadata.version(name) for name in (
                     "torch", "torch-npu", "transformers")})
    if not stack["cann"] or not stack["compat_profile"]:
        raise ValueError("CANN version and explicit compatibility profile are required")
    identity = dict(model=model, generation=generation, config=config, stack=stack,
                    execution=dict(memory_mode=mode, device_strategy="independent_sequential"),
                    metric_protocol="parameter_signature_v2")
    identity["sdc_tolerances"] = sdc_tolerances()
    return config, identity, factory


def execute(model, action, reference, devices, store, output, config, identity,
            factory, job_id):
    validate_request(model, action, reference, devices)
    if not TOKEN.fullmatch(job_id):
        raise ValueError("unsafe job ID")
    expected = step_count(config)
    if output.exists():
        raise ValueError("job output directory must be new; stale reports prohibited")
    output.mkdir(parents=True)
    if action == "compare":
        baseline = store.load(reference, identity, expected)
    else:
        if store.path(reference).exists():
            raise ValueError("reference already exists; choose a new ID")
        baseline = store.root / (".staging-" + job_id)
        baseline.mkdir()
    traces = output / "traces"
    rows = []
    for device in devices:
        instance = factory()
        instance.task_config, instance.device = dict(config), device
        instance.create_reference = action == "create_reference"
        # The explicit model-sdc-v1 patch supplies writable roots without
        # replacing installed files or making the container root writable.
        os.environ["CATMONITOR_MODEL_REFERENCE_DIR"] = str(baseline)
        os.environ["CATMONITOR_MODEL_TRACE_DIR"] = str(traces)
        try:
            result = instance.run()
        except Exception as exc:
            if is_npu_oom(exc):
                detail = " ".join(str(exc).split())[:1024]
                raise RuntimeError(
                    f"NPU_HBM_OOM: model={model}, logical_device={device}; "
                    f"basic memory screening does not guarantee full training capacity; {detail}"
                ) from exc
            raise
        row = validate_model_result(result, model, action, config)
        rows.append(dict(device=device, **row))
    csv_path = output / "model-results.csv"
    with csv_path.open("x", newline="", encoding="utf-8") as target:
        writer = csv.DictWriter(target, fieldnames=(
            "job_id", "action", "reference_id", "task", "device_id", "run_count",
            "result", "err_count", "exetime", "checked_steps", "saved_steps"))
        writer.writeheader()
        for row in rows:
            writer.writerow(dict(job_id=job_id, action=action, reference_id=reference,
                task=model, device_id=row["device"], run_count=row["run_count"],
                result="PASS", err_count=0, exetime=row["exetime"],
                checked_steps=row["metrics"].get("sdc_baseline_checked_steps", 0),
                saved_steps=row["metrics"].get("sdc_baseline_saved_steps", 0)))
    report = dict(schema_version=1, job_id=job_id, model=model, action=action,
                  reference_id=reference, devices=devices, identity_sha256=canonical_hash(identity),
                  outcome="reference_generated" if action == "create_reference" else "sdc_passed",
                  expected_steps=expected,
                  checked_steps=expected * len(devices) if action == "compare" else 0,
                  saved_steps=expected if action == "create_reference" else 0,
                  errors=0, csv=str(csv_path), rows=rows)
    report["memory_mode"] = identity.get("execution", {}).get("memory_mode", "standard")
    report["effective_config"] = dict(config)
    report["sdc_tolerances"] = dict(identity["sdc_tolerances"])
    write_json(output / "model-result.json", report)
    if action == "create_reference":
        store.publish(reference, baseline, identity, expected, job_id)
    return report


def main(argv=None):
    argv = list(sys.argv[1:] if argv is None else argv)
    if argv == ["--check-imports"]:
        for model in MODELS:
            model_dependencies(model)
        from ascend_npu_burn.benchmarks.model.model_base import ModelFactory
        importlib.import_module("ascend_npu_burn.benchmarks.model")
        config_module = importlib.import_module("ascend_npu_burn.benchmarks.model.run_config")
        if not callable(getattr(config_module, "_runtime_root", None)):
            raise ValueError("model-sdc-v1 patch is missing")
        if any(ModelFactory.get_model_instance(model) is None for model in MODELS):
            raise ValueError("GPT3/Llama2 model implementation is missing")
        print("CATMONITOR_MODEL_IMPORT=PASS", file=PROTOCOL_OUT or sys.stdout)
        return 0
    parser = argparse.ArgumentParser(description="NPU Burn model reference/SDC adapter")
    parser.add_argument("--model", choices=MODELS, required=True)
    parser.add_argument("--action", choices=("create_reference", "compare"), required=True)
    parser.add_argument("--reference", required=True)
    parser.add_argument("--devices", required=True)
    parser.add_argument("--describe", action="store_true")
    args = parser.parse_args(argv)
    devices = [int(x) for x in args.devices.split(",")]
    validate_request(args.model, args.action, args.reference, devices)
    config, identity, factory = load_runtime(args.model, os.environ["NPU_BURN_CHIP_GENERATION"])
    store = ReferenceStore(os.environ.get("CATMONITOR_MODEL_REFERENCE_ROOT",
                                         "/var/lib/catmonitor/stress/model-references"),
                           create=not args.describe)
    if args.action == "compare":
        store.load(args.reference, identity, step_count(config))
    memory = basic_memory_preflight(config, devices, factory)
    if args.describe:
        if args.action == "create_reference" and store.path(args.reference).exists():
            raise ModelPreflightError("reference_exists", "reference ID already exists")
        print(json.dumps(dict(preflight=memory["status"], model=args.model, action=args.action,
             reference_id=args.reference, devices=devices, expected_steps=step_count(config),
             memory_mode=memory_mode(), device_strategy="independent_sequential",
             hardware_validation="NOT_RUN", identity_sha256=canonical_hash(identity),
             memory_preflight=memory)),
              file=PROTOCOL_OUT or sys.stdout)
        return 0
    if memory["status"] != "pass":
        raise ValueError(memory["message"])
    root = Path(os.environ["NPU_BURN_OUTPUT_DIR"])
    if not root.is_absolute():
        raise ValueError("output root must be absolute")
    job_id = os.environ.get("CATMONITOR_MODEL_JOB_ID", "model-" + uuid.uuid4().hex)
    if not TOKEN.fullmatch(job_id):
        raise ValueError("unsafe job ID")
    execute(args.model, args.action, args.reference, devices, store, root / job_id,
            config, identity, factory, job_id)
    return 0


def cli_main(argv=None):
    try:
        return main(argv)
    except (ValueError, OSError, ImportError, KeyError, RuntimeError) as exc:
        # Metadata-only failures stay RC != 0, but expose a typed reason. Do
        # not infer missing/mismatched references by searching stderr strings.
        args = sys.argv[1:] if argv is None else argv
        if "--describe" in args:
            code = getattr(exc, "preflight_code", "dependency_unavailable" if isinstance(exc, ImportError) else "preflight_failed")
            print(json.dumps(dict(preflight="fail", code=code, message=str(exc),
                                  hardware_validation="NOT_RUN")), file=PROTOCOL_OUT or sys.stdout)
        print("ERROR: model SDC: " + str(exc), file=sys.stderr)
        return 1


if __name__ == "__main__":
    # Native import/destructor diagnostics must not contaminate describe JSON.
    PROTOCOL_OUT = os.fdopen(os.dup(1), "w")
    os.dup2(2, 1)
    overlay = Path("/opt/catmonitor/npuburn-python")
    if not (overlay / "ascend_npu_burn").is_dir():
        raise SystemExit("ERROR: image-owned NPU Burn overlay is missing")
    sys.path.insert(0, str(overlay))
    raise SystemExit(cli_main())
