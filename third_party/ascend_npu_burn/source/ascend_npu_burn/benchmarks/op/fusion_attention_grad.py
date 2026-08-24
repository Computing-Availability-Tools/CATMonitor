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
from benchmarks.op.template.single import SingleTemplate
from common.enum import DetectType
from common.tensor_info import TensorInfo
from common.log import logger
import numpy as np
import random
import os


class FusionAttentionGradOp(OpBase, SingleTemplate):
    def __init__(self, *args, **kwargs):
        super().__init__(*args, **kwargs)
        self.sub_detect_types = None

    @property
    def run_count(self):
        return self.task_config.get("run_count", 0)

    def set_sub_detect_types(self):
        self.sub_detect_types = [DetectType.SDC.value]

    def parse_cases(self):
        cases = []
        bshn_shape = self.task_config.get('BNSH', [])
        patterns = self.task_config.get('pattern', [""])
        for dtype in self.dtype:
            for each in bshn_shape:
                for pattern in patterns:
                    if len(each) != 4:
                        return cases
                    B, N, S, H = each[0], each[1], each[2], each[3]
                    cases.append({"dtype": dtype, "B": B, "S": S, "H": H, "N": N, "pattern": pattern})
        return cases

    def create_tensor_info(self, case):
        dtype = case["dtype"]
        pattern = case["pattern"]
        B = case["B"]
        S = case["S"]
        H = case["H"]
        N = case["N"]
        q_shape = [B, N, S, H]
        k_shape = [B, N, S, H]
        v_shape = [B, N, S, H]
        dy_shape = [B, N, S, H]
        attn_out = [B, N, S, H]
        softmax_shape = [B, N, S, 8]
        head_num = N  # 头数目
        input_layout = 'BNSD'
        scale = 2  # 缩放系数
        atten_mask = [B, 1, S, S]
        keep_prob = 1  # dropout比例，默认为1，全部保留
        sparse_mode = 0
        gen_mask_parallel = True
        pre_tokens = 2048
        next_tokens = 0
        tensor_info = {
            "q": TensorInfo(shape=q_shape, dtype=dtype, device=self.device, pattern=pattern),
            "k": TensorInfo(shape=k_shape, dtype=dtype, device=self.device, pattern=pattern),
            "v": TensorInfo(shape=v_shape, dtype=dtype, device=self.device, pattern=pattern),
            "dy": TensorInfo(shape=dy_shape, dtype=dtype, device=self.device, pattern=pattern),
            "attn_out": TensorInfo(
                shape=attn_out,
                dtype=dtype,
                device=self.device,
                pattern=pattern,
                creator=lambda p_shape, p_dtype, p_device: torch.randn(p_shape, dtype=p_dtype, device=p_device),
            ),
            "softmax_max": TensorInfo(
                shape=softmax_shape,
                dtype='float32',
                device=self.device,
                pattern=pattern,
                creator=lambda p_shape, p_dtype, p_device: torch.randn(p_shape, dtype=p_dtype, device=p_device),
            ),
            "softmax_sum": TensorInfo(
                shape=softmax_shape,
                dtype='float32',
                device=self.device,
                pattern=pattern,
                creator=lambda p_shape, p_dtype, p_device: torch.randn(p_shape, dtype=p_dtype, device=p_device),
            ),
            "atten_mask": torch.ones(atten_mask, dtype=torch.bool, device=f"npu:{self.device}"),
            "head_num": head_num,
            "input_layout": input_layout,
            "scale": scale,
            "keep_prob": keep_prob,
            "sparse_mode": sparse_mode,  # 遮挡模式，1,2,3,4,5，
            "gen_mask_parallel": gen_mask_parallel,
            "pre_tokens": pre_tokens,
            "next_tokens": next_tokens,
        }
        return tensor_info

    def run(self, tensor_mapping):
        set_deterministic(seed=0)
        fag_result = torch_npu.npu_fusion_attention_grad(
            query=tensor_mapping["q"],
            key=tensor_mapping["k"],
            value=tensor_mapping["v"],
            dy=tensor_mapping["dy"],
            head_num=tensor_mapping["head_num"],
            input_layout=tensor_mapping["input_layout"],
            pse=None,
            padding_mask=None,
            atten_mask=tensor_mapping["atten_mask"],
            softmax_max=tensor_mapping["softmax_max"],
            softmax_sum=tensor_mapping["softmax_sum"],
            attention_in=tensor_mapping["attn_out"],
            scale_value=tensor_mapping["scale"],
            pre_tockens=tensor_mapping["pre_tokens"],
            next_tockens=tensor_mapping["next_tokens"],
            sparse_mode=0,
            keep_prob=tensor_mapping["keep_prob"],
            seed=1,
            offset=1,
            numels=1,
        )
        out = fag_result[0]
        if not torch.isfinite(out).all():
            nan_num = torch.isnan(out).sum().item()
            inf_num = torch.isinf(out).sum().item()

            logger.warning(
                f"⚠️ Found non-finite values! nan={nan_num}, inf={inf_num}, shape={tuple(out.shape)}, dtype={out.dtype}"
            )

        return fag_result[0]


def set_deterministic(seed=42):
    torch.manual_seed(seed)
    torch_npu.npu.manual_seed(seed)
    torch.use_deterministic_algorithms(True)  # 强制报错非确定性操作
    np.random.seed(seed)
    random.seed(seed)
    os.environ['CUBLAS_WORKSPACE_CONFIG'] = ':4096:8'  # 确保确定性
