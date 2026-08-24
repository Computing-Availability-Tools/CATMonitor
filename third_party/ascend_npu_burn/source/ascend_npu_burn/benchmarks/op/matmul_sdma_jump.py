#!/usr/bin/env python3
# -*- coding: utf-8 -*-
# Copyright 2026 Huawei Technologies Co., Ltd
#
# Licensed under the Apache License, Version 2.0 (the "License");
# you may not use this file except in compliance with the License.
# You may obtain a copy of the License at
#
# http://www.apache.org/licenses/LICENSE-2.0
#
# Unless required by applicable law or agreed to in writing, software
# distributed under the License is distributed on an "AS IS" BASIS,
# WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
# See the License for the specific language governing permissions and
# limitations under the License.
# ==============================================================================
from benchmarks.op.base_matmul_sdma import BaseMatmulSdma, JumpStressor
from ascend_npu_burn.custom_ops import sdma_jump


def _jump_stress_func(stress_tensor, loop_num, sleep_ms, **kwargs):
    sdma_jump(stress_tensor, loop_num, sleep_ms)
    import torch

    torch.npu.synchronize(device=kwargs.get('device', 0))


class MatmulSDMAJumpOp(BaseMatmulSdma):
    STRESSOR_CLASS = JumpStressor
    STRESSOR_KWARGS = {
        'buffer_mb': 256,
        'target_gbps': 1330.0,
        'target_burst_sec': 2.0,
        'sleep_ms': 1000,
        'timeout_ms': 30000,
    }
    STRESS_FUNC = _jump_stress_func
