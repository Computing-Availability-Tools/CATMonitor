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
from benchmarks.op.op_base import OpBase
from common.enum import DetectType, DtypeStr
from benchmarks.op.template.single import SingleTemplate
from common.tensor_info import TensorInfo


class MatmulAddFp32Op(OpBase, SingleTemplate):
    def __init__(self):
        super().__init__()

    def set_sub_detect_types(self):
        self.sub_detect_types = [DetectType.SDC.value]

    def run(self, tensor_mapping):
        x = tensor_mapping["x"]
        weight = tensor_mapping["weight"]
        c = tensor_mapping["c"].detach().clone()
        torch_npu._npu_matmul_add_fp32(x, weight, c)
        return c

    def parse_cases(self):
        cases = []
        dtypes = self.task_config.get("dtype", [])
        patterns = self.task_config.get("pattern", [""])
        b = self.task_config.get("B", "")
        n = self.task_config.get("N", "")
        k = self.task_config.get("K", "")
        if not all([dtypes, b, n, k]):
            return cases
        for dtype in dtypes:
            for pattern in patterns:
                cases.append({
                    "dtype": dtype,
                    "pattern": pattern,
                    "b": b,
                    "n": n,
                    "k": k,
                })
        return cases

    def create_tensor_info(self, case):
        dtype = case.get("dtype", "")
        pattern = case.get("pattern", "")
        b = case.get("b", "")
        n = case.get("n", "")
        k = case.get("k", "")

        x_shape = [b, n, k]
        weight_shape = [b, n, k]
        c_shape = [b, k, k]
        tensor_info = {
            "x": TensorInfo(shape=x_shape, dtype=dtype, device=self.device, pattern=pattern),
            "weight": TensorInfo(shape=weight_shape, dtype=dtype, device=self.device, pattern=pattern),
            "c": TensorInfo(shape=c_shape, dtype=DtypeStr.FLOAT32.value, device=self.device, pattern=pattern)
        }
        return tensor_info
