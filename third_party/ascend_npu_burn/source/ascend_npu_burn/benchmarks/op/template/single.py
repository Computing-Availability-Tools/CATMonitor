#!/usr/bin/env python3
# -*- coding: utf-8 -*-
#
# This file is part of the MindCluster-AscendNPUBurn project.
# Copyright (c) 2026-2026 Huawei Technologies Co., Ltd. All Rights Reserved.
# MindCluster-AscendNPUBurn is licensed under Mulan PSL v2.
# You can use this software according to the terms and conditions of the Mulan PSL v2.
# You may obtain a copy of Mulan PSL v2 at:
#          http://license.coscl.org.cn/MulanPSL2
# THIS SOFTWARE IS PROVIDED ON AN "AS IS" BASIS, WITHOUT WARRANTIES OF ANY KIND,
# EITHER EXPRESS OR IMPLIED, INCLUDING BUT NOT LIMITED TO NON-INFRINGEMENT,
# MERCHANTABILITY OR FIT FOR A PARTICULAR PURPOSE.
# See the Mulan PSL v2 for more details.
# ===========================================================================
import datetime
import os
from contextlib import contextmanager
from datetime import timezone
from typing import Dict, List, Callable
import torch
import torch_npu
from torch_npu.profiler import _ExperimentalConfig, ProfilerLevel

from common.tensor_info import create_tensor
from common.thread import stream_sync_timeout
from detect.detect_base import DetectFactory
from common.log import logger
from common.const import TIMEOUT

POOL_SIZE = 10
CURRENT_FILE = os.path.abspath(__file__)
PROJECT_ROOT = os.path.dirname(os.path.dirname(os.path.dirname(os.path.dirname(CURRENT_FILE))))


def start_profiling(op_name: str, case_idx: int, device: int, enable_profiling: bool):
    """
    启动性能分析器

    Args:
        op_name: 操作名称
        case_idx: 用例索引
        device: 设备ID
        enable_profiling: 是否启用性能分析

    Returns:
        prof: 性能分析器实例
        prof_dir: 性能分析结果保存目录
    """
    if not enable_profiling or case_idx != 0:
        return None, None

    prof_dir = os.path.join(PROJECT_ROOT, f"output/profiling/{op_name}_case{case_idx}")
    os.makedirs(prof_dir, exist_ok=True)
    logger.info(f"[{op_name}] Starting Profiling, data will be saved to: {prof_dir}")
    # 定义 Profiler
    prof = torch_npu.profiler.profile(
        activities=[
            torch_npu.profiler.ProfilerActivity.CPU,
            torch_npu.profiler.ProfilerActivity.NPU
        ],
        record_shapes=True,
        profile_memory=True,
        with_stack=True,
        experimental_config=_ExperimentalConfig(profiler_level=ProfilerLevel.Level1)
    )
    prof.start()

    return prof, prof_dir


def stop_profiling(prof, op_name: str, case_idx: int, device: int, prof_dir: str):
    """
    停止性能分析器并保存结果

    Args:
        prof: 性能分析器实例
        op_name: 操作名称
        case_idx: 用例索引
        device: 设备ID
        prof_dir: 性能分析结果保存目录
    """
    if prof is None:
        return

    prof.stop()
    prof_file = os.path.join(prof_dir, f"{op_name}_case{case_idx}_npu{device}_prof.json")
    prof.export_chrome_trace(prof_file)
    logger.info(f"[{op_name}] NPU:{device} | ✅ Profiling results saved to: {prof_file}")


@contextmanager
def monitor_and_profile(op_name: str, case_idx: int, device: int,
                        enable_profiling: bool = False):
    """
    监控和性能分析的上下文管理器，自动处理启动和停止逻辑，并返回性能指标。

    Args:
        op_name: 操作名称
        case_idx: 用例索引
        device: 设备ID
        enable_profiling: 是否启用性能分析
    """
    # Setup
    case_start_time = datetime.datetime.now(tz=timezone.utc)
    prof, prof_dir = start_profiling(op_name, case_idx, device, enable_profiling)
    metrics = {}

    try:
        yield metrics
    finally:
        # Teardown
        stop_profiling(prof, op_name, case_idx, device, prof_dir)
        metrics['execution_time'] = (datetime.datetime.now(tz=timezone.utc) - case_start_time).total_seconds()


