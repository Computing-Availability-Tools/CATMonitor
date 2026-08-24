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
import threading

import torch

from benchmarks.op.op_base import OpBase
from benchmarks.op.base_matmul_sdma import BurstStressor
from benchmarks.op.template.single import monitor_and_profile
from common.enum import TensorType
from common.log import logger
from common.tensor_info import TensorInfo, create_tensor
from common.thread import OpThread, stream_sync_timeout
from common.const import TIMEOUT
from ascend_npu_burn.custom_ops import sdma_burst, sync_with_timeout


class SelfMatmul(torch.nn.Module):
    def __init__(self):
        super().__init__()
        self.matmul = torch.matmul

    def forward(self, mat1, mat2):
        return self.matmul(mat1, mat2)


class SelfRmsNorm(torch.nn.Module):
    def __init__(self, normalized_shape):
        super().__init__()
        self.rms_norm = torch.nn.functional.rms_norm
        self.normalized_shape = normalized_shape

    def forward(self, x):
        return self.rms_norm(x, self.normalized_shape)


class SelfRelu(torch.nn.Module):
    def forward(self, x):
        return torch.relu(x)


class SelfPow(torch.nn.Module):
    @staticmethod
    def forward(x, exponent):
        return x.pow(exponent)


def _burst_stress_func(stress_tensor, loop_num, **kwargs):
    sdma_burst(stress_tensor, loop_num)
    sync_with_timeout(kwargs.get('timeout_ms', 5000))
    torch.npu.synchronize(device=kwargs.get('device', 0))


class VectorStressor:
    def __init__(self, stream, device, op_func, params):
        self.stream = stream
        self.device = device
        self.op_func = op_func
        self.params = params
        self.stop_event = threading.Event()
        self.timeout = False
        self.thread = threading.Thread(target=self._worker_loop)

    def _worker_loop(self):
        torch.npu.set_device(f"npu:{self.device}")
        op_name = self.op_func.__class__.__name__
        with torch.npu.stream(self.stream):
            while not self.stop_event.is_set():
                self.op_func(*self.params)
                if stream_sync_timeout(torch.npu.current_stream(), TIMEOUT):
                    logger.error(f"[{op_name}] NPU:{self.device} Stream sync timed out")
                    self.timeout = True
                    return
        torch.npu.synchronize()

    def start(self):
        self.thread.start()

    def stop(self):
        self.stop_event.set()
        self.thread.join()


CASE_RMS_NORM = "rms_norm"
CASE_RELU = "relu"
CASE_POW = "pow"

VECTOR_CASES = [CASE_RMS_NORM, CASE_RELU, CASE_POW]


