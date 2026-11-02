"""Data-only job configuration resolver; importing this module probes no NPU.

The immutable schema comes from the bundled base config, not an externally
mounted deployment override. Torch API/strategy admission is still performed by
the ordinary runtime preflight after this resolver builds the frozen config.
"""
import copy
import json
import math
import sys
from pathlib import Path

MODEL_FIELDS = {
    "run_count", "num_warmup", "batch_size", "seq_len", "random_seed",
    "sample_pool", "num_classes", "vocab_size", "hidden_size",
    "num_hidden_layers", "num_attention_heads", "intermediate_size",
    "learning_rate", "dtype",
}
DIMENSIONS = {
    "shape", "MKN", "B", "N", "K", "k", "C", "H", "W", "input_data",
    "weight_data", "BSHN", "BNSH", "seq_len", "in.out", "batch_size",
    "head_num", "head_dim", "ESKN", "ND",
}
MATRIX_OPS = {
    "matmul", "matmul_atomic", "matmul_sdma", "matmul_sdma_jump",
    "cube_vector_sdma", "matmul_axpy", "matmul_pow",
}


def cases(config):
    return {name: value for group in config["cases"] for name, value in group.items()}


def positive(value, label, maximum=1048576):
    if type(value) is not int or not 1 <= value <= maximum:
        raise ValueError(label + " must be a bounded positive integer")
    return value


def integers(value, label, count=None):
    if not isinstance(value, list) or not value or len(value) > 128:
        raise ValueError(label + " must be a nonempty bounded array")
    if count is not None and len(value) != count:
        raise ValueError(label + " has the wrong rank")
    for item in value:
        positive(item, label)


def validate_dimension(value, template, label):
    if type(template) is int:
        positive(value, label)
    elif isinstance(template, list):
        if not isinstance(value, list) or not value or len(value) > 128:
            raise ValueError(label + " must be a nonempty bounded array")
        if isinstance(template[0], list):
            for item in value:
                validate_dimension(item, template[0], label)
        else:
            integers(value, label, None if label.rsplit(".", 1)[-1] in (
                "seq_len", "batch_size", "head_num", "head_dim") else len(template))
    else:
        raise ValueError("unsupported tensor shape schema: " + label)


def validate_operator(name, config):
    if name in MATRIX_OPS:
        for pair in config["shape"]:
            if len(pair) != 2:
                raise ValueError(name + " requires two matrix shapes")
            integers(pair[0], name, 2)
            integers(pair[1], name, 2)
            if pair[0][1] != pair[1][0]:
                raise ValueError(name + " matrix contraction dimensions do not match")
            if max(math.prod(pair[0]), math.prod(pair[1]), pair[0][0] * pair[1][1]) > 2**30:
                raise ValueError(name + " tensor is larger than the admitted element bound")
    if name in ("conv2d", "conv3d"):
        rank = 4 if name == "conv2d" else 5
        integers(config["input_data"], name + ".input_data", rank)
        integers(config["weight_data"], name + ".weight_data", rank)
        if config["input_data"][1] != config["weight_data"][1]:
            raise ValueError(name + " input/weight channels do not match")
        if any(a < b for a, b in zip(config["input_data"][2:], config["weight_data"][2:])):
            raise ValueError(name + " kernel is larger than the input")
        if max(math.prod(config["input_data"]), math.prod(config["weight_data"])) > 2**30:
            raise ValueError(name + " tensor is larger than the admitted element bound")
    if name == "fusion_attention":
        for b, s, h, n in config["BSHN"]:
            if h % n:
                raise ValueError("attention hidden dimension must be divisible by heads")
    combinations = len(config.get("dtype", [])) * len(config.get("pattern", []))
    if "shape" in config:
        combinations *= len(config["shape"])
    if name == "simplified_transformer":
        combinations *= math.prod(len(config[k]) for k in ("seq_len", "batch_size", "head_num", "head_dim"))
    if combinations > 512:
        raise ValueError("operator selection exceeds 512 case combinations")
    positive(config["run_count"], "run_count", 1000000)


def validate_model(config):
    if config.get("dtype") != "bfloat16":
        raise ValueError("models require bfloat16; no dtype substitution")
    for key in MODEL_FIELDS - {"dtype", "learning_rate", "num_warmup", "random_seed"}:
        positive(config[key], "model." + key, 1000000)
    for key in ("num_warmup", "random_seed"):
        if type(config[key]) is not int or not 0 <= config[key] <= 2147483647:
            raise ValueError("model." + key + " must be a non-negative integer")
    rate = config["learning_rate"]
    if type(rate) not in (int, float) or not math.isfinite(rate) or not 0 < rate <= 1:
        raise ValueError("learning_rate must be finite and in (0,1]")
    if config["hidden_size"] % config["num_attention_heads"]:
        raise ValueError("model hidden_size must be divisible by num_attention_heads")


