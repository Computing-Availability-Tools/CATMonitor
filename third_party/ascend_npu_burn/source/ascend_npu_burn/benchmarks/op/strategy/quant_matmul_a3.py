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
import torch_npu
from benchmarks.op.strategy.base_strategy import OpChipStrategy
from common.enum import DtypeStr
from common.tensor_info import TensorInfo
from common.enum import TensorType


class QuantMatmulA3Strategy(OpChipStrategy):
    def parse_cases(self, task_config):
        cases = []
        dtypes = task_config.get("dtype", [])
        patterns = task_config.get("pattern", [""])
        m = task_config.get("M", "")
        k = task_config.get("K", "")
        n = task_config.get("N", "")
        if not any([dtypes, m, k, n]):
            return cases
        for dtype in dtypes:
            for pattern in patterns:
                cases.append(
                    {
                        "dtype": dtype,
                        "pattern": pattern,
                        "m": m,
                        "k": k,
                        "n": n,
                    }
                )
        return cases

    def create_tensor_info(self, case, device):
        m = case["m"]
        k = case["k"]
        n = case["n"]
        dtype = case["dtype"]
        pattern = case["pattern"]
        x1_shape = None
        x2_shape = None
        scale_dtype = DtypeStr.FLOAT32.value
        out_dtype = DtypeStr.FLOAT16.value

        if dtype == DtypeStr.INT32.value:
            x1_shape = [m, k // 8]
            x2_shape = [k, n // 8]
        elif dtype == DtypeStr.INT8.value:
            x1_shape = [m, k]
            x2_shape = [k, n]
        scale_shape = [n]
        out_shape = [m, n]

        tensor_info = {
            "x1": TensorInfo(shape=x1_shape, dtype=dtype, device=device, pattern=pattern),
            "x2": TensorInfo(shape=x2_shape, dtype=dtype, device=device, pattern=pattern),
            "scale": TensorInfo(shape=scale_shape, dtype=scale_dtype, device=device, pattern=pattern),
            "out": TensorInfo(shape=out_shape, dtype=out_dtype, tensor_type=TensorType.OUTPUT.value),
        }
        return tensor_info

    def run(self, tensor_mapping):
        x1 = tensor_mapping["x1"]
        x2 = tensor_mapping["x2"]
        scale = tensor_mapping["scale"]
        out = tensor_mapping["out"]
        output = torch_npu.npu_quant_matmul(x1, x2, scale, output_dtype=out.dtype)
        return output
