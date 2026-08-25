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

from common.const import DTYPE_MAPPING
from common.enum import TensorType
from common.npu_backend import get_free_memory
from common.data_load import data_load

DEFAULT_SIZE = 10 * 1024 ** 3


class TensorInfo:
    def __init__(
            self,
            shape=None,
            dtype="",
            device=0,
            pattern="",
            creator=None,
            tensor_type=TensorType.INPUT.value,
    ):
        self.shape = shape
        self.dtype = DTYPE_MAPPING[dtype]
        self.device_id = device
        self.pattern = pattern
        self.creator = creator
        self.tensor_type = tensor_type

    def create(self):
        if self.tensor_type == TensorType.OUTPUT.value:
            return self
        if self.creator is not None:
            return self.creator(self.shape, self.dtype, f"npu:{self.device_id}")
        return data_load.create_tensor(self.shape, self.dtype, self.device_id, self.pattern)

    def calc_tensor_size(self):
        tensor_size = 1
        for dim in self.shape:
            tensor_size *= dim
        dtype_size = torch.tensor([], dtype=self.dtype).element_size()
        tensor_size *= dtype_size
        return tensor_size


def sum_tensor_size(tensor_info):
    return sum([single.calc_tensor_size() for single in tensor_info.values() if isinstance(single, TensorInfo)])


def sum_input_tensor_size(tensor_info):
    return sum([single.calc_tensor_size() for single in tensor_info.values()
                if isinstance(single, TensorInfo) and single.tensor_type == TensorType.INPUT.value])


def create_default_tensor(p_shape, p_dtype, p_device):
    return torch.randn(p_shape, dtype=p_dtype, device=p_device)


def create_tensor(tensor_info, device, off_line=True):
    # necessary memory size
    necessary_tensor_size = sum_tensor_size(tensor_info)

    # free memory size
    free_memory_size = get_free_memory(device)

    # assume memory size
    assume_memory_size = int(free_memory_size * 0.8)  # 只占用80%的显存
    if off_line:
        assume_memory_size = free_memory_size
    if assume_memory_size < necessary_tensor_size:
        raise Exception("Not enough memory to run the op")

    # input tensor size
    input_tensor_size = sum_input_tensor_size(tensor_info)

    default_creator = None
    pre_init_size = assume_memory_size - necessary_tensor_size
    if pre_init_size < input_tensor_size:
        default_creator = create_default_tensor

    tensor_mapping = {}
    for key, info in tensor_info.items():
        if isinstance(info, TensorInfo):
            if info.creator is None and default_creator is not None:
                info.creator = default_creator
            tensor_mapping[key] = info.create()
        else:
            tensor_mapping[key] = info
    return tensor_mapping
