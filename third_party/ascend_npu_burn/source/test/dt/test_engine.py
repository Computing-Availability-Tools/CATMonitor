#!/usr/bin/env python3
# -*- coding: utf-8 -*-
# Copyright 2026 Huawei Technologies Co., Ltd
import pytest
import os
import csv
import tempfile
import sys
from unittest.mock import MagicMock, patch

mock_custom_ops = MagicMock()
sys.modules['custom_ops'] = mock_custom_ops

from runtime.engine import generate_csv_report, generate_error_report, result_display, npu_result_display  # noqa: E402
from runtime.scheduler import Scheduler  # noqa: E402


@pytest.mark.unit
class TestGenerateCsvReport:
    def test_empty_results(self):
        with tempfile.TemporaryDirectory() as tmpdir:
            generate_csv_report([], output_dir=tmpdir)
            output_path = os.path.join(tmpdir, "npu_burn_results.csv")
            assert os.path.isfile(output_path)

    def test_results_with_error_skipped(self):
        with tempfile.TemporaryDirectory() as tmpdir:
            results = [{"error": "some error", "device_id": 0}]
            generate_csv_report(results, output_dir=tmpdir)
            output_path = os.path.join(tmpdir, "npu_burn_results.csv")
            with open(output_path, 'r', encoding='utf-8') as f:
                reader = csv.DictReader(f)
                rows = list(reader)
            assert len(rows) == 0

    def test_results_with_case_statistics(self):
        with tempfile.TemporaryDirectory() as tmpdir:
            results = [
                {
                    "device_id": 0,
                    "result": {
                        "case_statistics": [
                            {
                                "task": "matmul",
                                "case_idx": 0,
                                "run_count": 100,
                                "stream_count": 1,
                                "exetime": 1.5,
                                "err_count": 0,
                                "result": True,
                                "case": {"dtype": "float32", "shape": [[1024, 1024], [1024, 1024]]},
                            }
                        ]
                    },
                }
            ]
            generate_csv_report(results, output_dir=tmpdir)
            output_path = os.path.join(tmpdir, "npu_burn_results.csv")
            with open(output_path, 'r', encoding='utf-8') as f:
                reader = csv.DictReader(f)
                rows = list(reader)
            assert len(rows) == 1
            assert rows[0]["task"] == "matmul"
            assert rows[0]["result"] == "PASS"

    def test_multiple_cases(self):
        with tempfile.TemporaryDirectory() as tmpdir:
            results = [
                {
                    "device_id": 0,
                    "result": {
                        "case_statistics": [
                            {
                                "task": "matmul",
                                "case_idx": 0,
                                "run_count": 100,
                                "stream_count": 1,
                                "exetime": 1.0,
                                "err_count": 0,
                                "result": True,
                                "case": {"dtype": "float32"},
                            },
                            {
                                "task": "matmul",
                                "case_idx": 1,
                                "run_count": 100,
                                "stream_count": 1,
                                "exetime": 1.2,
                                "err_count": 0,
                                "result": True,
                                "case": {"dtype": "float16"},
                            },
                        ]
                    },
                }
            ]
            generate_csv_report(results, output_dir=tmpdir)
            output_path = os.path.join(tmpdir, "npu_burn_results.csv")
            with open(output_path, 'r', encoding='utf-8') as f:
                reader = csv.DictReader(f)
                rows = list(reader)
            assert len(rows) == 2

    def test_append_to_existing_file(self):
        with tempfile.TemporaryDirectory() as tmpdir:
            results1 = [
                {
                    "device_id": 0,
                    "result": {
                        "case_statistics": [
                            {
                                "task": "matmul",
                                "case_idx": 0,
                                "run_count": 10,
                                "stream_count": 1,
                                "exetime": 0.5,
                                "err_count": 0,
                                "result": True,
                                "case": {},
                            }
                        ]
                    },
                }
            ]
            generate_csv_report(results1, output_dir=tmpdir)
            generate_csv_report(results1, output_dir=tmpdir)
            output_path = os.path.join(tmpdir, "npu_burn_results.csv")
            with open(output_path, 'r', encoding='utf-8') as f:
                reader = csv.DictReader(f)
                rows = list(reader)
            assert len(rows) == 2


@pytest.mark.unit
class TestGenerateErrorReport:
    def test_no_errors_no_file(self):
        with tempfile.TemporaryDirectory() as tmpdir:
            results = [{"device_id": 0, "result": {"error_records": []}}]
            generate_error_report(results, output_dir=tmpdir)
            output_path = os.path.join(tmpdir, "npu_burn_errors.csv")
            assert not os.path.isfile(output_path)

    def test_with_error_records(self):
        with tempfile.TemporaryDirectory() as tmpdir:
            results = [
                {
                    "device_id": 0,
                    "task_name": "matmul",
                    "result": {
                        "error_records": [
                            {
                                "detect_type": "sdc",
                                "step": 42,
                                "timestamp": "2026-01-01 00:00:00.000000",
                                "fail_num": 1,
                                "result": False,
                                "err_info": "SDC detected",
                            }
                        ]
                    },
                }
            ]
            generate_error_report(results, output_dir=tmpdir)
            output_path = os.path.join(tmpdir, "npu_burn_errors.csv")
            assert os.path.isfile(output_path)
            with open(output_path, 'r', encoding='utf-8') as f:
                reader = csv.DictReader(f)
                rows = list(reader)
            assert len(rows) == 1
            assert rows[0]["detect_type"] == "sdc"
            assert rows[0]["step"] == "42"

    def test_error_results_skipped(self):
        with tempfile.TemporaryDirectory() as tmpdir:
            results = [{"error": "task failed", "device_id": 0}]
            generate_error_report(results, output_dir=tmpdir)
            output_path = os.path.join(tmpdir, "npu_burn_errors.csv")
            assert not os.path.isfile(output_path)


