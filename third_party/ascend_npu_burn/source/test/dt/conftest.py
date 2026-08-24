#!/usr/bin/env python3
# -*- coding: utf-8 -*-
# Copyright 2026 Huawei Technologies Co., Ltd
import sys
import pathlib
from unittest.mock import MagicMock

import pytest
import torch

PROJECT_ROOT = pathlib.Path(__file__).parent.parent.parent.resolve()
SOURCE_DIR = PROJECT_ROOT / "ascend_npu_burn"
for p in [str(PROJECT_ROOT), str(SOURCE_DIR)]:
    if p not in sys.path:
        sys.path.append(p)

torch_npu_mock = MagicMock()
torch_npu_mock.npu.matmul.allow_hf32 = False
torch_npu_mock.npu.get_device_properties.return_value = MagicMock(total_memory=64 * 1024**3)
torch_npu_mock.npu.memory_allocated.return_value = 0
torch_npu_mock.npu.is_available.return_value = False
torch_npu_mock.npu.Stream = MagicMock
torch_npu_mock.npu.default_stream = MagicMock
torch_npu_mock.npu.current_stream = MagicMock
torch_npu_mock.npu.Event = MagicMock
torch_npu_mock.npu.synchronize = MagicMock
torch_npu_mock.npu.set_device = MagicMock
torch_npu_mock.npu.empty_cache = MagicMock
torch_npu_mock.npu.config.allow_internal_format = False
torch_npu_mock.npu.set_compile_mode = MagicMock
torch_npu_mock.npu_fusion_attention = MagicMock()
torch_npu_mock.npu_quant_matmul = MagicMock()
torch_npu_mock.profiler.profile = MagicMock
torch_npu_mock.profiler.ProfilerActivity = MagicMock
torch_npu_mock.profiler._ExperimentalConfig = MagicMock
torch_npu_mock.profiler.ProfilerLevel = MagicMock
sys.modules.setdefault("torch_npu", torch_npu_mock)
sys.modules.setdefault("torch_npu.profiler", torch_npu_mock.profiler)

if not hasattr(torch, "npu"):
    torch.npu = torch_npu_mock.npu

sys.modules.setdefault("torchair", MagicMock())
sys.modules.setdefault("torchair.ge", MagicMock())
sys.modules.setdefault("torchair.configs", MagicMock())
sys.modules.setdefault("torchair.configs.compiler_config", MagicMock())

mock_custom_ops = MagicMock()
sys.modules.setdefault("custom_ops", mock_custom_ops)
sys.modules.setdefault("ascend_npu_burn.custom_ops", mock_custom_ops)
sys.modules.setdefault("ascend_npu_burn.custom_ops.custom_ops_lib", MagicMock())


def pytest_configure(config):
    config.addinivalue_line("markers", "unit: Unit tests (no NPU required)")
    config.addinivalue_line("markers", "npu: Tests requiring NPU device")


@pytest.fixture
def npu_device():
    if not torch.npu.is_available():
        pytest.skip("NPU not available")
    return 0


@pytest.fixture
def clean_memory():
    yield
    if torch.npu.is_available():
        torch.npu.empty_cache()
