#!/usr/bin/env python3
"""Validate or copy NPU JSON configuration without touching an image or hardware."""
import argparse
import hashlib
import json
from pathlib import Path

FILES = ("base_config.json", "a3_config.json")
MODELS = ("gpt3", "llama2")


def invalid_json_constant(value):
    raise ValueError("non-finite constant is not valid configuration JSON: " + value)


def read_directory(directory):
    directory = Path(directory)
    if not directory.is_absolute() or not directory.is_dir():
        raise ValueError("NPU config directory must be an existing absolute directory")
    data = {}
    for name in FILES:
        path = directory / name
        if path.is_symlink() or not path.is_file():
            raise ValueError("complete config directory requires regular " + name)
        value = json.loads(path.read_text(encoding="utf-8"), parse_constant=invalid_json_constant)
        if not isinstance(value, dict) or not isinstance(value.get("cases"), list) or not value["cases"]:
            raise ValueError("config must contain a nonempty cases list: " + name)
        seen = set()
        for group in value["cases"]:
            if not isinstance(group, dict) or not group:
                raise ValueError("invalid case group: " + name)
            for case, settings in group.items():
                if case in seen or not isinstance(settings, dict):
                    raise ValueError("duplicate case or invalid settings: " + name)
                seen.add(case)
        data[name] = value
    return data


def model_settings(config):
    return {name: settings for group in config["cases"] for name, settings in group.items() if name in MODELS}


def validate_models(config):
    models = model_settings(config)
    if set(models) != set(MODELS):
        raise ValueError("base config must retain both gpt3 and llama2")
    for settings in models.values():
        for key in ("batch_size", "seq_len", "num_hidden_layers", "hidden_size",
                    "num_attention_heads", "intermediate_size", "run_count"):
            if type(settings.get(key)) is not int or settings[key] <= 0:
                raise ValueError("model " + key + " must be a positive integer")
        if type(settings.get("num_warmup")) is not int or settings["num_warmup"] < 0:
            raise ValueError("model num_warmup must be a nonnegative integer")
        if settings.get("dtype") != "bfloat16":
            raise ValueError("models require bfloat16; no dtype substitution")


def identity(directory):
    return {name: hashlib.sha256((Path(directory) / name).read_bytes()).hexdigest() for name in FILES}


def create_profile(source, output, batch_size, warmup=None, run_count=None):
    data = read_directory(source)
    validate_models(data["base_config.json"])
    for name, value, minimum in (("batch_size", batch_size, 1), ("num_warmup", warmup, 0), ("run_count", run_count, 1)):
        if value is not None and (type(value) is not int or value < minimum):
            raise ValueError("invalid " + name)
    output = Path(output)
    if not output.is_absolute() or output == Path(output.anchor) or output.exists():
        raise ValueError("output must be a new absolute child directory; no overwrite")
    for settings in model_settings(data["base_config.json"]).values():
        settings["batch_size"] = batch_size
        if warmup is not None:
            settings["num_warmup"] = warmup
        if run_count is not None:
            settings["run_count"] = run_count
    validate_models(data["base_config.json"])
    output.mkdir(mode=0o750, parents=True)
    (output / "base_config.json").write_text(json.dumps(data["base_config.json"], indent=2) + "\n", encoding="utf-8")
    # Preserve the entire A3 override byte-for-byte; this is an A2 variant,
    # not an edit to the upstream defaults or an A3 support statement.
    (output / "a3_config.json").write_bytes((Path(source) / "a3_config.json").read_bytes())
    return {"scope": "A2_config_variant", "config_dir": str(output),
            "files": identity(output), "models": model_settings(data["base_config.json"])}


def create_from_preset(source, output, preset_path):
    preset = json.loads(Path(preset_path).read_text(encoding="utf-8"),
                        parse_constant=invalid_json_constant)
    if not isinstance(preset, dict) or type(preset.get("schema_version")) is not int or preset["schema_version"] != 1:
        raise ValueError("unsupported NPU preset schema")
    if not isinstance(preset.get("name"), str) or not preset["name"] or preset.get("models") != list(MODELS):
        raise ValueError("preset must name both gpt3 and llama2")
    overrides = preset.get("model_overrides")
    if not isinstance(overrides, dict) or set(overrides) != {"batch_size", "num_warmup", "run_count"}:
        raise ValueError("preset overrides must contain only batch_size, num_warmup and run_count")
    mode = preset.get("model_memory_mode")
    if mode not in ("standard", "checkpointed"):
        raise ValueError("unsupported preset model memory mode")
    required = preset.get("required_model_parameters")
    if not isinstance(required, dict) or not required:
        raise ValueError("preset must declare its required model parameters")
    data = read_directory(source)
    validate_models(data["base_config.json"])
    for model, settings in model_settings(data["base_config.json"]).items():
        for key, expected in required.items():
            actual = settings.get(key)
            if type(actual) is not type(expected) or actual != expected:
                raise ValueError("source model does not match preset: " + model + "." + key)
    result = create_profile(source, output, overrides["batch_size"],
                            overrides["num_warmup"], overrides["run_count"])
    result.update(preset=preset["name"], model_memory_mode=mode)
    return result


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--validate-dir")
    parser.add_argument("--source-dir")
    parser.add_argument("--output-dir")
    parser.add_argument("--batch-size", type=int)
    parser.add_argument("--num-warmup", type=int)
    parser.add_argument("--run-count", type=int)
    parser.add_argument("--preset", help="Named configuration preset; validation history is not a new hardware PASS")
    args = parser.parse_args()
    try:
        if args.validate_dir:
            if any(value is not None for value in (args.source_dir, args.output_dir, args.batch_size, args.num_warmup, args.run_count, args.preset)):
                raise ValueError("--validate-dir cannot be combined with profile creation")
            data = read_directory(args.validate_dir)
            validate_models(data["base_config.json"])
            print(json.dumps(identity(args.validate_dir), sort_keys=True))
        elif args.preset:
            if not args.source_dir or not args.output_dir:
                raise ValueError("preset requires --source-dir and --output-dir")
            if any(value is not None for value in (args.batch_size, args.num_warmup, args.run_count)):
                raise ValueError("--preset cannot be combined with individual model overrides")
            print(json.dumps(create_from_preset(args.source_dir, args.output_dir, args.preset), sort_keys=True))
        else:
            if not args.source_dir or not args.output_dir or args.batch_size is None:
                raise ValueError("creation requires --source-dir, --output-dir and --batch-size")
            print(json.dumps(create_profile(args.source_dir, args.output_dir, args.batch_size,
                                            args.num_warmup, args.run_count), sort_keys=True))
    except (ValueError, OSError) as error:
        parser.exit(1, "ERROR: " + str(error) + "\n")


if __name__ == "__main__":
    main()