class CubeVectorSdmaOp(OpBase):
    def __init__(self):
        super().__init__()
        self.stream_count = 1

    @property
    def run_count(self):
        return self.task_config.get("run_count", 0)

    def _start_sdma_stressor(self):
        stressor = BurstStressor(
            device=self.device,
            buffer_mb=128,
            target_gbps=1330.0,
            target_burst_sec=2.0,
            sleep_sec=0,
            timeout_ms=5000,
            stress_func=_burst_stress_func,
            stress_func_kwargs={'timeout_ms': 5000, 'device': self.device},
        )
        stressor.start()
        return stressor

    def run(self, tensor_mapping):
        run_count = self.task_config.get("run_count", 100)
        vector_op = tensor_mapping.get("vector_op", CASE_RMS_NORM)

        a = tensor_mapping["a"]
        b = tensor_mapping["b"]

        matmul = SelfMatmul().npu(self.device)

        if vector_op == CASE_RMS_NORM:
            x = tensor_mapping["x"]
            normalized_shape = x.shape[-1:]
            vector_module = SelfRmsNorm(normalized_shape).npu(self.device)
            vector_params = (x,)
        elif vector_op == CASE_RELU:
            x = tensor_mapping["x"]
            vector_module = SelfRelu().npu(self.device)
            vector_params = (x,)
        elif vector_op == CASE_POW:
            x = tensor_mapping["x"]
            vector_module = SelfPow().npu(self.device)
            vector_params = (x, 2.0)
        else:
            raise ValueError(f"Unknown vector_op: {vector_op}")

        torch.npu.synchronize()

        vector_stream = torch.npu.Stream(self.device)
        matmul_stream = torch.npu.Stream(self.device)
        check_stream = torch.npu.Stream(self.device)

        vector_stressor = VectorStressor(vector_stream, self.device, vector_module, vector_params)
        thread_matmul = OpThread(check_stream, matmul_stream, run_count, self.device, matmul, (a, b))

        vector_stressor.start()
        thread_matmul.start()

        thread_matmul.join()
        vector_stressor.stop()

        return thread_matmul.result

    def case_run(self, enable_profiling: bool = False):
        error_records = []
        case_statistics = []
        cases = self.parse_cases()
        op_name = self.__class__.__name__
        logger.info(f"[{op_name}] NPU:{self.device} | Start running {len(cases)} case(s)")

        for case_idx, case in enumerate(cases):
            logger.info(f"[{op_name}] NPU:{self.device} | Running case {case_idx + 1}/{len(cases)}")

            stressor = self._start_sdma_stressor()

            try:
                with monitor_and_profile(op_name, case_idx, self.device, enable_profiling) as metrics:
                    tensor_info = self.create_tensor_info(case)
                    tensor_mapping = create_tensor(tensor_info, self.device)
                    case_results = self.run(tensor_mapping)
                    err_count = len(case_results)
                    error_records.extend(case_results)
            finally:
                stressor.stop()

            case_stats = {
                "op": op_name,
                "task": op_name,
                "case_idx": case_idx,
                "case": case,
                "run_count": self.run_count,
                "err_count": err_count,
                "stream_count": self.stream_count,
                "exetime": metrics['execution_time'],
                "result": err_count == 0,
            }
            case_statistics.append(case_stats)
        return {"error_records": error_records, "case_statistics": case_statistics}

    def parse_cases(self):
        cases = []
        dtypes = self.task_config.get("dtype", [])
        patterns = self.task_config.get("pattern", [""])
        shapes = self.task_config.get("shape", [])
        vector_ops = self.task_config.get("vector_op", VECTOR_CASES)
        for dtype in dtypes:
            for pattern in patterns:
                for shape in shapes:
                    for vector_op in vector_ops:
                        cases.append(
                            {
                                "dtype": dtype,
                                "pattern": pattern,
                                "shape": shape,
                                "vector_op": vector_op,
                            }
                        )
        return cases

    def create_tensor_info(self, case):
        dtype = case.get("dtype", "")
        pattern = case.get("pattern", "")
        shapes = case.get("shape", [])
        vector_op = case.get("vector_op", CASE_RMS_NORM)

        if len(shapes) >= 2 and len(shapes[0]) >= 2 and len(shapes[1]) >= 2:
            a_shape = shapes[0]
            b_shape = shapes[1]
        else:
            a_shape = [4096, 4096]
            b_shape = [4096, 4096]
        c_shape = [a_shape[0], b_shape[1]]

        if vector_op == CASE_RMS_NORM:
            x_shape = [1, 2948]
        else:
            x_shape = [1, 2048]

        tensor_info = {
            "a": TensorInfo(shape=a_shape, dtype=dtype, device=self.device, pattern=pattern),
            "b": TensorInfo(shape=b_shape, dtype=dtype, device=self.device, pattern=pattern),
            "c": TensorInfo(shape=c_shape, dtype=dtype, tensor_type=TensorType.OUTPUT.value),
            "x": TensorInfo(shape=x_shape, dtype=dtype, device=self.device, pattern=pattern),
            "vector_op": case.get("vector_op", CASE_RMS_NORM),
        }
        return tensor_info
