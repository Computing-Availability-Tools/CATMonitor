#!/usr/bin/env python3
# -*- coding: utf-8 -*-
# Copyright 2026 Huawei Technologies Co., Ltd
import pytest
from unittest.mock import MagicMock, patch
from common.enum import DetectType, TensorType

from benchmarks.op.gmm_backward_rmsnorm import GMMBackwardRMSNormOp, RMSNormStressor  # noqa: E402


@pytest.mark.unit
class TestRMSNormInit:
    def test_init_creates_tensors(self):
        with patch("benchmarks.op.gmm_backward_rmsnorm.torch") as mock_torch:
            mock_stream = MagicMock()
            mock_torch.npu.Stream.return_value = mock_stream
            mock_torch.randn.return_value = MagicMock()
            rms = RMSNormStressor(device=0, task_config={"ND": [512, 4096]})
            assert rms.x is not None
            assert rms.w is not None
            assert rms.device == 0

    def test_init_default_nd_shape(self):
        with patch("benchmarks.op.gmm_backward_rmsnorm.torch") as mock_torch:
            mock_torch.npu.Stream.return_value = MagicMock()
            mock_torch.randn.return_value = MagicMock()
            RMSNormStressor(device=0, task_config={})
            mock_torch.randn.assert_called()


@pytest.mark.unit
class TestRMSNormCreateTensor:
    def test_valid_nd_shape(self):
        with patch("benchmarks.op.gmm_backward_rmsnorm.torch") as mock_torch:
            mock_torch.npu.Stream.return_value = MagicMock()
            mock_torch.randn.return_value = MagicMock()
            rms = RMSNormStressor(device=0, task_config={"ND": [256, 2048]})
            rms._create_tensor()
            calls = mock_torch.randn.call_args_list
            assert len(calls) >= 2

    def test_invalid_nd_shape_fallback(self):
        with patch("benchmarks.op.gmm_backward_rmsnorm.torch") as mock_torch:
            mock_torch.npu.Stream.return_value = MagicMock()
            mock_torch.randn.return_value = MagicMock()
            rms = RMSNormStressor(device=0, task_config={"ND": [1, 2, 3]})
            rms._create_tensor()
            calls = mock_torch.randn.call_args_list
            x_call = calls[-2]
            assert x_call[0][0] == [512, 4096]

    def test_empty_nd_shape_fallback(self):
        with patch("benchmarks.op.gmm_backward_rmsnorm.torch") as mock_torch:
            mock_torch.npu.Stream.return_value = MagicMock()
            mock_torch.randn.return_value = MagicMock()
            rms = RMSNormStressor(device=0, task_config={"ND": []})
            rms._create_tensor()
            calls = mock_torch.randn.call_args_list
            x_call = calls[-2]
            assert x_call[0][0] == [512, 4096]


@pytest.mark.unit
class TestRMSNormStartStop:
    def test_start_begins_thread(self):
        with patch("benchmarks.op.gmm_backward_rmsnorm.torch") as mock_torch:
            mock_torch.npu.Stream.return_value = MagicMock()
            mock_torch.randn.return_value = MagicMock()
            rms = RMSNormStressor(device=0, task_config={"ND": [512, 4096]})
            rms.start()
            assert rms.thread.is_alive()
            rms.stop()

    def test_stop_sets_event(self):
        with patch("benchmarks.op.gmm_backward_rmsnorm.torch") as mock_torch:
            mock_torch.npu.Stream.return_value = MagicMock()
            mock_torch.randn.return_value = MagicMock()
            rms = RMSNormStressor(device=0, task_config={"ND": [512, 4096]})
            rms.start()
            rms.stop()
            assert rms.stop_event.is_set()
            assert not rms.thread.is_alive()


@pytest.mark.unit
class TestGMMBackwardRMSNormOpRegistration:
    def test_op_registered_in_factory(self):
        from benchmarks.op.op_base import OpFactory

        instance = OpFactory.get_op_instance("gmm_backward_rms_norm")
        assert instance is not None
        assert isinstance(instance, GMMBackwardRMSNormOp)


@pytest.mark.unit
class TestGMMBackwardRMSNormOpSubDetectTypes:
    def test_sub_detect_types_is_sdc(self):
        op = GMMBackwardRMSNormOp()
        op.set_sub_detect_types()
        assert op.sub_detect_types == [DetectType.SDC.value]


