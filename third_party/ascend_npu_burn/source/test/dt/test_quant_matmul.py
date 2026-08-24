#!/usr/bin/env python3
# -*- coding: utf-8 -*-
# Copyright 2026 Huawei Technologies Co., Ltd
import pytest
from unittest.mock import MagicMock, patch

from benchmarks.op.quant_matmul import QuantMatmulOp
from benchmarks.op.strategy.quant_matmul_a3 import QuantMatmulA3Strategy
from benchmarks.op.strategy.chip_strategy import ChipGenerationStrategy
from benchmarks.op.strategy.quant_matmul_a5 import _mx_quant_run, _other_quant_run
from common.enum import DtypeStr, TensorType, DetectType, ChipGeneration


def _make_op(device=0):
    op = QuantMatmulOp()
    op.strategy = None
    op.device = device
    return op


@pytest.mark.unit
class TestSetSubDetectTypes:
    def test_sub_detect_types(self):
        op = QuantMatmulOp()
        op.set_sub_detect_types()
        assert op.sub_detect_types == [DetectType.SDC.value]


@pytest.mark.unit
class TestParseCases:
    def test_invalid_mkn_returns_empty(self):
        op = _make_op()
        op.task_config = {"dtype": ["int8"], "MKN": [[1024, 512]]}
        assert op.parse_cases() == []

    def test_strategy_delegation(self):
        op = QuantMatmulOp()
        mock_strategy = MagicMock()
        mock_strategy.parse_cases.return_value = [{"dtype": "int8"}]
        op.strategy = mock_strategy
        op.task_config = {}
        assert op.parse_cases() == [{"dtype": "int8"}]
        mock_strategy.parse_cases.assert_called_once_with(op.task_config)


@pytest.mark.unit
class TestCreateTensorInfo:
    def test_no_strategy_returns_empty(self):
        op = _make_op()
        case = {"dtype": "int8", "pattern": "gauss_random", "m": 1024, "k": 512, "n": 256}
        assert op.create_tensor_info(case) == {}

    def test_strategy_delegation(self):
        op = QuantMatmulOp()
        mock_strategy = MagicMock()
        mock_strategy.create_tensor_info.return_value = {"x1": MagicMock()}
        op.strategy = mock_strategy
        case = {"dtype": "int8", "pattern": "gauss_random", "m": 1024, "k": 512, "n": 256}
        op.create_tensor_info(case)
        mock_strategy.create_tensor_info.assert_called_once_with(case, op.device)


@pytest.mark.unit
class TestRun:
    def test_no_strategy_returns_none(self):
        op = _make_op()
        assert op.run({"x1": MagicMock()}) is None

    def test_strategy_delegation(self):
        op = QuantMatmulOp()
        mock_strategy = MagicMock()
        mock_strategy.run.return_value = "strategy_result"
        op.strategy = mock_strategy
        x1 = MagicMock()
        assert op.run({"x1": x1}) == "strategy_result"
        mock_strategy.run.assert_called_once_with({"x1": x1})


@pytest.mark.unit
class TestMxQuantRun:
    def test_mx_quant_run(self):
        with (
            patch("benchmarks.op.strategy.quant_matmul_a5.torch_npu") as mock_torch_npu,
            patch("benchmarks.op.strategy.quant_matmul_a5.DTYPE_MAPPING", {"float4_e2m1fn_x2": "mock_dtype"}),
        ):
            mock_torch_npu.npu_quant_matmul.return_value = "output"
            mock_x2 = MagicMock()
            mock_scale = MagicMock()
            mock_out = MagicMock(dtype="bfloat16")
            tensor_mapping = {
                "x1": MagicMock(dtype="float4_e2m1fn_x2"),
                "x2": mock_x2,
                "scale": mock_scale,
                "per_token_scale": MagicMock(),
                "out": mock_out,
                "x1_real_data": "float4_e2m1fn_x2",
                "x2_real_data": "float4_e2m1fn_x2",
            }
            result = _mx_quant_run(tensor_mapping)
            mock_x2.transpose.assert_called_with(-1, -2)
            mock_scale.transpose.assert_called_with(0, 1)
            assert result == "output"


@pytest.mark.unit
class TestOtherQuantRun:
    def test_other_quant_run(self):
        with patch("benchmarks.op.strategy.quant_matmul_a5.torch_npu") as mock_torch_npu:
            mock_torch_npu.npu_quant_matmul.return_value = "output"
            mock_x2 = MagicMock()
            mock_scale = MagicMock()
            mock_out = MagicMock(dtype="bfloat16")
            tensor_mapping = {
                "x1": MagicMock(),
                "x2": mock_x2,
                "scale": mock_scale,
                "per_token_scale": MagicMock(),
                "out": mock_out,
            }
            result = _other_quant_run(tensor_mapping)
            mock_x2.transpose.assert_not_called()
            mock_scale.transpose.assert_not_called()
            assert result == "output"


@pytest.mark.unit
class TestQuantMatmulA3Strategy:
    def test_parse_cases(self):
        strategy = QuantMatmulA3Strategy()
        config = {"dtype": ["int8"], "pattern": ["gauss_random"], "M": 1024, "K": 512, "N": 256}
        cases = strategy.parse_cases(config)
        assert len(cases) == 1
        assert cases[0]["dtype"] == "int8"

    def test_parse_cases_empty(self):
        strategy = QuantMatmulA3Strategy()
        assert strategy.parse_cases({}) == []

    def test_create_tensor_info_int8(self):
        strategy = QuantMatmulA3Strategy()
        case = {"dtype": DtypeStr.INT8.value, "pattern": "gauss_random", "m": 1024, "k": 512, "n": 256}
        info = strategy.create_tensor_info(case, device=0)
        assert info["x1"].shape == [1024, 512]
        assert info["x2"].shape == [512, 256]
        assert info["out"].tensor_type == TensorType.OUTPUT.value

    def test_create_tensor_info_int32(self):
        strategy = QuantMatmulA3Strategy()
        case = {"dtype": DtypeStr.INT32.value, "pattern": "gauss_random", "m": 1024, "k": 512, "n": 256}
        info = strategy.create_tensor_info(case, device=0)
        assert info["x1"].shape == [1024, 512 // 8]
        assert info["x2"].shape == [512, 256 // 8]

    def test_run(self):
        with patch("benchmarks.op.strategy.quant_matmul_a3.torch_npu") as mock_torch_npu:
            mock_torch_npu.npu_quant_matmul.return_value = "a3_output"
            strategy = QuantMatmulA3Strategy()
            mock_out = MagicMock()
            result = strategy.run({"x1": MagicMock(), "x2": MagicMock(), "scale": MagicMock(), "out": mock_out})
            assert result == "a3_output"


@pytest.mark.unit
class TestChipGenerationStrategyRegistration:
    def test_a3_strategy_registered(self):
        ChipGenerationStrategy.register("QuantMatmulOp", ChipGeneration.A3.value, QuantMatmulA3Strategy)
        strategy = ChipGenerationStrategy.get_strategy("QuantMatmulOp", ChipGeneration.A3.value)
        assert isinstance(strategy, QuantMatmulA3Strategy)

    def test_nonexistent_returns_none(self):
        assert ChipGenerationStrategy.get_strategy("QuantMatmulOp", "NONEXISTENT") is None