@pytest.mark.unit
class TestResultDisplay:
    def test_error_result(self):
        results = [
            {"task_seq": 1, "task_name": "matmul", "device_id": 0, "error": "RuntimeError: something went wrong"}
        ]
        result_display(0, results)

    def test_skipped_result(self):
        results = [
            {
                "task_seq": 1,
                "task_name": "matmul",
                "device_id": 0,
                "error": "Skipped due to previous timeout on this device",
            }
        ]
        result_display(0, results)

    def test_empty_results(self):
        result_display(0, [])


@pytest.mark.unit
class TestNpuResultDisplay:
    def test_empty_results(self, capsys):
        npu_result_display([])
        captured = capsys.readouterr()
        assert "No results" in captured.out

    def test_pass_results(self, capsys):
        results = [
            {
                "device_id": 0,
                "task_name": "matmul",
                "result": {"case_statistics": [{"err_count": 0, "run_count": 100, "stream_count": 1}]},
            }
        ]
        npu_result_display(results)
        captured = capsys.readouterr()
        assert "PASS" in captured.out

    def test_fail_results(self, capsys):
        results = [
            {
                "device_id": 0,
                "task_name": "conv2d",
                "result": {"case_statistics": [{"err_count": 3, "run_count": 100, "stream_count": 1}]},
            }
        ]
        npu_result_display(results)
        captured = capsys.readouterr()
        assert "FAIL" in captured.out

    def test_error_results(self, capsys):
        results = [{"device_id": 0, "task_name": "matmul", "error": "some error"}]
        npu_result_display(results)
        captured = capsys.readouterr()
        assert "FAIL" in captured.out

    def test_skipped_not_counted_as_fail(self, capsys):
        results = [{"device_id": 0, "task_name": "matmul", "error": "Skipped due to previous timeout"}]
        npu_result_display(results)
        captured = capsys.readouterr()
        assert "PASS" in captured.out

    def test_multiple_devices(self, capsys):
        results = [
            {
                "device_id": 0,
                "task_name": "matmul",
                "result": {"case_statistics": [{"err_count": 0, "run_count": 100, "stream_count": 1}]},
            },
            {
                "device_id": 1,
                "task_name": "conv2d",
                "result": {"case_statistics": [{"err_count": 2, "run_count": 100, "stream_count": 1}]},
            },
        ]
        npu_result_display(results)
        captured = capsys.readouterr()
        assert "0" in captured.out
        assert "1" in captured.out


@pytest.mark.unit
class TestScheduler:
    def test_op_dispatch(self):
        args = MagicMock()
        args.detect = "sdc"
        args.enable_profiling = False

        scheduler = Scheduler(args, "conv2d", {"dtype": ["float32"]}, 0)
        with patch("runtime.scheduler.OpFactory") as mock_of:
            mock_op = MagicMock()
            mock_op.case_run.return_value = {"error_records": [], "case_statistics": []}
            mock_of.get_op_instance.return_value = mock_op
            result = scheduler.run()
            mock_of.get_op_instance.assert_called_once_with("conv2d")
            assert "error_records" in result

    def test_model_dispatch(self):
        args = MagicMock()
        args.detect = "sdc"
        args.enable_profiling = False
        args.create_reference = None

        scheduler = Scheduler(args, "llama2", {}, 0)
        with patch("runtime.scheduler.OpFactory") as mock_of:
            mock_of.get_op_instance.return_value = None
            with patch("runtime.scheduler.ModelFactory") as mock_mf:
                mock_model = MagicMock()
                mock_model.run.return_value = {"error_records": [], "case_statistics": []}
                mock_mf.get_model_instance.return_value = mock_model
                scheduler.run()
                mock_mf.get_model_instance.assert_called_once_with("llama2")

    def test_unknown_task(self):
        args = MagicMock()
        args.detect = "sdc"
        args.enable_profiling = False

        scheduler = Scheduler(args, "nonexistent_task", {}, 0)
        with patch("runtime.scheduler.OpFactory") as mock_of:
            mock_of.get_op_instance.return_value = None
            with patch("runtime.scheduler.ModelFactory") as mock_mf:
                mock_mf.get_model_instance.return_value = None
                result = scheduler.run()
                assert result == "op_instance not found"
