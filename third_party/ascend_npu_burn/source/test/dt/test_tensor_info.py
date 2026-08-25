#!/usr/bin/env python3
# -*- coding: utf-8 -*-
# Copyright 2026 Huawei Technologies Co., Ltd
import pytest
import torch
from unittest.mock import MagicMock, patch

from common.tensor_info import TensorInfo, sum_tensor_size, sum_input_tensor_size, create_tensor
from common.enum import TensorType


@pytest.mark.unit
class TestTensorInfoCreation:

    def test_default_tensor_type_is_input(self):
        info = TensorInfo(shape=[10, 10], dtype="float32")
        assert info.tensor_type == TensorType.INPUT.value

    def test_output_tensor_type(self):
        info = TensorInfo(shape=[10, 10], dtype="float32", tensor_type=TensorType.OUTPUT.value)
        assert info.tensor_type == TensorType.OUTPUT.value

    def test_dtype_mapping_float32(self):
        info = TensorInfo(shape=[10, 10], dtype="float32")
        assert info.dtype == torch.float32

    def test_device_id_default(self):
        info = TensorInfo(shape=[10, 10], dtype="float32")
        assert info.device_id == 0

    def test_device_id_custom(self):
        info = TensorInfo(shape=[10, 10], dtype="float32", device=3)
        assert info.device_id == 3

    def test_pattern_default(self):
        info = TensorInfo(shape=[10, 10], dtype="float32")
        assert info.pattern == ""

    def test_pattern_custom(self):
        info = TensorInfo(shape=[10, 10], dtype="float32", pattern="gauss_random")
        assert info.pattern == "gauss_random"

    def test_creator_default(self):
        info = TensorInfo(shape=[10, 10], dtype="float32")
        assert info.creator is None

    def test_creator_custom(self):
        def my_creator(shape, dtype, device):
            return torch.zeros(shape, dtype=dtype)

        info = TensorInfo(shape=[10, 10], dtype="float32", creator=my_creator)
        assert info.creator is my_creator


@pytest.mark.unit
class TestTensorInfoCalcSize:

    def test_float32_2d(self):
        info = TensorInfo(shape=[1024, 1024], dtype="float32")
        assert info.calc_tensor_size() == 1024 * 1024 * 4

    def test_float16_2d(self):
        info = TensorInfo(shape=[512, 512], dtype="float16")
        assert info.calc_tensor_size() == 512 * 512 * 2

    def test_bfloat16_2d(self):
        info = TensorInfo(shape=[256, 256], dtype="bfloat16")
        assert info.calc_tensor_size() == 256 * 256 * 2

    def test_int8_2d(self):
        info = TensorInfo(shape=[100, 200], dtype="int8")
        assert info.calc_tensor_size() == 100 * 200 * 1

    def test_int16_2d(self):
        info = TensorInfo(shape=[50, 50], dtype="int16")
        assert info.calc_tensor_size() == 50 * 50 * 2

    def test_int32_2d(self):
        info = TensorInfo(shape=[30, 30], dtype="int32")
        assert info.calc_tensor_size() == 30 * 30 * 4

    def test_int64_2d(self):
        info = TensorInfo(shape=[20, 20], dtype="int64")
        assert info.calc_tensor_size() == 20 * 20 * 8

    def test_uint8_2d(self):
        info = TensorInfo(shape=[40, 40], dtype="uint8")
        assert info.calc_tensor_size() == 40 * 40 * 1

    def test_bool_2d(self):
        info = TensorInfo(shape=[10, 10], dtype="bool")
        assert info.calc_tensor_size() == 10 * 10 * 1

    def test_1d_tensor(self):
        info = TensorInfo(shape=[1000], dtype="float32")
        assert info.calc_tensor_size() == 1000 * 4

    def test_3d_tensor(self):
        info = TensorInfo(shape=[2, 3, 4], dtype="float32")
        assert info.calc_tensor_size() == 2 * 3 * 4 * 4

    def test_4d_tensor(self):
        info = TensorInfo(shape=[1, 3, 224, 224], dtype="float32")
        assert info.calc_tensor_size() == 1 * 3 * 224 * 224 * 4

    def test_5d_tensor(self):
        info = TensorInfo(shape=[1, 3, 10, 224, 224], dtype="float16")
        assert info.calc_tensor_size() == 1 * 3 * 10 * 224 * 224 * 2


