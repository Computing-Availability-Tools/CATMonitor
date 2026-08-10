#!/usr/bin/env python3
# -*- coding: utf-8 -*-
# Copyright 2026 Huawei Technologies Co., Ltd
import pytest
from unittest.mock import MagicMock, patch

from benchmarks.op.base_matmul_sdma import (  # noqa: E402
    DummyStressor,
    BaseMatmulSdma,
)
from common.enum import TensorType  # noqa: E402


@pytest.mark.unit
class TestCreateTensorInfo:
    def _make_op(self):
        class _TestSdmaOp(BaseMatmulSdma):
            def __init__(self, device_id: int, *args, **kwargs):
                super().__init__(*args, **kwargs)
                self.device = device_id

        op = _TestSdmaOp(0)
        return op

    def test_valid_case(self):
        op = self._make_op()
        case = {
            "shape": [[1024, 1024], [1024, 1024]],
            "dtype": "float32",
            "pattern": "gauss_random",
        }
        result = op.create_tensor_info(case)
        assert "a" in result
        assert "b" in result
        assert "c" in result
        assert result["a"].shape == [1024, 1024]
        assert result["b"].shape == [1024, 1024]
        assert result["c"].shape == [1024, 1024]
        assert result["c"].tensor_type == TensorType.OUTPUT.value

    def test_c_shape_is_matmul_result(self):
        op = self._make_op()
        case = {
            "shape": [[2, 3], [3, 4]],
            "dtype": "float32",
            "pattern": "gauss_random",
        }
        result = op.create_tensor_info(case)
        assert result["c"].shape == [2, 4]

    def test_missing_shape_raises(self):
        op = self._make_op()
        with pytest.raises(ValueError, match="case\\['shape'\\] must contain at least 2 elements"):
            op.create_tensor_info({"dtype": "float32", "pattern": "gauss_random"})

    def test_empty_shape_raises(self):
        op = self._make_op()
        with pytest.raises(ValueError, match="case\\['shape'\\] must contain at least 2 elements"):
            op.create_tensor_info({"shape": [], "dtype": "float32", "pattern": "gauss_random"})

    def test_single_shape_raises(self):
        op = self._make_op()
        with pytest.raises(ValueError, match="case\\['shape'\\] must contain at least 2 elements"):
            op.create_tensor_info({"shape": [[1024, 1024]], "dtype": "float32", "pattern": "gauss_random"})

    def test_missing_dtype_raises(self):
        op = self._make_op()
        with pytest.raises(ValueError, match="case\\['dtype'\\] is required"):
            op.create_tensor_info({"shape": [[1024, 1024], [1024, 1024]], "pattern": "gauss_random"})

    def test_missing_pattern_raises(self):
        op = self._make_op()
        with pytest.raises(ValueError, match="case\\['pattern'\\] is required"):
            op.create_tensor_info({"shape": [[1024, 1024], [1024, 1024]], "dtype": "float32"})

    def test_none_shape_raises(self):
        op = self._make_op()
        with pytest.raises(ValueError, match="case\\['shape'\\] must contain at least 2 elements"):
            op.create_tensor_info({"shape": None, "dtype": "float32", "pattern": "gauss_random"})

    def test_none_dtype_raises(self):
        op = self._make_op()
        with pytest.raises(ValueError, match="case\\['dtype'\\] is required"):
            op.create_tensor_info({"shape": [[1024, 1024], [1024, 1024]], "dtype": None, "pattern": "gauss_random"})

    def test_none_pattern_raises(self):
        op = self._make_op()
        with pytest.raises(ValueError, match="case\\['pattern'\\] is required"):
            op.create_tensor_info({"shape": [[1024, 1024], [1024, 1024]], "dtype": "float32", "pattern": None})

    def test_tensor_info_device_set(self):
        op = self._make_op()
        op.device = 3
        case = {
            "shape": [[2, 2], [2, 2]],
            "dtype": "float32",
            "pattern": "gauss_random",
        }
        result = op.create_tensor_info(case)
        assert result["a"].device_id == 3
        assert result["b"].device_id == 3

    def test_output_tensor_has_no_device_pattern(self):
        op = self._make_op()
        case = {
            "shape": [[2, 2], [2, 2]],
            "dtype": "float32",
            "pattern": "gauss_random",
        }
        result = op.create_tensor_info(case)
        assert result["c"].tensor_type == TensorType.OUTPUT.value


@pytest.mark.unit
class TestDummyStressor:
    def test_init_accepts_any_kwargs(self):
        DummyStressor(device=0, buffer_mb=64, anything="test")

    def test_start_noop(self):
        s = DummyStressor()
        s.start()

    def test_stop_noop(self):
        s = DummyStressor()
        s.stop()


@pytest.mark.unit
class TestBaseMatmulSdmaDefaults:
    def test_default_stressor_class(self):
        assert BaseMatmulSdma.STRESSOR_CLASS is DummyStressor

    def test_default_stressor_kwargs(self):
        assert BaseMatmulSdma.STRESSOR_KWARGS == {}

    def test_default_stress_func(self):
        assert BaseMatmulSdma.STRESS_FUNC is None

    def test_default_stress_func_kwargs(self):
        assert BaseMatmulSdma.STRESS_FUNC_KWARGS == {}

    def test_subclass_inherits_stressor_class(self):
        class _ChildOp(BaseMatmulSdma):
            pass

        assert _ChildOp.STRESSOR_CLASS is DummyStressor

    def test_subclass_can_override_stressor_class(self):
        class _CustomStressor:
            def __init__(self, **kwargs):
                pass

            def start(self):
                pass

            def stop(self):
                pass

        class _ChildOp(BaseMatmulSdma):
            STRESSOR_CLASS = _CustomStressor

        assert _ChildOp.STRESSOR_CLASS is _CustomStressor


@pytest.mark.unit
class TestBaseMatmulSdmaRun:
    def test_run_calls_matmul(self):
        class _TestOp(BaseMatmulSdma):
            pass

        op = _TestOp()
        mock_a = MagicMock()
        mock_b = MagicMock()
        with patch("benchmarks.op.base_matmul_sdma.torch.matmul") as mock_matmul:
            mock_matmul.return_value = "result"
            output = op.run({"a": mock_a, "b": mock_b})
            mock_matmul.assert_called_once_with(mock_a, mock_b)
            assert output == "result"
