#!/usr/bin/env python3
# -*- coding: utf-8 -*-
# Copyright 2026 Huawei Technologies Co., Ltd
import pytest
from unittest.mock import MagicMock, patch
from common.enum import DetectType, TensorType

from benchmarks.op.cube_vector_sdma import (  # noqa:E402
    CubeVectorSdmaOp,
    VectorStressor,
    _burst_stress_func,
    CASE_RMS_NORM,
    CASE_RELU,
    CASE_POW,
    VECTOR_CASES,
)


@pytest.mark.unit
class TestVectorStressor:
    def test_init(self):
        vs = VectorStressor(stream=MagicMock(), device=0, op_func=MagicMock(), params=(MagicMock(),))
        assert vs.device == 0
        assert vs.timeout is False
        assert not vs.stop_event.is_set()

    def test_start_stop(self):
        with (
            patch("benchmarks.op.cube_vector_sdma.torch") as mock_torch,
            patch("benchmarks.op.cube_vector_sdma.stream_sync_timeout", return_value=False),
        ):
            mock_torch.npu.Stream.return_value = MagicMock()
            mock_torch.npu.current_stream.return_value = MagicMock()
            vs = VectorStressor(stream=MagicMock(), device=0, op_func=MagicMock(), params=(MagicMock(),))
            vs.start()
            assert vs.thread.is_alive()
            vs.stop()
            assert vs.stop_event.is_set()
            assert not vs.thread.is_alive()

    def test_timeout_on_stream_sync(self):
        with (
            patch("benchmarks.op.cube_vector_sdma.torch") as mock_torch,
            patch("benchmarks.op.cube_vector_sdma.stream_sync_timeout", return_value=True),
        ):
            mock_torch.npu.Stream.return_value = MagicMock()
            mock_torch.npu.current_stream.return_value = MagicMock()
            vs = VectorStressor(stream=MagicMock(), device=0, op_func=MagicMock(), params=(MagicMock(),))
            vs.start()
            vs.thread.join(timeout=5)
            assert vs.timeout is True


@pytest.mark.unit
class TestBurstStressFunc:
    def test_burst_stress_func_calls(self):
        with (
            patch("benchmarks.op.cube_vector_sdma.sdma_burst") as mock_burst,
            patch("benchmarks.op.cube_vector_sdma.sync_with_timeout") as mock_sync,
            patch("benchmarks.op.cube_vector_sdma.torch") as mock_torch,
        ):
            mock_tensor = MagicMock()
            _burst_stress_func(mock_tensor, 10, timeout_ms=5000, device=0)
            mock_burst.assert_called_once_with(mock_tensor, 10)
            mock_sync.assert_called_once_with(5000)
            mock_torch.npu.synchronize.assert_called_once_with(device=0)


@pytest.mark.unit
class TestCubeVectorSdmaOpRegistration:
    def test_registered_in_factory(self):
        from benchmarks.op.op_base import OpFactory

        instance = OpFactory.get_op_instance("cube_vector_sdma")
        assert isinstance(instance, CubeVectorSdmaOp)


@pytest.mark.unit
class TestCubeVectorSdmaOpRunCount:
    def test_from_config(self):
        op = CubeVectorSdmaOp()
        op.task_config = {"run_count": 500}
        assert op.run_count == 500

    def test_default_zero(self):
        op = CubeVectorSdmaOp()
        op.task_config = {}
        assert op.run_count == 0


@pytest.mark.unit
class TestCubeVectorSdmaOpParseCases:
    def test_basic(self):
        op = CubeVectorSdmaOp()
        op.task_config = {
            "dtype": ["bfloat16"],
            "pattern": ["gauss_random"],
            "shape": [[[4096, 4096], [4096, 4096]]],
            "vector_op": ["rms_norm"],
        }
        cases = op.parse_cases()
        assert len(cases) == 1
        assert cases[0]["vector_op"] == "rms_norm"

    def test_default_vector_ops(self):
        op = CubeVectorSdmaOp()
        op.task_config = {"dtype": ["bfloat16"], "pattern": ["gauss_random"], "shape": [[[4096, 4096], [4096, 4096]]]}
        cases = op.parse_cases()
        assert [c["vector_op"] for c in cases] == VECTOR_CASES

    def test_empty_dtype(self):
        op = CubeVectorSdmaOp()
        op.task_config = {
            "dtype": [],
            "pattern": ["gauss_random"],
            "shape": [[[4096, 4096], [4096, 4096]]],
            "vector_op": ["rms_norm"],
        }
        assert op.parse_cases() == []

    def test_full_six_cases(self):
        op = CubeVectorSdmaOp()
        op.task_config = {
            "dtype": ["bfloat16"],
            "pattern": ["uniform_random", "gauss_random"],
            "shape": [[[4096, 4096], [4096, 4096]]],
            "vector_op": ["rms_norm", "relu", "pow"],
        }
        assert len(op.parse_cases()) == 6


