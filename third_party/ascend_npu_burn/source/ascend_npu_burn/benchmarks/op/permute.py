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
from benchmarks.op.template.single import SingleTemplate
from common.enum import DetectType, TensorType
from common.tensor_info import TensorInfo


class PermuteOp(OpBase, SingleTemplate):
    def __init__(self):
        super().__init__()

    def set_sub_detect_types(self):
        self.sub_detect_types = [DetectType.SDC.value]

    def run(self, tensor_mapping):
        input_tensor = tensor_mapping["input"] # 参数是NCHW

        nhwc_tensor = input_tensor.permute(0, 2, 3, 1)  # 参数是NCHW -> NHWC
        nhwc_bais_tensor = torch.ones_like(nhwc_tensor, device=f"npu:{self.device}")
        nhwc_output = torch.add(nhwc_tensor, nhwc_bais_tensor)

        nchw_tensor = nhwc_output.permute(0, 3, 1, 2) # 参数是NHWC -> NCHW
        nchw_bais_tensor = torch.ones_like(nchw_tensor, device=f"npu:{self.device}")
        nchw_output = torch.add(nchw_tensor, nchw_bais_tensor)
        return nchw_output

    def parse_cases(self):
        cases = []
        dtypes = self.task_config.get("dtype", [])
        patterns = self.task_config.get("pattern", [""])
        n = self.task_config.get("N", "")
        c = self.task_config.get("C", "")
        h = self.task_config.get("H", "")
        w = self.task_config.get("W", "")
        if not all([dtypes, n, c, h, w]):
            return cases
        for dtype in dtypes:
            for pattern in patterns:
                cases.append({
                    "dtype": dtype,
                    "pattern": pattern,
                    "n": n,
                    "c": c,
                    "h": h,
                    "w": w,
                })
        return cases

    def create_tensor_info(self, case):
        dtype = case["dtype"]
        pattern = case["pattern"]
        n = case["n"]
        c = case["c"]
        h = case["h"]
        w = case["w"]
        input_shape = [n, c, h, w]

        tensor_info = {
            "input": TensorInfo(shape=input_shape, dtype=dtype, device=self.device, pattern=pattern),
            "out": TensorInfo(shape=input_shape, dtype=dtype, tensor_type=TensorType.OUTPUT.value)
        }
        return tensor_info
