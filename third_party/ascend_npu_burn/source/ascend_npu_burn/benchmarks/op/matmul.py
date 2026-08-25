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
from common.enum import DetectType, TensorType, DtypeStr
from benchmarks.op.template.single import SingleTemplate
from common.tensor_info import TensorInfo


class MatmulOp(OpBase, SingleTemplate):
    def __init__(self):
        super().__init__()

    def set_sub_detect_types(self):
        self.sub_detect_types = [DetectType.SDC.value]

    def run(self, tensor_mapping):
        a = tensor_mapping["a"]
        b = tensor_mapping["b"]
        allow_hf32 = tensor_mapping.get("allow_hf32", False)
        torch.npu.set_device(self.device)  # 切换到目标卡，让hf32只作用于目标卡的所有流
        if allow_hf32:
            torch_npu.npu.matmul.allow_hf32 = True
        else:
            torch_npu.npu.matmul.allow_hf32 = False
        output = torch.matmul(a, b)
        return output

    def create_tensor_info(self, case):
        shapes = case.get("shape", [])
        dtype = case.get("dtype", "")
        pattern = case.get("pattern", "")
        if not any([shapes, dtype]) and len(shapes) != 2:
            return None
        a_shape = shapes[0]
        b_shape = shapes[1]
        if len(a_shape) != 2 or len(b_shape) != 2:
            return None
        c_shape = [a_shape[0], b_shape[1]]
        tensor_info = None
        if dtype == "hfloat32":
            tensor_info = {
                "a": TensorInfo(shape=a_shape, dtype=DtypeStr.FLOAT32.value, device=self.device, pattern=pattern),
                "b": TensorInfo(shape=b_shape, dtype=DtypeStr.FLOAT32.value, device=self.device, pattern=pattern),
                "c": TensorInfo(shape=c_shape, dtype=DtypeStr.FLOAT32.value, tensor_type=TensorType.OUTPUT.value),
                "allow_hf32": True
            }
            return tensor_info
        if pattern == "faulty_js":
            tensor_info = {
                "a": TensorInfo(shape=a_shape, dtype=dtype, device=self.device, pattern='faulty_js_left'),
                "b": TensorInfo(shape=b_shape, dtype=dtype, device=self.device, pattern='gauss_random'),
                "c": TensorInfo(shape=c_shape, dtype=dtype, tensor_type=TensorType.OUTPUT.value)
            }
        else:
            tensor_info = {
                "a": TensorInfo(shape=a_shape, dtype=dtype, device=self.device, pattern=pattern),
                "b": TensorInfo(shape=b_shape, dtype=dtype, device=self.device, pattern=pattern),
                "c": TensorInfo(shape=c_shape, dtype=dtype, tensor_type=TensorType.OUTPUT.value)
            }
        return tensor_info
