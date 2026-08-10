#!/usr/bin/env python3
# -*- coding: utf-8 -*-
# Copyright 2026 Huawei Technologies Co., Ltd
#
# Licensed under the Apache License, Version 2.0 (the "License");
# you may not use this file except in compliance with the License.
# You may obtain a copy of the License at
#
# http://www.apache.org/licenses/LICENSE-2.0
#
# Unless required by applicable law or agreed to in writing, software
# distributed under the License is distributed on an "AS IS" BASIS,
# WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
# See the License for the specific language governing permissions and
# limitations under the License.

import os
import glob
import time
from pathlib import Path
from setuptools import setup, find_packages

BUILD_DIR = Path(__file__).parent.resolve()
if (BUILD_DIR / "ascend_npu_burn").exists():
    PROJECT_ROOT = BUILD_DIR.resolve()
else:
    PROJECT_ROOT = BUILD_DIR.parent.resolve()

RELATIVE_ROOT = os.path.relpath(PROJECT_ROOT, BUILD_DIR)

ASCEND_NPU_BURN_DIR = PROJECT_ROOT / "ascend_npu_burn"
CSRC_DIR = ASCEND_NPU_BURN_DIR / "custom_ops" / "csrc"
VERSION_FILE = "version.info"

EXT_MODULES = []
TORCH_VERSION = ""

try:
    import torch

    TORCH_VERSION = "+torch-" + torch.__version__.split("+")[0]
except ImportError:
    pass

try:
    import torch_npu
    from torch.utils.cpp_extension import BuildExtension
    from torch_npu.utils.cpp_extension import NpuExtension

    PYTORCH_NPU_INSTALL_PATH = os.path.dirname(os.path.abspath(torch_npu.__file__))
    USE_NINJA = os.getenv('USE_NINJA') == '1'

    source_files = glob.glob(os.path.join(str(CSRC_DIR), "*.cpp"), recursive=True)
    source_files = [os.path.relpath(f, str(BUILD_DIR)) for f in source_files]

    ext = NpuExtension(
        name="ascend_npu_burn.custom_ops.custom_ops_lib",
        sources=source_files,
        extra_compile_args=[
            '-I' + os.path.join(PYTORCH_NPU_INSTALL_PATH, "include/third_party/acl/inc"),
        ],
    )
    EXT_MODULES.append(ext)

    CMDCLASS = {"build_ext": BuildExtension.with_options(use_ninja=USE_NINJA)}
except ImportError:
    CMDCLASS = {}


def get_version_from_config():
    config_path = PROJECT_ROOT / "service_config.ini"
    if config_path.exists():
        try:
            with open(config_path, 'r', encoding='utf-8') as f:
                for line in f:
                    line = line.strip()
                    if line.startswith("ascend_npu_burn:"):
                        return line.split(":", 1)[1].strip()
        except Exception:
            pass
    return "26.1.0"


def write_version_file():
    version = get_version_from_config()
    version_file_path = ASCEND_NPU_BURN_DIR / VERSION_FILE
    build_timestamp = time.strftime('%Y-%m-%d %H:%M:%S', time.localtime())
    with open(version_file_path, 'w', encoding='utf-8') as f:
        f.write(f"{version}\n")
        f.write(f"{build_timestamp}\n")
    return version


write_version_file()

setup(
    name="ascend_npu_burn",
    version=get_version_from_config() + TORCH_VERSION,
    description="Ascend NPU Burn - Unified package with custom operators and benchmark tools",
    author="Huawei Technologies Co., Ltd",
    packages=find_packages(where=RELATIVE_ROOT),
    package_dir={"": RELATIVE_ROOT},
    package_data={
        "": [
            "*.json",
            "*.sh",
            "*.py",
            "**/*.json",
            "**/*.sh",
            "**/*.py",
            "**/*.cpp",
            "**/*.h",
            "**/*.hpp",
        ],
    },
    include_package_data=True,
    ext_modules=EXT_MODULES if EXT_MODULES else None,
    cmdclass=CMDCLASS,
    entry_points={
        "console_scripts": [
            "npu-burn=ascend_npu_burn.npu_burn:main",
        ],
    },
)