def resolve(config, schema, generation, selected, parameters=None, model=None):
    result = copy.deepcopy(config)
    entries, declared = cases(result), cases(schema)
    parameters = parameters or {}
    if set(parameters) - {"operators", "model", "sdc_tolerances"}:
        raise ValueError("unknown NPU job parameter")
    overrides = parameters.get("operators", {})
    if not isinstance(overrides, dict) or ("operators" in parameters and not overrides):
        raise ValueError("operator parameters must be a nonempty object")
    effective = {}
    if model:
        if model not in ("gpt3", "llama2") or overrides:
            raise ValueError("model/operator parameter combination is invalid")
        training = parameters.get("model", {})
        if not isinstance(training, dict) or set(training) - MODEL_FIELDS:
            raise ValueError("unknown model training parameter")
        entries[model].update(copy.deepcopy(training))
        validate_model(entries[model])
        effective["model"] = {k: entries[model][k] for k in MODEL_FIELDS}
        tolerances = dict(loss=0.01, grad=0.1, checksum=0.1)
        supplied = parameters.get("sdc_tolerances", {})
        if not isinstance(supplied, dict) or set(supplied) - {"loss", "grad", "checksum"}:
            raise ValueError("unknown SDC tolerance")
        tolerances.update(supplied)
        if any(type(v) not in (int, float) or not math.isfinite(v) or not 0 <= v <= 1
               for v in tolerances.values()):
            raise ValueError("SDC tolerance must be finite and in [0,1]")
        effective["sdc_tolerances"] = tolerances
    else:
        if "model" in parameters or "sdc_tolerances" in parameters:
            raise ValueError("training/tolerance parameters require a model selection")
        if not selected or set(overrides) - set(selected):
            raise ValueError("parameters must target selected operators only")
        effective["operators"] = {}
        for name in selected:
            if name not in entries or name in ("gpt3", "llama2"):
                raise ValueError("invalid selected operator")
            if generation == "A2" and name == "quant_matmul":
                raise ValueError("no upstream A2 quant_matmul strategy")
            override = overrides.get(name, {})
            if not isinstance(override, dict) or set(override) - {"dtype", "shape", "dimensions", "pattern", "run_count"}:
                raise ValueError("unknown operator parameter")
            actual, template = entries[name], declared[name]
            for key in ("dtype", "pattern"):
                if key in override:
                    values = override[key]
                    if (not isinstance(values, list) or not values or len(values) > 32
                            or any(not isinstance(v, str) for v in values)
                            or len(set(values)) != len(values)
                            or set(values) - set(template.get(key, []))):
                        raise ValueError(name + " has unsupported " + key + "; no substitution")
                    actual[key] = list(values)
            dims = dict(override.get("dimensions", {}))
            if "shape" in override:
                if "shape" in dims:
                    raise ValueError("shape was provided twice")
                dims["shape"] = override["shape"]
            for key, value in dims.items():
                if key not in DIMENSIONS or key not in template:
                    raise ValueError(name + " has no tensor dimension " + key)
                validate_dimension(value, template[key], name + "." + key)
                actual[key] = copy.deepcopy(value)
            if "run_count" in override:
                actual["run_count"] = override["run_count"]
            validate_operator(name, actual)
            current = {k: copy.deepcopy(actual[k]) for k in ("dtype", "pattern", "run_count") if k in actual}
            current["dimensions"] = {k: copy.deepcopy(actual[k]) for k in DIMENSIONS if k in actual and k != "shape"}
            if "shape" in actual:
                current["shape"] = copy.deepcopy(actual["shape"])
            if not current["dimensions"]:
                del current["dimensions"]
            effective["operators"][name] = current
    return dict(config=result, generation=generation, effective=effective)


def main():
    data = sys.stdin.buffer.read(32 * 1024 + 1)
    if len(data) > 32 * 1024:
        raise ValueError("job parameter input exceeds 32 KiB")
    request = json.loads(data)
    sys.path.insert(0, "/opt/catmonitor/npuburn-python")
    from ascend_npu_burn.case_catalog import load_config, select_cases, merge_config
    generation = request["generation"]
    directory = request.get("directory") or None
    config = load_config(generation, directory)
    model = request.get("model") or None
    selected = [] if model else select_cases(generation, request["run_case"], request.get("group", ""), directory)
    schema = json.loads(Path("/usr/local/share/catmonitor/job-parameter-base.json").read_text())
    if generation == "A3":
        schema = merge_config(schema, json.loads(Path("/usr/local/share/catmonitor/job-parameter-a3.json").read_text()))
    result = resolve(config, schema, generation, selected, request.get("parameters"), model)
    print(json.dumps(result, sort_keys=True, separators=(",", ":"), allow_nan=False))


if __name__ == "__main__":
    try:
        main()
    except (ValueError, KeyError, TypeError, OSError) as exc:
        print("ERROR: job parameters: " + str(exc), file=sys.stderr)
        raise SystemExit(1)
