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
import torch_npu
import math
from benchmarks.op.strategy.base_strategy import OpChipStrategy
from common.const import DTYPE_MAPPING
from common.enum import DtypeStr
from common.tensor_info import TensorInfo
from common.enum import TensorType


def _mx_quant_run(tensor_mapping):
    x1 = tensor_mapping["x1"]
    x2 = tensor_mapping["x2"]
    scale = tensor_mapping["scale"]
    per_token_scale = tensor_mapping["per_token_scale"]
    out = tensor_mapping["out"]
    x1_real_data = tensor_mapping["x1_real_data"]
    x2_real_data = tensor_mapping["x2_real_data"]

    x2 = x2.transpose(-1, -2)
    scale = scale.transpose(0, 1)

    output = torch_npu.npu_quant_matmul(
        x1,
        x2,
        scale,
        bias=None,
        pertoken_scale=per_token_scale,
        pertoken_scale_dtype=torch_npu.float8_e8m0fnu,
        output_dtype=out.dtype,
        group_sizes=[1, 1, 32],
        x1_dtype=DTYPE_MAPPING[x1_real_data],
        x2_dtype=DTYPE_MAPPING[x2_real_data],
        scale_dtype=torch_npu.float8_e8m0fnu,
    )
    return output


def _other_quant_run(tensor_mapping):
    x1 = tensor_mapping["x1"]
    x2 = tensor_mapping["x2"]
    scale = tensor_mapping["scale"]
    per_token_scale = tensor_mapping["per_token_scale"]
    out = tensor_mapping["out"]

    output = torch_npu.npu_quant_matmul(
        x1, x2, scale=scale, bias=None, pertoken_scale=per_token_scale, output_dtype=out.dtype
    )
    return output


def _other_quant_tensor_info(dtype, pattern, m, k, n, device):
    x1_shape = [m, k]
    x1_dtype = dtype
    x2_shape = [k, n]
    x2_dtype = dtype
    per_token_scale_shape = [m]
    per_token_scale_dtype = DtypeStr.FLOAT32.value
    scale_shape = [n]
    scale_dtype = DtypeStr.FLOAT32.value
    out_shape = [m, n]
    out_dtype = DtypeStr.BFLOAT16.value

    if dtype in [DtypeStr.FLOAT8_E4M3FN.value, DtypeStr.HIFLOAT8.value]:
        x1_dtype = DtypeStr.INT8.value
        x2_dtype = DtypeStr.INT8.value

    tensor_info = {
        "x1": TensorInfo(shape=x1_shape, dtype=x1_dtype, device=device, pattern=pattern),
        "x2": TensorInfo(shape=x2_shape, dtype=x2_dtype, device=device, pattern=pattern),
        "scale": TensorInfo(shape=scale_shape, dtype=scale_dtype, device=device, pattern=pattern),
        "per_token_scale": TensorInfo(
            shape=per_token_scale_shape, dtype=per_token_scale_dtype, device=device, pattern=pattern
        ),
        "out": TensorInfo(shape=out_shape, dtype=out_dtype, tensor_type=TensorType.OUTPUT.value),
    }
    return tensor_info


def _mx_quant_tensor_info(dtype, pattern, m, k, n, device):
    act_k = k // 2 if dtype == DtypeStr.FLOAT4_E2M1FN_X2.value else k
    k_group_64 = math.ceil(k / 64)
    k_group_2 = k // 2

    x1_shape = [m, act_k]
    x1_dtype = dtype
    x1_real_data = dtype
    x2_shape = [n, k_group_2]
    x2_dtype = dtype
    x2_real_data = dtype
    per_token_scale_shape = [m, k_group_64, 2]
    per_token_scale_dtype = DtypeStr.INT8.value
    scale_shape = [n, k_group_64, 2]
    scale_dtype = DtypeStr.INT8.value
    out_shape = [m, n]
    out_dtype = DtypeStr.BFLOAT16.value

    if dtype == DtypeStr.FLOAT4_E2M1FN_X2.value:
        x1_dtype = DtypeStr.INT8.value
        x2_dtype = DtypeStr.INT8.value

    tensor_info = {
        "x1": TensorInfo(shape=x1_shape, dtype=x1_dtype, device=device, pattern=pattern),
        "x2": TensorInfo(shape=x2_shape, dtype=x2_dtype, device=device, pattern=pattern),
        "scale": TensorInfo(
            shape=scale_shape,
            dtype=scale_dtype,
            device=device,
            creator=lambda p_shape, p_dtype, p_device: torch.randint(1, 10, size=p_shape, dtype=p_dtype).npu(
                device=p_device
            ),
        ),
        "per_token_scale": TensorInfo(
            shape=per_token_scale_shape,
            dtype=per_token_scale_dtype,
            device=device,
            creator=lambda p_shape, p_dtype, p_device: torch.randint(1, 10, size=p_shape, dtype=p_dtype).npu(
                device=p_device
            ),
        ),
        "out": TensorInfo(shape=out_shape, dtype=out_dtype, tensor_type=TensorType.OUTPUT.value),
        "x1_real_data": x1_real_data,
        "x2_real_data": x2_real_data,
    }
    return tensor_info


class QuantMatmulA5Strategy(OpChipStrategy):
    def run(self, tensor_mapping):
        x1_real_data = tensor_mapping.get("x1_real_data", "")
        if x1_real_data and x1_real_data == DtypeStr.FLOAT4_E2M1FN_X2.value:
            return _mx_quant_run(tensor_mapping)
        else:
            return _other_quant_run(tensor_mapping)

    def parse_cases(self, task_config):
        cases = []
        dtypes = task_config.get("dtype", [])
        patterns = task_config.get("pattern", [""])
        # M:seq_len/token, K:hidden_size, N:head_size
        shapes = task_config.get("MKN", [])
        if not any([dtypes, shapes]):
            return cases
        for dtype in dtypes:
            for pattern in patterns:
                for shape in shapes:
                    if len(shape) != 3:
                        continue
                    cases.append(
                        {
                            "dtype": dtype,
                            "pattern": pattern,
                            "m": shape[0],
                            "k": shape[1],
                            "n": shape[2],
                        }
                    )
        return cases

    def create_tensor_info(self, case, device):
        dtype = case["dtype"]
        pattern = case["pattern"]
        m = case["m"]
        k = case["k"]
        n = case["n"]
        if dtype == DtypeStr.FLOAT4_E2M1FN_X2.value:
            return _mx_quant_tensor_info(dtype, pattern, m, k, n, device)
        else:
            return _other_quant_tensor_info(dtype, pattern, m, k, n, device)