@pytest.mark.unit
class TestCubeVectorSdmaOpCreateTensorInfo:
    def test_rms_norm_x_shape(self):
        op = CubeVectorSdmaOp()
        op.device = 0
        result = op.create_tensor_info(
            {
                "dtype": "bfloat16",
                "pattern": "gauss_random",
                "shape": [[4096, 4096], [4096, 4096]],
                "vector_op": "rms_norm",
            }
        )
        assert result["x"].shape == [1, 2948]
        assert result["c"].shape == [4096, 4096]
        assert result["c"].tensor_type == TensorType.OUTPUT.value

    def test_relu_x_shape(self):
        op = CubeVectorSdmaOp()
        op.device = 0
        result = op.create_tensor_info(
            {"dtype": "bfloat16", "pattern": "gauss_random", "shape": [[4096, 4096], [4096, 4096]], "vector_op": "relu"}
        )
        assert result["x"].shape == [1, 2048]

    def test_pow_x_shape(self):
        op = CubeVectorSdmaOp()
        op.device = 0
        result = op.create_tensor_info(
            {"dtype": "bfloat16", "pattern": "gauss_random", "shape": [[4096, 4096], [4096, 4096]], "vector_op": "pow"}
        )
        assert result["x"].shape == [1, 2048]

    def test_default_shapes_on_empty(self):
        op = CubeVectorSdmaOp()
        op.device = 0
        result = op.create_tensor_info(
            {"dtype": "bfloat16", "pattern": "gauss_random", "shape": [], "vector_op": "rms_norm"}
        )
        assert result["a"].shape == [4096, 4096]
        assert result["b"].shape == [4096, 4096]
        assert result["c"].shape == [4096, 4096]

    def test_custom_shapes(self):
        op = CubeVectorSdmaOp()
        op.device = 0
        result = op.create_tensor_info(
            {"dtype": "bfloat16", "pattern": "gauss_random", "shape": [[2048, 1024], [1024, 4096]], "vector_op": "relu"}
        )
        assert result["a"].shape == [2048, 1024]
        assert result["b"].shape == [1024, 4096]
        assert result["c"].shape == [2048, 4096]

    def test_device_set(self):
        op = CubeVectorSdmaOp()
        op.device = 3
        result = op.create_tensor_info(
            {
                "dtype": "bfloat16",
                "pattern": "gauss_random",
                "shape": [[4096, 4096], [4096, 4096]],
                "vector_op": "rms_norm",
            }
        )
        assert result["a"].device_id == 3


