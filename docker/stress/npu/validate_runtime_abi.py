#!/usr/bin/env python3
"""Capture and compare NPU Burn builder/runtime package ABI metadata.

This deliberately uses distribution metadata instead of importing torch_npu.
The final import, HAL and custom-op checks require the host driver mount and
belong to fixed-container preflight, not a Docker build.
"""

from __future__ import annotations

import argparse
import importlib.metadata as metadata
import json
from pathlib import Path
import sys
import sysconfig


def package_version(name: str) -> str:
    try:
        return metadata.version(name)
    except metadata.PackageNotFoundError as error:
        raise SystemExit(f"ERROR: required runtime package metadata is unavailable: {name}") from error


def current_contract(cann_version: str) -> dict[str, str]:
    return {
        "python_major_minor": f"{sys.version_info.major}.{sys.version_info.minor}",
        "python_soabi": sysconfig.get_config_var("SOABI") or "",
        "torch_version": package_version("torch"),
        "torch_npu_version": package_version("torch-npu"),
        "cann_version": cann_version,
    }


def parse_args() -> argparse.Namespace:
    parser = argparse.ArgumentParser()
    subparsers = parser.add_subparsers(dest="command", required=True)

    capture = subparsers.add_parser("capture")
    capture.add_argument("--cann-version", required=True)
    capture.add_argument("--output", type=Path, required=True)

    validate = subparsers.add_parser("validate")
    validate.add_argument("--expected", type=Path, required=True)
    validate.add_argument("--cann-version", required=True)
    return parser.parse_args()


def main() -> None:
    args = parse_args()
    actual = current_contract(args.cann_version)
    if args.command == "capture":
        args.output.write_text(json.dumps(actual, sort_keys=True) + "\n", encoding="utf-8")
        for key, value in actual.items():
            print(f"CATMONITOR_BUILDER_{key.upper()}={value}")
        return

    expected = json.loads(args.expected.read_text(encoding="utf-8"))
    mismatches = [
        f"{key}: builder={expected.get(key)!r} runtime={actual.get(key)!r}"
        for key in sorted(actual)
        if expected.get(key) != actual.get(key)
    ]
    if mismatches:
        print("ERROR: builder/runtime ABI mismatch", file=sys.stderr)
        for mismatch in mismatches:
            print(f"  {mismatch}", file=sys.stderr)
        raise SystemExit(1)
    for key, value in actual.items():
        print(f"CATMONITOR_RUNTIME_{key.upper()}={value}")
    print("CATMONITOR_RUNTIME_ABI=PASS")


if __name__ == "__main__":
    main()
