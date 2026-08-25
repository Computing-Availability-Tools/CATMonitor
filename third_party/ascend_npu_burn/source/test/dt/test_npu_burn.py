#!/usr/bin/env python3
# -*- coding: utf-8 -*-
# Copyright 2026 Huawei Technologies Co., Ltd
import pytest
import argparse
from unittest.mock import MagicMock, patch

from npu_burn import parse_args, get_active_numa_info, generate_run_cases


@pytest.mark.unit
class TestParseArgs:

    def test_default_values(self):
        with patch("sys.argv", ["npu_burn.py"]):
            args = parse_args()
            assert args.run_case is None
            assert args.group is None
            assert args.create_reference is None
            assert args.mode is None
            assert args.device == "all"
            assert args.log_level == "info"
            assert args.enable_profiling is False

    def test_parse_args(self):
        with patch("sys.argv", ["npu_burn.py", "-r", "matmul,conv2d", "-c", "gpt3", "--sdc_detect", "-d", "0,1,2",
                                "--log_level", "debug", "--enable_profiling"]):
            args = parse_args()
            assert args.run_case == "matmul,conv2d"
            assert args.create_reference == "gpt3"
            assert args.device == "0,1,2"
            assert args.log_level == "debug"
            assert args.enable_profiling is True

    def test_group(self):
        with patch("sys.argv", ["npu_burn.py", "-g", "all_ops"]):
            args = parse_args()
            assert args.group == "all_ops"


@pytest.mark.unit
class TestGetActiveNumaInfo:

    def test_all_devices(self):
        args = MagicMock()
        args.device = "all"
        with patch("npu_burn.numa_npu_topo", {0: [0, 1], 1: [2, 3]}):
            ids, numa_map, cnt = get_active_numa_info(args)
            assert ids == [0, 1]
            assert numa_map == {0: [0, 1], 1: [2, 3]}
            assert cnt == 2

    def test_specific_devices(self):
        args = MagicMock()
        args.device = "0,2"
        with patch("npu_burn.numa_npu_topo", {0: [0, 1], 1: [2, 3]}):
            ids, numa_map, cnt = get_active_numa_info(args)
            assert 0 in numa_map
            assert 1 in numa_map
            assert numa_map[0] == [0]
            assert numa_map[1] == [2]

    def test_no_matching_devices(self):
        args = MagicMock()
        args.device = "99"
        with patch("npu_burn.numa_npu_topo", {0: [0, 1], 1: [2, 3]}):
            ids, numa_map, cnt = get_active_numa_info(args)
            assert cnt == 0

    def test_empty_numa_topo(self):
        args = MagicMock()
        args.device = "all"
        with patch("npu_burn.numa_npu_topo", {}):
            ids, numa_map, cnt = get_active_numa_info(args)
            assert cnt == 0


@pytest.mark.unit
class TestGenerateRunCases:

    def _make_args(self, run_case=None, group=None, create_reference=None):
        args = MagicMock()
        args.run_case = run_case
        args.group = group
        args.create_reference = create_reference
        return args

    def test_no_args_returns_empty(self):
        args = self._make_args()
        with patch("npu_burn.ConfigManager") as mock_cfg_cls:
            mock_cfg = MagicMock()
            mock_cfg_cls.return_value = mock_cfg
            cases = generate_run_cases(args)
            assert len(cases) == 0

    def test_run_case_by_name(self):
        args = self._make_args(run_case="matmul")
        mock_case = MagicMock()
        with patch("npu_burn.ConfigManager") as mock_cfg_cls:
            mock_cfg = MagicMock()
            mock_cfg.get_by_name.return_value = mock_case
            mock_cfg_cls.return_value = mock_cfg
            cases = generate_run_cases(args)
            assert len(cases) == 1

    def test_run_case_by_seq(self):
        args = self._make_args(run_case="1")
        mock_case = MagicMock()
        with patch("npu_burn.ConfigManager") as mock_cfg_cls:
            mock_cfg = MagicMock()
            mock_cfg.get_by_seq.return_value = mock_case
            mock_cfg_cls.return_value = mock_cfg
            cases = generate_run_cases(args)
            assert len(cases) == 1

    def test_run_case_not_found(self):
        args = self._make_args(run_case="nonexistent")
        with patch("npu_burn.ConfigManager") as mock_cfg_cls:
            mock_cfg = MagicMock()
            mock_cfg.get_by_name.return_value = None
            mock_cfg_cls.return_value = mock_cfg
            cases = generate_run_cases(args)
            assert len(cases) == 0

    def test_group_cases(self):
        args = self._make_args(group="all_ops")
        mock_cases = [MagicMock(), MagicMock()]
        with patch("npu_burn.ConfigManager") as mock_cfg_cls:
            mock_cfg = MagicMock()
            mock_cfg.get_by_group.return_value = mock_cases
            mock_cfg_cls.return_value = mock_cfg
            cases = generate_run_cases(args)
            assert len(cases) == 2

    def test_create_reference(self):
        args = self._make_args(create_reference="gpt3")
        mock_case = MagicMock()
        with patch("npu_burn.ConfigManager") as mock_cfg_cls:
            mock_cfg = MagicMock()
            mock_cfg.get_by_name.return_value = mock_case
            mock_cfg_cls.return_value = mock_cfg
            cases = generate_run_cases(args)
            assert len(cases) == 1

    def test_conflict_run_case_and_group(self):
        args = self._make_args(run_case="matmul", group="all_ops")
        with patch("npu_burn.ConfigManager") as mock_cfg_cls:
            mock_cfg = MagicMock()
            mock_cfg_cls.return_value = mock_cfg
            cases = generate_run_cases(args)
            assert len(cases) == 0

    def test_multiple_run_cases(self):
        args = self._make_args(run_case="1,2")
        mock_case1 = MagicMock()
        mock_case2 = MagicMock()
        with patch("npu_burn.ConfigManager") as mock_cfg_cls:
            mock_cfg = MagicMock()
            mock_cfg.get_by_seq.side_effect = [mock_case1, mock_case2]
            mock_cfg_cls.return_value = mock_cfg
            cases = generate_run_cases(args)
            assert len(cases) == 2