@pytest.mark.unit
class TestGMMBackwardRMSNormOpParseCases:
    def test_valid_config_returns_cases(self):
        op = GMMBackwardRMSNormOp()
        op.task_config = {
            "dtype": ["float16"],
            "pattern": ["gauss_random"],
            "ESKN": [8, 32768, 768, 2048],
        }
        cases = op.parse_cases()
        assert len(cases) == 1
        assert cases[0]["dtype"] == "float16"
        assert cases[0]["pattern"] == "gauss_random"
        assert cases[0]["shape"] == [8, 32768, 768, 2048]

    def test_multiple_dtypes_and_patterns(self):
        op = GMMBackwardRMSNormOp()
        op.task_config = {
            "dtype": ["float16", "float32"],
            "pattern": ["gauss_random", "uniform_random"],
            "ESKN": [8, 32768, 768, 2048],
        }
        cases = op.parse_cases()
        assert len(cases) == 4

    def test_empty_eskn_returns_empty(self):
        op = GMMBackwardRMSNormOp()
        op.task_config = {
            "dtype": ["float16"],
            "pattern": ["gauss_random"],
            "ESKN": [],
        }
        cases = op.parse_cases()
        assert len(cases) == 0

    def test_eskn_wrong_length_returns_empty(self):
        op = GMMBackwardRMSNormOp()
        op.task_config = {
            "dtype": ["float16"],
            "pattern": ["gauss_random"],
            "ESKN": [8, 32768, 768],
        }
        cases = op.parse_cases()
        assert len(cases) == 0

    def test_missing_eskn_returns_empty(self):
        op = GMMBackwardRMSNormOp()
        op.task_config = {
            "dtype": ["float16"],
            "pattern": ["gauss_random"],
        }
        cases = op.parse_cases()
        assert len(cases) == 0

    def test_empty_dtype_returns_empty(self):
        op = GMMBackwardRMSNormOp()
        op.task_config = {
            "dtype": [],
            "pattern": ["gauss_random"],
            "ESKN": [8, 32768, 768, 2048],
        }
        cases = op.parse_cases()
        assert len(cases) == 0

    def test_empty_pattern_returns_empty(self):
        op = GMMBackwardRMSNormOp()
        op.task_config = {
            "dtype": ["float16"],
            "pattern": [],
            "ESKN": [8, 32768, 768, 2048],
        }
        cases = op.parse_cases()
        assert len(cases) == 0


@pytest.mark.unit
class TestGMMBackwardRMSNormOpCreateTensorInfo:
    def test_valid_case_creates_tensor_info(self):
        op = GMMBackwardRMSNormOp()
        op.device = 0
        case = {
            "dtype": "float16",
            "pattern": "gauss_random",
            "shape": [8, 32768, 768, 2048],
        }
        result = op.create_tensor_info(case)
        assert "x" in result
        assert "w" in result
        assert "y" in result

    def test_x_shape_is_s_k(self):
        op = GMMBackwardRMSNormOp()
        op.device = 0
        case = {
            "dtype": "float16",
            "pattern": "gauss_random",
            "shape": [8, 32768, 768, 2048],
        }
        result = op.create_tensor_info(case)
        assert result["x"].shape == [32768, 768]

    def test_w_shape_is_e_k_n(self):
        op = GMMBackwardRMSNormOp()
        op.device = 0
        case = {
            "dtype": "float16",
            "pattern": "gauss_random",
            "shape": [8, 32768, 768, 2048],
        }
        result = op.create_tensor_info(case)
        assert result["w"].shape == [8, 768, 2048]

    def test_y_shape_is_s_n(self):
        op = GMMBackwardRMSNormOp()
        op.device = 0
        case = {
            "dtype": "float16",
            "pattern": "gauss_random",
            "shape": [8, 32768, 768, 2048],
        }
        result = op.create_tensor_info(case)
        assert result["y"].shape == [32768, 2048]

    def test_x_and_w_are_input_type(self):
        op = GMMBackwardRMSNormOp()
        op.device = 0
        case = {
            "dtype": "float16",
            "pattern": "gauss_random",
            "shape": [8, 32768, 768, 2048],
        }
        result = op.create_tensor_info(case)
        assert result["x"].tensor_type == TensorType.INPUT.value
        assert result["w"].tensor_type == TensorType.INPUT.value

    def test_y_has_custom_creator(self):
        op = GMMBackwardRMSNormOp()
        op.device = 0
        case = {
            "dtype": "float16",
            "pattern": "gauss_random",
            "shape": [8, 32768, 768, 2048],
        }
        result = op.create_tensor_info(case)
        assert result["y"].creator is not None

    def test_device_set_correctly(self):
        op = GMMBackwardRMSNormOp()
        op.device = 3
        case = {
            "dtype": "float16",
            "pattern": "gauss_random",
            "shape": [8, 32768, 768, 2048],
        }
        result = op.create_tensor_info(case)
        assert result["x"].device_id == 3
        assert result["w"].device_id == 3

    def test_different_shape_dimensions(self):
        op = GMMBackwardRMSNormOp()
        op.device = 0
        case = {
            "dtype": "float32",
            "pattern": "uniform_random",
            "shape": [4, 1024, 256, 512],
        }
        result = op.create_tensor_info(case)
        assert result["x"].shape == [1024, 256]
        assert result["w"].shape == [4, 256, 512]
        assert result["y"].shape == [1024, 512]