@pytest.mark.unit
class TestCubeVectorSdmaOpRun:
    def test_unknown_vector_op_raises(self):
        op = CubeVectorSdmaOp()
        op.device = 0
        op.task_config = {"run_count": 1}
        with (
            patch("benchmarks.op.cube_vector_sdma.SelfMatmul") as MockMatmul,
            patch("benchmarks.op.cube_vector_sdma.torch"),
        ):
            MockMatmul.return_value.npu.return_value = MagicMock()
            with pytest.raises(ValueError, match="Unknown vector_op"):
                op.run({"a": MagicMock(), "b": MagicMock(), "x": MagicMock(), "vector_op": "unknown"})

    def test_rms_norm_branch(self):
        op = CubeVectorSdmaOp()
        op.device = 0
        op.task_config = {"run_count": 1}
        mock_x = MagicMock()
        mock_x.shape = [1, 2948]
        with (
            patch("benchmarks.op.cube_vector_sdma.SelfMatmul") as MockMatmul,
            patch("benchmarks.op.cube_vector_sdma.SelfRmsNorm") as MockNorm,
            patch("benchmarks.op.cube_vector_sdma.VectorStressor") as MockVS,
            patch("benchmarks.op.cube_vector_sdma.OpThread") as MockOT,
            patch("benchmarks.op.cube_vector_sdma.torch") as mock_torch,
        ):
            MockMatmul.return_value.npu.return_value = MagicMock()
            MockNorm.return_value.npu.return_value = MagicMock()
            MockVS.return_value = MagicMock()
            MockOT.return_value = MagicMock()
            MockOT.return_value.result = []
            mock_torch.npu.Stream.return_value = MagicMock()
            mock_torch.npu.synchronize = MagicMock()
            op.run({"a": MagicMock(), "b": MagicMock(), "x": mock_x, "vector_op": CASE_RMS_NORM})
            MockNorm.assert_called_once()

    def test_relu_branch(self):
        op = CubeVectorSdmaOp()
        op.device = 0
        op.task_config = {"run_count": 1}
        mock_x = MagicMock()
        with (
            patch("benchmarks.op.cube_vector_sdma.SelfMatmul") as MockMatmul,
            patch("benchmarks.op.cube_vector_sdma.SelfRelu") as MockRelu,
            patch("benchmarks.op.cube_vector_sdma.VectorStressor") as MockVS,
            patch("benchmarks.op.cube_vector_sdma.OpThread") as MockOT,
            patch("benchmarks.op.cube_vector_sdma.torch") as mock_torch,
        ):
            MockMatmul.return_value.npu.return_value = MagicMock()
            MockRelu.return_value.npu.return_value = MagicMock()
            MockVS.return_value = MagicMock()
            MockOT.return_value = MagicMock()
            MockOT.return_value.result = []
            mock_torch.npu.Stream.return_value = MagicMock()
            mock_torch.npu.synchronize = MagicMock()
            op.run({"a": MagicMock(), "b": MagicMock(), "x": mock_x, "vector_op": CASE_RELU})
            MockRelu.assert_called_once()

    def test_pow_branch(self):
        op = CubeVectorSdmaOp()
        op.device = 0
        op.task_config = {"run_count": 1}
        mock_x = MagicMock()
        with (
            patch("benchmarks.op.cube_vector_sdma.SelfMatmul") as MockMatmul,
            patch("benchmarks.op.cube_vector_sdma.SelfPow") as MockPow,
            patch("benchmarks.op.cube_vector_sdma.VectorStressor") as MockVS,
            patch("benchmarks.op.cube_vector_sdma.OpThread") as MockOT,
            patch("benchmarks.op.cube_vector_sdma.torch") as mock_torch,
        ):
            MockMatmul.return_value.npu.return_value = MagicMock()
            MockPow.return_value.npu.return_value = MagicMock()
            MockVS.return_value = MagicMock()
            MockOT.return_value = MagicMock()
            MockOT.return_value.result = []
            mock_torch.npu.Stream.return_value = MagicMock()
            mock_torch.npu.synchronize = MagicMock()
            op.run({"a": MagicMock(), "b": MagicMock(), "x": mock_x, "vector_op": CASE_POW})
            MockPow.assert_called_once()

    def test_returns_matmul_result(self):
        op = CubeVectorSdmaOp()
        op.device = 0
        op.task_config = {"run_count": 1}
        mock_x = MagicMock()
        mock_x.shape = [1, 2948]
        with (
            patch("benchmarks.op.cube_vector_sdma.SelfMatmul") as MockMatmul,
            patch("benchmarks.op.cube_vector_sdma.SelfRmsNorm") as MockNorm,
            patch("benchmarks.op.cube_vector_sdma.VectorStressor") as MockVS,
            patch("benchmarks.op.cube_vector_sdma.OpThread") as MockOT,
            patch("benchmarks.op.cube_vector_sdma.torch") as mock_torch,
        ):
            MockMatmul.return_value.npu.return_value = MagicMock()
            MockNorm.return_value.npu.return_value = MagicMock()
            MockVS.return_value = MagicMock()
            MockOT.return_value.result = [{"step": 0}]
            mock_torch.npu.Stream.return_value = MagicMock()
            mock_torch.npu.synchronize = MagicMock()
            result = op.run({"a": MagicMock(), "b": MagicMock(), "x": mock_x, "vector_op": CASE_RMS_NORM})
            assert result == [{"step": 0}]


