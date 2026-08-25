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
import os
import threading

import numpy as np
import torch
import torch_npu

from benchmarks.op.op_base import OpBase
from benchmarks.op.template.single import monitor_and_profile
from common.enum import DetectType
from common.tensor_info import TensorInfo, create_tensor
from common.log import logger


def set_deterministic(seed=42):
    torch.manual_seed(seed)
    torch_npu.npu.manual_seed(seed)
    torch_npu.npu.manual_seed_all(seed)
    torch.use_deterministic_algorithms(True)
    torch_npu.npu.empty_cache()
    np.random.seed(seed)
    os.environ['CUBLAS_WORKSPACE_CONFIG'] = ':4096:8'


class SelfLinear(torch.nn.Module):
    def __init__(self, in_features, out_features, bias=True):
        super(SelfLinear, self).__init__()
        self.linear = torch.nn.Linear(in_features, out_features, bias=bias)

    def forward(self, x):
        return self.linear(x)


class SelfSoftmax(torch.nn.Module):
    def __init__(self):
        super(SelfSoftmax, self).__init__()
        self.softmax = torch.nn.Softmax(dim=-1)

    def forward(self, x):
        return self.softmax(x)


class LinearSoftmaxOp(OpBase):
    def __init__(self):
        super().__init__()
        self.stream_count = 2

    @property
    def run_count(self):
        return self.task_config.get("run_count", 0)

    def set_sub_detect_types(self):
        self.sub_detect_types = [DetectType.SDC.value]

    def run(self, tensor_mapping, case):
        in_out = case["in.out"]
        num_loop = case["num_loop"]
        query = tensor_mapping["query"]
        set_deterministic(seed=42)
        torch_npu.npu.set_device(query.device)
        flinear = SelfLinear(in_out[0], in_out[1]).npu(query.device)
        fsoftmax = SelfSoftmax().npu(query.device)
        torch_npu.npu.synchronize()
        stream_linear = torch_npu.npu.Stream(query.device)
        stream_softmax = torch_npu.npu.Stream(query.device)

        stop_event = threading.Event()
        thread_mm = OperatorThread(query.device, stream_linear, num_loop, flinear, query, "Linear", stop_event)
        thread_soft = OperatorThread(query.device, stream_softmax, num_loop, fsoftmax, query, "Softmax", stop_event)

        thread_mm.start()
        thread_soft.start()

        # 等待两个线程完成
        thread_mm.join()
        thread_soft.join()

        num_fail = thread_mm.num_fail + thread_soft.num_fail
        logger.info(f"{self.__class__.__name__}Testing completed, error count：{num_fail}")
        return num_fail

    def case_run(self, enable_profiling: bool = False):
        error_records = []
        case_statistics = []
        cases = self.parse_cases()
        op_name = self.__class__.__name__

        logger.info(f"[{op_name}] NPU:{self.device} | Start running {len(cases)} case(s)")
        for case_idx, case in enumerate(cases):
            logger.info(f"[{op_name}] NPU:{self.device} | Running case {case_idx + 1}/{len(cases)}")

            err_count = 0
            with monitor_and_profile(op_name, case_idx, self.device, enable_profiling) as metrics:
                tensor_info = self.create_tensor_info(case)
                tensor_mapping = create_tensor(tensor_info, self.device)
                num_fail = self.run(tensor_mapping, case)
                if num_fail > 0:
                    err_count += num_fail
                    err_record = {
                        "op": self.__class__.__name__,
                        "detect_type": DetectType.SDC.value,
                        "device": self.device,
                        "fail_num": num_fail,
                        "result": False
                    }
                    error_records.append(err_record)
            # 添加case统计信息到结果中
            case_stats = {
                "op": op_name,
                "task": op_name,
                "case_idx": case_idx,
                "case": case,
                "run_count": self.run_count,
                "err_count": err_count,
                "stream_count": self.stream_count,
                "exetime": metrics['execution_time'],
                "result": err_count == 0
            }
            case_statistics.append(case_stats)
        return {
            "error_records": error_records,
            "case_statistics": case_statistics
        }

    def parse_cases(self):
        cases = []
        seq_length = self.task_config.get("seq_len", 1024)
        in_out = self.task_config.get("in.out", [1024, 1024])
        run_count = self.task_config.get("run_count", 100)
        for dtype in self.task_config.get("dtype", [""]):
            cases.append(
                {
                    "dtype": dtype,
                    "seq_length": seq_length,
                    "in.out": in_out,
                    "num_loop": run_count
                }
            )
        return cases

    def create_tensor_info(self, case):
        dtype = case["dtype"]
        in_out = case["in.out"]
        seq_length = case["seq_length"]
        tensor_info = {
            "query": TensorInfo(shape=[seq_length, in_out[0]], dtype=dtype, device=self.device)
        }
        return tensor_info


class OperatorThread(threading.Thread):
    def __init__(self, device, stream, iterations, function, data, key_str, stop_event):
        super().__init__()
        self.device = device
        self.stream = stream
        self.iterations = iterations
        self.op_func = function
        self.data = data
        self.num_fail = 0
        self.print_it = 2000
        self.key_str = key_str
        self.stop_event = stop_event

    def run(self):
        with torch.npu.stream(self.stream):
            golden_out = self.op_func(self.data)
            run_iter = 0
            while not self.stop_event.is_set():
                cur_out = self.op_func(self.data)
                eq_val = torch.eq(golden_out, cur_out).all()
                run_iter += 1
                if not eq_val:
                    self.num_fail += 1
                if self.key_str == "Linear":
                    if run_iter == self.iterations:
                        self.stop_event.set()

            torch_npu.npu.synchronize()
