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
from benchmarks.op.op_base import OpBase
from common.enum import DetectType, DtypeStr
from benchmarks.op.template.single import SingleTemplate
from common.tensor_info import TensorInfo


class WeightQuantBatchMatmulOp(OpBase, SingleTemplate):
    def __init__(self):
        super().__init__()

    def set_sub_detect_types(self):
        self.sub_detect_types = [DetectType.SDC.value]

    def run(self, tensor_mapping):
        x = tensor_mapping["x"]
        weight = tensor_mapping["weight"]
        anti_quant_scale = tensor_mapping["anti_quant_scale"]
        anti_quant_offset = tensor_mapping["anti_quant_offset"]
        quant_scale = tensor_mapping["quant_scale"]
        quant_offset = tensor_mapping["quant_offset"]

        quant_scale = torch_npu.npu_trans_quant_param(quant_scale, quant_offset)

        output = torch_npu.npu_weight_quant_batchmatmul(x, weight, anti_quant_scale, anti_quant_offset, quant_scale)
        return output

    def parse_cases(self):
        cases = []
        dtypes = self.task_config.get("dtype", [])
        patterns = self.task_config.get("pattern", [""])
        b = self.task_config.get("B", "")
        n = self.task_config.get("N", "")
        k = self.task_config.get("k", "")
        if not all([dtypes, b, n, k]):
            return cases
        for dtype in dtypes:
            for pattern in patterns:
                cases.append({
                    "dtype": dtype,
                    "pattern": pattern,
                    "b": b,
                    "n": n,
                    "k": k
                })
        return cases

    def create_tensor_info(self, case):
        dtype = case.get("dtype", "")
        pattern = case.get("pattern", "")
        b = case.get("b", "")
        n = case.get("n", "")
        k = case.get("k", "")

        x_shape = [b, n]
        weight_shape = [n, k]
        anti_quant_scale_shape = [1, k]
        anti_quant_offset_shape = [1, k]
        quant_scale_shape = [1, k]
        quant_offset_shape = [1, k]

        tensor_info = {
            "weight": TensorInfo(shape=weight_shape, dtype=DtypeStr.INT8.value, device=self.device,
                                 creator=lambda p_shape, p_dtype, p_device:
                                 torch.randint(low=-8, high=8, size=p_shape, dtype=p_dtype, device=p_device)),
            "x": TensorInfo(shape=x_shape, dtype=dtype, device=self.device, pattern=pattern),
            "anti_quant_scale": TensorInfo(shape=anti_quant_scale_shape, dtype=dtype, device=self.device,
                                           pattern=pattern),
            "anti_quant_offset": TensorInfo(shape=anti_quant_offset_shape, dtype=dtype, device=self.device,
                                           pattern=pattern),
            "quant_scale": TensorInfo(shape=quant_scale_shape, dtype=DtypeStr.FLOAT32.value, device=self.device,
                                           pattern=pattern),
            "quant_offset": TensorInfo(shape=quant_offset_shape, dtype=DtypeStr.FLOAT32.value, device=self.device,
                                           pattern=pattern)
        }
        return tensor_info
