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
import torch.nn.functional as F
from benchmarks.op.op_base import OpBase
from common.enum import DetectType
from benchmarks.op.template.single import SingleTemplate
from common.tensor_info import TensorInfo


class Conv2dOp(OpBase, SingleTemplate):
    def __init__(self):
        super().__init__()

    def set_sub_detect_types(self):
        self.sub_detect_types = [DetectType.SDC.value]

    def parse_cases(self):
        cases = []
        input_data = self.task_config.get("input_data", [])
        weight_data = self.task_config.get("weight_data", [])
        for dtype in self.dtype:
            for pattern in self.pattern:
                cases.append({"dtype": dtype, "pattern": pattern, "input_data": input_data, "weight_data": weight_data})
        return cases

    def run(self, tensor_mapping):
        input_data = tensor_mapping["input_data"].clone().detach().requires_grad_(True)
        weight_data = tensor_mapping["weight_data"].clone().detach().requires_grad_(True)
        bias_data = tensor_mapping["bias_data"].clone().detach().requires_grad_(True)
        target_grad = tensor_mapping["target_grad"].clone().detach()
        output = F.conv2d(input_data, weight_data, bias_data, stride=1, padding=1)
        output.backward(target_grad)
        return input_data.grad

    def create_tensor_info(self, case):
        dtype = case["dtype"]
        pattern = case["pattern"]
        input_shape = case["input_data"]  # [N, Cin, Hin, Win]
        weight_shape = case["weight_data"]  # [Cout, Cin, Kernel_h, Kernel_w]
        padding = 1
        stride = 1
        if not any([input_shape, weight_shape]) and len(input_shape) != 4 and len(weight_shape) != 4:
            return None
        kernel_h = weight_shape[2]
        kernel_w = weight_shape[3]
        out_h = int((input_shape[2] + 2 * padding - kernel_h) / stride + 1)
        out_w = int((input_shape[3] + 2 * padding - kernel_w) / stride + 1)
        target_grad_shape = [input_shape[0], weight_shape[0], out_h, out_w]
        bias_data = [weight_shape[0]]

        tensor_info = {
            "input_data": TensorInfo(shape=input_shape, dtype=dtype, device=self.device, pattern=pattern),
            "weight_data": TensorInfo(shape=weight_shape, dtype=dtype, device=self.device, pattern=pattern),
            "bias_data": TensorInfo(shape=bias_data, dtype=dtype, device=self.device, pattern=pattern,
                                    creator=lambda p_shape, p_dtype, p_device: torch.randn(p_shape[0], dtype=p_dtype,
                                                                                           device=p_device)),
            "target_grad": TensorInfo(shape=target_grad_shape, dtype=dtype, device=self.device, pattern=pattern,
                                      creator=lambda p_shape, p_dtype, p_device: torch.randn(p_shape[0], p_shape[1],
                                                                                             p_shape[2], p_shape[3],
                                                                                             dtype=p_dtype,
                                                                                             device=p_device)),
        }
        return tensor_info
