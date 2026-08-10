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
from benchmarks.op.base_matmul_sdma import BaseMatmulSdma, BurstStressor
from ascend_npu_burn.custom_ops import sdma_burst, sync_with_timeout


def _burst_stress_func(stress_tensor, loop_num, **kwargs):
    sdma_burst(stress_tensor, loop_num)
    sync_with_timeout(kwargs.get('timeout_ms', 5000))
    import torch

    torch.npu.synchronize(device=kwargs.get('device', 0))


class MatmulSDMAOp(BaseMatmulSdma):
    STRESSOR_CLASS = BurstStressor
    STRESSOR_KWARGS = {
        'buffer_mb': 64,
        'target_gbps': 1330.0,
        'target_burst_sec': 2.0,
        'sleep_sec': 0,
        'timeout_ms': 5000,
    }
    STRESS_FUNC = _burst_stress_func
