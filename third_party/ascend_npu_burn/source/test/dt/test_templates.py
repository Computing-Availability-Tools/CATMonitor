#!/usr/bin/env python3
# -*- coding: utf-8 -*-
# Copyright 2026 Huawei Technologies Co., Ltd
import pytest
import torch
from unittest.mock import MagicMock, patch, PropertyMock

from benchmarks.op.op_base import OpBase, OpFactory
from benchmarks.op.template.single import SingleTemplate, start_profiling, stop_profiling, monitor_and_profile
from common.enum import DetectType, TensorType
from common.tensor_info import TensorInfo


@pytest.mark.unit
class TestSingleTemplate:

    def _make_op(self, config=None):
        class _TestSingleOp(OpBase, SingleTemplate):
            pass

        op = _TestSingleOp()
        op.task_config = config or {}
        op.detect_type = "sdc"
        op.sub_detect_types = [DetectType.SDC.value]
        op.device = 0
        return op

    def test_dtype_property(self):
        op = self._make_op({"dtype": ["float32", "float16"]})
        assert op.dtype == ["float32", "float16"]

    def test_dtype_default(self):
        op = self._make_op({})
        assert op.dtype == [""]

    def test_run_count_property(self):
        op = self._make_op({"run_count": 100})
        assert op.run_count == 100

    def test_run_count_default(self):
        op = self._make_op({})
        assert op.run_count == 0

    def test_shape_property(self):
        op = self._make_op({"shape": [[[1024, 1024], [1024, 1024]]]})
        assert op.shape == [[[1024, 1024], [1024, 1024]]]

    def test_shape_default(self):
        op = self._make_op({})
        assert op.shape == [[]]

    def test_pattern_property(self):
        op = self._make_op({"pattern": ["gauss_random"]})
        assert op.pattern == ["gauss_random"]

    def test_pattern_default(self):
        op = self._make_op({})
        assert op.pattern == [""]

    def test_detect_instances_property(self):
        op = self._make_op()
        instances = op.detect_instances
        assert len(instances) > 0

    def test_parse_cases_single_dtype_single_shape(self):
        op = self._make_op({
            "dtype": ["float32"],
            "shape": [[[1024, 1024], [1024, 1024]]],
            "pattern": ["gauss_random"],
        })
        cases = op.parse_cases()
        assert len(cases) == 1
        assert cases[0]["dtype"] == "float32"
        assert cases[0]["shape"] == [[1024, 1024], [1024, 1024]]
        assert cases[0]["pattern"] == "gauss_random"

    def test_parse_cases_multiple_dtypes(self):
        op = self._make_op({
            "dtype": ["float32", "float16"],
            "shape": [[[1024, 1024], [1024, 1024]]],
            "pattern": ["gauss_random"],
        })
        cases = op.parse_cases()
        assert len(cases) == 2
        dtypes = [c["dtype"] for c in cases]
        assert "float32" in dtypes
        assert "float16" in dtypes

    def test_parse_cases_multiple_shapes(self):
        op = self._make_op({
            "dtype": ["float32"],
            "shape": [[[512, 512], [512, 512]], [[256, 256], [256, 256]]],
            "pattern": ["gauss_random"],
        })
        cases = op.parse_cases()
        assert len(cases) == 2

    def test_parse_cases_multiple_patterns(self):
        op = self._make_op({
            "dtype": ["float32"],
            "shape": [[[1024, 1024], [1024, 1024]]],
            "pattern": ["gauss_random", "uniform_random"],
        })
        cases = op.parse_cases()
        assert len(cases) == 2
        patterns = [c["pattern"] for c in cases]
        assert "gauss_random" in patterns
        assert "uniform_random" in patterns

    def test_parse_cases_cartesian_product(self):
        op = self._make_op({
            "dtype": ["float32", "float16"],
            "shape": [[[1024, 1024], [1024, 1024]]],
            "pattern": ["gauss_random", "uniform_random"],
        })
        cases = op.parse_cases()
        assert len(cases) == 2 * 1 * 2

    def test_parse_cases_empty_config(self):
        op = self._make_op({})
        cases = op.parse_cases()
        assert len(cases) == 1

    def test_create_tensor_info_not_implemented(self):
        op = self._make_op()
        with pytest.raises(NotImplementedError):
            op.create_tensor_info({})


@pytest.mark.unit
class TestSingleTemplateCaseRun:

    def _make_op(self, config=None):
        class _TestRunOp(OpBase, SingleTemplate):
            def create_tensor_info(self, case):
                return {
                    "a": TensorInfo(shape=[2, 2], dtype="float32", device=self.device, pattern="gauss_random"),
                    "b": TensorInfo(shape=[2, 2], dtype="float32", device=self.device, pattern="gauss_random"),
                    "c": TensorInfo(shape=[2, 2], dtype="float32", tensor_type=TensorType.OUTPUT.value),
                }

            def run(self, tensor_mapping):
                return torch.randn(2, 2)

        op = _TestRunOp()
        op.task_config = config or {}
        op.detect_type = "sdc"
        op.sub_detect_types = [DetectType.SDC.value]
        op.device = 0
        return op

    def test_no_detect_instances_returns_empty(self):
        op = self._make_op({"dtype": ["float32"], "pattern": ["gauss_random"], "shape": [[[2, 2], [2, 2]]]})
        op.detect_type = "nonexistent"
        op.sub_detect_types = []
        result = op.case_run()
        assert result == {}

    def test_make_timeout_record(self):
        op = self._make_op({"dtype": ["float32"], "pattern": ["gauss_random"], "shape": [[[2, 2], [2, 2]]]})
        record = op._make_timeout_record("TestOp", 5, 0, {"dtype": "float32"}, "timeout error", has_timeout=True)
        assert "error_records" in record
        assert "case_statistics" in record
        assert record["error_records"][0]["has_timeout"] is True
        assert record["error_records"][0]["step"] == 5
        assert record["case_statistics"][0]["result"] is False


@pytest.mark.unit
class TestStartProfiling:

    def test_disabled_returns_none(self):
        prof, prof_dir = start_profiling("test_op", 0, 0, enable_profiling=False)
        assert prof is None
        assert prof_dir is None

    def test_non_zero_case_idx_returns_none(self):
        prof, prof_dir = start_profiling("test_op", 1, 0, enable_profiling=True)
        assert prof is None
        assert prof_dir is None


@pytest.mark.unit
class TestStopProfiling:

    def test_none_prof_noop(self):
        stop_profiling(None, "test_op", 0, 0, "/tmp")

    def test_prof_stop_called(self):
        mock_prof = MagicMock()
        stop_profiling(mock_prof, "test_op", 0, 0, "/tmp")
        mock_prof.stop.assert_called_once()
        mock_prof.export_chrome_trace.assert_called_once()


@pytest.mark.unit
class TestMonitorAndProfile:

    def test_yields_metrics_dict(self):
        with monitor_and_profile("test_op", 0, 0, enable_profiling=False) as metrics:
            assert isinstance(metrics, dict)
        assert "execution_time" in metrics
        assert metrics["execution_time"] >= 0

    def test_execution_time_measured(self):
        import time
        with monitor_and_profile("test_op", 0, 0, enable_profiling=False) as metrics:
            time.sleep(0.05)
        assert metrics["execution_time"] >= 0.04
