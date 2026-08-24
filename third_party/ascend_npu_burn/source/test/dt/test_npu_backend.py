#!/usr/bin/env python3
# -*- coding: utf-8 -*-
# Copyright 2026 Huawei Technologies Co., Ltd
import pytest
from unittest.mock import MagicMock, patch


@pytest.mark.unit
class TestNpuBackend:

    @patch("common.npu_backend.torch_npu")
    def test_get_free_memory(self, mock_torch_npu):
        mock_props = MagicMock()
        mock_props.total_memory = 64 * 1024 ** 3
        mock_torch_npu.npu.get_device_properties.return_value = mock_props
        mock_torch_npu.npu.memory_allocated.return_value = 10 * 1024 ** 3

        from common.npu_backend import get_free_memory
        result = get_free_memory(0)
        assert result == 54 * 1024 ** 3