@pytest.mark.unit
class TestGMMBackwardRMSNormOpRun:
    def test_run_calls_npu_gmm_and_backward(self):
        op = GMMBackwardRMSNormOp()
        mock_x = MagicMock()
        mock_w = MagicMock()
        mock_y = MagicMock()
        mock_x_after = MagicMock(name="x_after")
        mock_w_after = MagicMock(name="w_after")
        mock_y_after = MagicMock(name="y_after")
        mock_x.clone.return_value.detach.return_value.requires_grad_.return_value = mock_x_after
        mock_w.clone.return_value.detach.return_value.requires_grad_.return_value = mock_w_after
        mock_y.clone.return_value = mock_y_after

        mock_result = MagicMock()
        with patch("benchmarks.op.gmm_backward_rmsnorm.npu_gmm", return_value=mock_result) as mock_gmm:
            op.run({"x": mock_x, "w": mock_w, "y": mock_y})
            mock_gmm.assert_called_once()
            mock_result.backward.assert_called_once_with(mock_y_after)

    def test_run_returns_w_grad(self):
        op = GMMBackwardRMSNormOp()
        mock_x = MagicMock()
        mock_w = MagicMock()
        mock_y = MagicMock()
        mock_w_after = MagicMock(name="w_after")
        mock_w.clone.return_value.detach.return_value.requires_grad_.return_value = mock_w_after
        mock_result = MagicMock()
        mock_w.grad = MagicMock()

        with patch("benchmarks.op.gmm_backward_rmsnorm.npu_gmm", return_value=mock_result):
            output = op.run({"x": mock_x, "w": mock_w, "y": mock_y})
            assert output is mock_w_after.grad

    def test_run_uses_group_list(self):
        op = GMMBackwardRMSNormOp()
        mock_x = MagicMock()
        mock_w = MagicMock()
        mock_y = MagicMock()
        mock_x_after = MagicMock(name="x_after")
        mock_x_after.shape = [32768, 1]
        mock_x.clone.return_value.detach.return_value.requires_grad_.return_value = mock_x_after
        mock_w_after = MagicMock(name="w_after")
        mock_w_after.shape = [8, 1, 1]
        mock_w.clone.return_value.detach.return_value.requires_grad_.return_value = mock_w_after
        mock_result = MagicMock()

        with patch("benchmarks.op.gmm_backward_rmsnorm.npu_gmm", return_value=mock_result) as mock_gmm:
            op.run({"x": mock_x, "w": mock_w, "y": mock_y})
            call_kwargs = mock_gmm.call_args
            assert call_kwargs[1]["group_list"] == [4096, 8192, 12288, 16384, 20480, 24576, 28672, 32768]


@pytest.mark.unit
class TestGMMBackwardRMSNormOpCaseRun:
    def test_case_run_starts_and_stops_bg_stressor(self):
        op = GMMBackwardRMSNormOp()
        op.task_config = {"ND": [512, 4096], "run_count": 100}
        op.device = 0
        op.detect_type = "sdc"
        op.sub_detect_types = [DetectType.SDC.value]

        with (
            patch("benchmarks.op.gmm_backward_rmsnorm.RMSNormStressor") as MockRMSNorm,
            patch.object(type(op).__mro__[2], "case_run", return_value={}),
        ):
            mock_stressor = MagicMock()
            MockRMSNorm.return_value = mock_stressor
            op.case_run()
            mock_stressor.start.assert_called_once()
            mock_stressor.stop.assert_called_once()

    def test_case_run_stops_stressor_on_error(self):
        op = GMMBackwardRMSNormOp()
        op.task_config = {"ND": [512, 4096], "run_count": 100}
        op.device = 0
        op.detect_type = "sdc"
        op.sub_detect_types = [DetectType.SDC.value]

        with (
            patch("benchmarks.op.gmm_backward_rmsnorm.RMSNormStressor") as MockRMSNorm,
            patch.object(type(op).__mro__[2], "case_run", side_effect=RuntimeError("test error")),
        ):
            mock_stressor = MagicMock()
            MockRMSNorm.return_value = mock_stressor
            with pytest.raises(RuntimeError, match="test error"):
                op.case_run()
            mock_stressor.stop.assert_called_once()
