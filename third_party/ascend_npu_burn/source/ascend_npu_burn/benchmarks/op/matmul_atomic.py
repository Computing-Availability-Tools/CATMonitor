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
from common.enum import DetectType, TensorType
from benchmarks.op.template.single import SingleTemplate
from common.tensor_info import TensorInfo


class MatmulAtomicOp(OpBase, SingleTemplate):
    def __init__(self):
        super().__init__()

    def set_sub_detect_types(self):
        self.sub_detect_types = [DetectType.SDC.value]

    def run(self, tensor_mapping):
        mat1 = tensor_mapping["mat1"]
        mat2 = tensor_mapping["mat2"]
        input_tensor = tensor_mapping["input_tensor"]
        output = input_tensor.clone().addmm_(mat1, mat2)
        return output

    def create_tensor_info(self, case):
        shapes = case["shape"]
        dtype = case["dtype"]
        pattern = case["pattern"]
        if not any([shapes, dtype]) and len(shapes) != 2:
            return None
        mat1_shape = shapes[0]
        mat2_shape = shapes[1]
        result_shape = [mat1_shape[0], mat2_shape[1]]
        tensor_info = {
            "mat1": TensorInfo(shape=mat1_shape, dtype=dtype, device=self.device, pattern=pattern),
            "mat2": TensorInfo(shape=mat2_shape, dtype=dtype, device=self.device, pattern=pattern),
            "input_tensor": TensorInfo(shape=result_shape, dtype=dtype, device=self.device, pattern=pattern,
                                       creator=lambda p_shape, p_dtype, p_device: torch.randn(p_shape[0], p_shape[1],
                                                                                              dtype=p_dtype,
                                                                                              device=p_device)),
            "result": TensorInfo(shape=result_shape, dtype=dtype, tensor_type=TensorType.OUTPUT.value)
        }
        return tensor_info
