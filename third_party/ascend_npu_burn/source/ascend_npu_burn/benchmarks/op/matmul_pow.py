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
import torch

from benchmarks.op.op_base import OpBase
from benchmarks.op.template.single import monitor_and_profile
from common.enum import TensorType
from common.log import logger
from common.tensor_info import TensorInfo, create_tensor
from common.thread import OpThread


class SelfMatmul(torch.nn.Module):
    def __init__(self):
        super(SelfMatmul, self).__init__()
        self.matmul = torch.matmul

    def forward(self, mat1, mat2):
        return self.matmul(mat1, mat2)


class SelfPow(torch.nn.Module):
    def __init__(self):
        super(SelfPow, self).__init__()

    @staticmethod
    def forward(x, exponent):
        return x.pow(exponent)


class MatmulPowOp(OpBase):
    def __init__(self):
        super().__init__()
        self.stream_count = 2

    @property
    def run_count(self):
        return self.task_config.get("run_count", 0)

    def run(self, tensor_mapping):
        run_count = self.task_config.get("run_count", 100)

        a = tensor_mapping["a"]
        b = tensor_mapping["b"]
        x = tensor_mapping["x"]
        exponent = 2.0

        torch.npu.synchronize()
        matmul = SelfMatmul().npu(self.device)
        pow_op = SelfPow().npu(self.device)

        matmul_stream = torch.npu.Stream(self.device)
        pow_stream = torch.npu.Stream(self.device)
        check_stream = torch.npu.Stream(self.device)

        thread_matmul = OpThread(check_stream, matmul_stream, run_count, self.device, matmul, (a, b))
        thread_pow = OpThread(check_stream, pow_stream, run_count, self.device, pow_op, (x, exponent))
        thread_matmul.start()
        thread_pow.start()
        thread_matmul.join()
        thread_pow.join()

        result = thread_matmul.result + thread_pow.result
        return result

    def case_run(self, enable_profiling: bool = False):
        error_records = []
        case_statistics = []
        cases = self.parse_cases()
        op_name = self.__class__.__name__
        logger.info(f"[{op_name}] NPU:{self.device} | Start running {len(cases)} case(s)")

        for case_idx, case in enumerate(cases):
            logger.info(f"[{op_name}] NPU:{self.device} | Running case {case_idx + 1}/{len(cases)}")
            with monitor_and_profile(op_name, case_idx, self.device, enable_profiling) as metrics:
                tensor_info = self.create_tensor_info(case)
                tensor_mapping = create_tensor(tensor_info, self.device)
                case_results = self.run(tensor_mapping)
                err_count = len(case_results)
                error_records.extend(case_results)
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
        dtypes = self.task_config.get("dtype", [])
        patterns = self.task_config.get("pattern", [""])
        shapes = self.task_config.get("shape", [])
        for dtype in dtypes:
            for pattern in patterns:
                for shape in shapes:
                    cases.append(
                        {
                            "dtype": dtype,
                            "pattern": pattern,
                            "shape": shape,
                        }
                    )
        return cases

    def create_tensor_info(self, case):
        dtype = case.get("dtype", "")
        pattern = case.get("pattern", "")
        shapes = case.get("shape", [])

        # matmul参数: bf16 左4096*4096，右4096*4096
        if len(shapes) >= 2:
            a_shape = shapes[0]
            b_shape = shapes[1]
        else:
            a_shape = [4096, 4096]
            b_shape = [4096, 4096]
        c_shape = [a_shape[0], b_shape[1]]

        # pow参数: bf16 1*2048
        x_shape = [1, 2048]

        tensor_info = {
            "a": TensorInfo(shape=a_shape, dtype=dtype, device=self.device, pattern=pattern),
            "b": TensorInfo(shape=b_shape, dtype=dtype, device=self.device, pattern=pattern),
            "c": TensorInfo(shape=c_shape, dtype=dtype, tensor_type=TensorType.OUTPUT.value),
            "x": TensorInfo(shape=x_shape, dtype=dtype, device=self.device, pattern=pattern),
            "pow_output": TensorInfo(shape=x_shape, dtype=dtype, tensor_type=TensorType.OUTPUT.value)
        }
        return tensor_info