@pytest.mark.unit
class TestCubeVectorSdmaOpCaseRun:
    def test_starts_stops_sdma_stressor(self):
        op = CubeVectorSdmaOp()
        op.task_config = {
            "dtype": ["bfloat16"],
            "pattern": ["gauss_random"],
            "shape": [[[4096, 4096], [4096, 4096]]],
            "vector_op": ["rms_norm"],
            "run_count": 1,
        }
        op.device = 0
        op.detect_type = "sdc"
        op.sub_detect_types = [DetectType.SDC.value]
        with (
            patch("benchmarks.op.cube_vector_sdma.BurstStressor") as MockBurst,
            patch("benchmarks.op.cube_vector_sdma.monitor_and_profile") as mock_mp,
            patch.object(CubeVectorSdmaOp, "run", return_value=[]),
            patch.object(CubeVectorSdmaOp, "create_tensor_info", return_value={}),
            patch("benchmarks.op.cube_vector_sdma.create_tensor", return_value={}),
        ):
            mock_stressor = MagicMock()
            MockBurst.return_value = mock_stressor
            mock_mp_cm = MagicMock()
            mock_mp_cm.__enter__ = MagicMock(return_value={"execution_time": 1.0})
            mock_mp_cm.__exit__ = MagicMock(return_value=False)
            mock_mp.return_value = mock_mp_cm
            op.case_run()
            mock_stressor.start.assert_called_once()
            mock_stressor.stop.assert_called_once()

    def test_stops_stressor_on_error(self):
        op = CubeVectorSdmaOp()
        op.task_config = {
            "dtype": ["bfloat16"],
            "pattern": ["gauss_random"],
            "shape": [[[4096, 4096], [4096, 4096]]],
            "vector_op": ["rms_norm"],
            "run_count": 1,
        }
        op.device = 0
        op.detect_type = "sdc"
        op.sub_detect_types = [DetectType.SDC.value]
        with (
            patch("benchmarks.op.cube_vector_sdma.BurstStressor") as MockBurst,
            patch("benchmarks.op.cube_vector_sdma.monitor_and_profile") as mock_mp,
            patch.object(CubeVectorSdmaOp, "run", side_effect=RuntimeError("err")),
            patch.object(CubeVectorSdmaOp, "create_tensor_info", return_value={}),
            patch("benchmarks.op.cube_vector_sdma.create_tensor", return_value={}),
        ):
            mock_stressor = MagicMock()
            MockBurst.return_value = mock_stressor
            mock_mp_cm = MagicMock()
            mock_mp_cm.__enter__ = MagicMock(return_value={"execution_time": 1.0})
            mock_mp_cm.__exit__ = MagicMock(return_value=False)
            mock_mp.return_value = mock_mp_cm
            with pytest.raises(RuntimeError, match="err"):
                op.case_run()
            mock_stressor.stop.assert_called_once()

    def test_returns_error_records_and_statistics(self):
        op = CubeVectorSdmaOp()
        op.task_config = {
            "dtype": ["bfloat16"],
            "pattern": ["gauss_random"],
            "shape": [[[4096, 4096], [4096, 4096]]],
            "vector_op": ["rms_norm"],
            "run_count": 1,
        }
        op.device = 0
        op.detect_type = "sdc"
        op.sub_detect_types = [DetectType.SDC.value]
        with (
            patch("benchmarks.op.cube_vector_sdma.BurstStressor") as MockBurst,
            patch("benchmarks.op.cube_vector_sdma.monitor_and_profile") as mock_mp,
            patch.object(CubeVectorSdmaOp, "run", return_value=[]),
            patch.object(CubeVectorSdmaOp, "create_tensor_info", return_value={}),
            patch("benchmarks.op.cube_vector_sdma.create_tensor", return_value={}),
        ):
            mock_stressor = MagicMock()
            MockBurst.return_value = mock_stressor
            mock_mp_cm = MagicMock()
            mock_mp_cm.__enter__ = MagicMock(return_value={"execution_time": 1.0})
            mock_mp_cm.__exit__ = MagicMock(return_value=False)
            mock_mp.return_value = mock_mp_cm
            result = op.case_run()
            assert "error_records" in result
            assert "case_statistics" in result
            assert result["case_statistics"][0]["result"] is True

    def test_multiple_cases(self):
        op = CubeVectorSdmaOp()
        op.task_config = {
            "dtype": ["bfloat16"],
            "pattern": ["gauss_random"],
            "shape": [[[4096, 4096], [4096, 4096]]],
            "vector_op": ["rms_norm", "relu"],
            "run_count": 1,
        }
        op.device = 0
        op.detect_type = "sdc"
        op.sub_detect_types = [DetectType.SDC.value]
        with (
            patch("benchmarks.op.cube_vector_sdma.BurstStressor") as MockBurst,
            patch("benchmarks.op.cube_vector_sdma.monitor_and_profile") as mock_mp,
            patch.object(CubeVectorSdmaOp, "run", return_value=[]),
            patch.object(CubeVectorSdmaOp, "create_tensor_info", return_value={}),
            patch("benchmarks.op.cube_vector_sdma.create_tensor", return_value={}),
        ):
            mock_stressor = MagicMock()
            MockBurst.return_value = mock_stressor
            mock_mp_cm = MagicMock()
            mock_mp_cm.__enter__ = MagicMock(return_value={"execution_time": 1.0})
            mock_mp_cm.__exit__ = MagicMock(return_value=False)
            mock_mp.return_value = mock_mp_cm
            result = op.case_run()
            assert len(result["case_statistics"]) == 2
            assert MockBurst.call_count == 2
