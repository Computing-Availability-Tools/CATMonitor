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
import math
from typing import Dict
import torch

from common.const import INT_TYPE_LIST
from entity.context import DataInfo


class DataLoad:
    __instance = None

    def __init__(self):
        self.data_size = 5  # 默认申请5G
        self.data_pool: Dict[str, DataInfo] = {}

    def __new__(cls, *args, **kwargs):
        if cls.__instance is None:
            cls.__instance = super().__new__(cls)
        return cls.__instance

    @staticmethod
    def init_by_pattern(data, pattern):
        if not pattern or pattern == "gauss_random":
            data.normal_(0, 1)
        elif pattern == "uniform_random":
            data.uniform_(0, 1)
        elif pattern == 'gauss_random_3':
            data.normal_(0, 3)
        elif pattern == 'gauss_random_8':
            data.normal_(0, 8)
        elif pattern == 'gauss_random_10p_zero':
            data.normal_(0, 1)
            mask = torch.rand_like(data) < 0.1
            data.masked_fill_(mask, 0)
        elif pattern == 'faulty_js_left':
            data.normal_(0, 1e-7)
        return data

    @staticmethod
    def init_by_type(data, dtype):
        if dtype == torch.bool:
            data.bernoulli_(0.5)    # 二值均匀分布
            return data
        max_num = 1
        min_num = 0
        if not dtype or dtype == torch.int8:
            max_num = 2 ** 7 - 1
            min_num = -2 ** 7
        elif dtype == torch.int16:
            max_num = 2 ** 15 - 1
            min_num = -2 ** 15
        elif dtype == torch.int32:
            max_num = 2 ** 31 - 1
            min_num = -2 ** 31
        elif dtype == torch.int64:
            max_num = 2 ** 63 - 1
            min_num = -2 ** 63
        elif dtype == torch.uint8:
            max_num = 2 ** 8 - 1
            min_num = 0
        data.uniform_(min_num, max_num)
        return data

    def _create_data(self, device_id, dtype, pattern):
        per_element_bytes = torch.tensor([], dtype=dtype).element_size()
        target_bytes = self.data_size * 1024 ** 3
        elements_num = target_bytes // per_element_bytes
        if pattern == "gauss_random_10p_zero":  # aclnnNonZeroV2的tensor shape <= 2 ** 31 -1
            elements_num = min(2 ** 31 -1, elements_num)
        if dtype == torch.uint8 or dtype == torch.int8:
            elements_num = elements_num // 4
        elif dtype == torch.int16:
            elements_num = elements_num // 2
        empty_data = torch.empty(elements_num, dtype=dtype, device=f"npu:{device_id}", requires_grad=False)
        if dtype in INT_TYPE_LIST:
            return DataLoad.init_by_type(empty_data, dtype)
        return DataLoad.init_by_pattern(empty_data, pattern)

    def _update_data(self, device_id, dtype, pattern):
        if device_id not in self.data_pool:
            data = self._create_data(device_id, dtype, pattern)
            self.data_pool[device_id] = DataInfo(device_id, dtype, pattern, data)
            return
        data_info = self.data_pool.get(device_id)
        if dtype != data_info.dtype or pattern != data_info.pattern:
            new_data = self._create_data(device_id, dtype, pattern)
            self.data_pool[device_id] = DataInfo(device_id, dtype, pattern, new_data)

            torch.npu.set_device(device_id)
            torch.npu.empty_cache()

    def _get_data(self, device_id, dtype, pattern):
        data_info = self.data_pool.get(device_id, None)
        if not data_info or dtype != data_info.dtype or pattern != data_info.pattern:
            self._update_data(device_id, dtype, pattern)
            data_info = self.data_pool.get(device_id)
        return data_info.data

    def create_tensor(self, shape, dtype, device_id, pattern):
        if not shape or not dtype or device_id is None:
            raise ValueError(f"create_tensor params: shape:{shape} or dtype:{dtype} or device_id:{device_id} is empty")
        if dtype in INT_TYPE_LIST:
            pattern = "uniform_random"
        if not pattern:
            pattern = "gauss_random"

        data = self._get_data(device_id, dtype, pattern)

        select_elements = math.prod(shape)
        total_elements = data.numel()
        start_idx = torch.randint(0, total_elements - select_elements + 1, (1,))
        selected_tensor = data[start_idx: start_idx + select_elements].detach().clone()
        selected_tensor = selected_tensor.reshape(shape)
        return selected_tensor

data_load = DataLoad()