class SingleTemplate:
    task_config: Dict
    detect_type: str
    sub_detect_types: List
    device: int
    run: Callable

    @property
    def dtype(self):
        return self.task_config.get("dtype", [""])

    @property
    def run_count(self):
        return self.task_config.get("run_count", 0)

    @property
    def shape(self):
        return self.task_config.get("shape", [[]])

    @property
    def pattern(self):
        return self.task_config.get("pattern", [""])

    @property
    def detect_instances(self):
        return DetectFactory.get_detect_instances(self.detect_type, self.sub_detect_types)

    def parse_cases(self):
        cases = []
        for dtype in self.dtype:
            for pattern in self.pattern:
                for shape in self.shape:
                    cases.append(
                        {
                            "dtype": dtype,
                            "shape": shape,
                            "pattern": pattern
                        }
                    )
        return cases

    def create_tensor_info(self, case):
        raise NotImplementedError("create_tensor_info method must be implemented by subclass")

    def case_run(self, enable_profiling: bool = False):
        check_stream = torch.npu.Stream(device=self.device)
        default_stream = torch.npu.default_stream(device=self.device)
        error_records = []
        case_statistics = []
        cases = self.parse_cases()
        op_name = self.__class__.__name__
        if len(self.detect_instances) <= 0:
            return {}
        detect_instance = self.detect_instances[0]

        logger.info(f"[{op_name}] NPU:{self.device} | Start running {len(cases)} case(s)")

        for case_idx, case in enumerate(cases):
            logger.info(f"[{op_name}] NPU:{self.device} | Running case {case_idx + 1}/{len(cases)}")
            with monitor_and_profile(op_name, case_idx, self.device, enable_profiling) as metrics:
                tensor_info = self.create_tensor_info(case)
                tensor_mapping = create_tensor(tensor_info, self.device)
                golden_output = self.run(tensor_mapping)
                if stream_sync_timeout(torch.npu.current_stream(), TIMEOUT):
                    err_info = f"Stream synchronization timed out after {TIMEOUT} seconds"
                    return self._make_timeout_record(op_name, "golden", case_idx, case, err_info, has_timeout=True)

                err_count = 0

                res_tensor = torch.zeros(self.run_count, dtype=torch.bool, device=f"npu:{self.device}")
                time_list = []
                cur_output_list = [None] * POOL_SIZE
                check_events = [torch.npu.Event() for _ in range(POOL_SIZE)]

                for event in check_events:
                    event.record(default_stream)

                for i in range(self.run_count):
                    check_events[i % POOL_SIZE].synchronize()
                    cur_output_list[i % POOL_SIZE] = self.run(tensor_mapping)
                    if stream_sync_timeout(torch.npu.current_stream(), TIMEOUT):
                        err_info = f"Stream synchronization timed out after {TIMEOUT} seconds"
                        return self._make_timeout_record(op_name, i, case_idx, case, err_info, has_timeout=True)
                    forward_event = torch.npu.Event()
                    default_stream.record_event(forward_event)
                    time_list.append(datetime.datetime.now(tz=timezone.utc).strftime("%Y-%m-%d %H:%M:%S.%f"))

                    with torch.npu.stream(check_stream):
                        check_stream.wait_event(forward_event)
                        res_tensor[i] = detect_instance.core_detect(cur_output_list[i % POOL_SIZE], golden_output)
                        check_stream.record_event(check_events[i % POOL_SIZE])
                torch.npu.synchronize()

                if torch.any(res_tensor):
                    err_steps = torch.nonzero(res_tensor).flatten().tolist()
                    err_count += len(err_steps)
                    sample_steps = err_steps[:3]
                    logger.warning(
                        f"[{op_name}] NPU:{self.device} | ⚠️ Case {case_idx + 1} Failed! "
                        f"Error count: {len(err_steps)}/{self.run_count}. First few err steps: {sample_steps}")

                    for non_zero_index in err_steps:
                        err_record = {
                            "op": op_name,
                            "detect_type": detect_instance.SUB_DETECT_TYPE,
                            "step": non_zero_index,
                            "timestamp": time_list[non_zero_index],
                            "result": False
                        }
                        error_records.append(err_record)
                torch.npu.synchronize()

            case_stats = {
                "op": op_name,
                "task": op_name,
                "case_idx": case_idx,
                "case": case,
                "run_count": self.run_count,
                "err_count": err_count,
                "exetime": metrics['execution_time'],
                "result": err_count == 0
            }
            case_statistics.append(case_stats)

        return {
            "error_records": error_records,
            "case_statistics": case_statistics
        }

    def _make_timeout_record(self, op_name, step, case_idx, case, error=None, has_timeout=False):
        error_records = [{
            "op": op_name,
            "detect_type": self.detect_instances[0].SUB_DETECT_TYPE,
            "step": step,
            "timestamp": datetime.datetime.now(tz=datetime.timezone.utc).strftime("%Y-%m-%d %H:%M:%S.%f"),
            "result": error is None,
            **({"err_info": str(error)} if error else {}),
            "has_timeout": has_timeout
        }]
        case_statistics = [{
            "op": op_name,
            "task": op_name,
            "case_idx": case_idx,
            "case": case,
            "run_count": self.run_count,
            "err_count": 1,
            "exetime": TIMEOUT,
            "result": False
        }]

        return {
            "error_records": error_records,
            "case_statistics": case_statistics
        }