@pytest.mark.unit
class TestTensorInfoCreate:

    def test_output_type_returns_self(self):
        info = TensorInfo(shape=[10, 10], dtype="float32", tensor_type=TensorType.OUTPUT.value)
        result = info.create()
        assert result is info

    def test_custom_creator_called(self):
        calls = []

        def my_creator(shape, dtype, device):
            calls.append((shape, dtype, device))
            return torch.zeros(shape, dtype=dtype)

        info = TensorInfo(shape=[5, 5], dtype="float32", device=0, creator=my_creator)
        tensor = info.create()
        assert len(calls) == 1
        assert calls[0][0] == [5, 5]
        assert calls[0][1] == torch.float32
        assert tensor.shape == (5, 5)


@pytest.mark.unit
class TestSumTensorSize:

    def test_single_tensor(self):
        tensor_info = {"a": TensorInfo(shape=[100, 100], dtype="float32")}
        assert sum_tensor_size(tensor_info) == 100 * 100 * 4

    def test_multiple_tensors(self):
        tensor_info = {
            "a": TensorInfo(shape=[100, 100], dtype="float32"),
            "b": TensorInfo(shape=[50, 50], dtype="float16"),
        }
        assert sum_tensor_size(tensor_info) == 100 * 100 * 4 + 50 * 50 * 2

    def test_skips_non_tensor_info(self):
        tensor_info = {
            "a": TensorInfo(shape=[100, 100], dtype="float32"),
            "flag": True,
            "count": 42,
        }
        assert sum_tensor_size(tensor_info) == 100 * 100 * 4

    def test_empty_dict(self):
        assert sum_tensor_size({}) == 0


@pytest.mark.unit
class TestSumInputTensorSize:

    def test_only_input_counted(self):
        tensor_info = {
            "input": TensorInfo(shape=[100, 100], dtype="float32", tensor_type=TensorType.INPUT.value),
            "output": TensorInfo(shape=[100, 100], dtype="float32", tensor_type=TensorType.OUTPUT.value),
        }
        assert sum_input_tensor_size(tensor_info) == 100 * 100 * 4

    def test_all_input(self):
        tensor_info = {
            "a": TensorInfo(shape=[50, 50], dtype="float32"),
            "b": TensorInfo(shape=[50, 50], dtype="float32"),
        }
        assert sum_input_tensor_size(tensor_info) == 50 * 50 * 4 * 2

    def test_all_output(self):
        tensor_info = {
            "a": TensorInfo(shape=[100, 100], dtype="float32", tensor_type=TensorType.OUTPUT.value),
        }
        assert sum_input_tensor_size(tensor_info) == 0

    def test_empty_dict(self):
        assert sum_input_tensor_size({}) == 0


@pytest.mark.unit
class TestCreateTensor:

    def test_output_tensor_info_passes_through(self):
        tensor_info = {
            "result": TensorInfo(shape=[10, 10], dtype="float32", tensor_type=TensorType.OUTPUT.value),
        }
        with patch("common.tensor_info.get_free_memory", return_value=10 * 1024 ** 3):
            result = create_tensor(tensor_info, device=0, off_line=True)
        assert "result" in result
        assert result["result"].shape == [10, 10]

    def test_non_tensor_info_values_pass_through(self):
        tensor_info = {
            "flag": True,
            "count": 42,
        }
        with patch("common.tensor_info.get_free_memory", return_value=10 * 1024 ** 3):
            result = create_tensor(tensor_info, device=0, off_line=True)
        assert result["flag"] is True
        assert result["count"] == 42

    def test_insufficient_memory_raises(self):
        tensor_info = {
            "a": TensorInfo(shape=[10000, 10000], dtype="float64"),
        }
        with patch("common.tensor_info.get_free_memory", return_value=100):
            with pytest.raises(Exception, match="Not enough memory"):
                create_tensor(tensor_info, device=0, off_line=True)

    def test_off_line_uses_full_memory(self):
        tensor_info = {
            "a": TensorInfo(shape=[10, 10], dtype="float32"),
        }
        with patch("common.tensor_info.get_free_memory", return_value=10 * 1024 ** 3) as mock_mem:
            with patch("common.tensor_info.data_load") as mock_dl:
                mock_dl.create_tensor.return_value = torch.randn(10, 10)
                create_tensor(tensor_info, device=0, off_line=True)
                mock_mem.assert_called_once_with(0)
