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


class FusionAttentionOp(OpBase, SingleTemplate):
    def __init__(self):
        super().__init__()

    def set_sub_detect_types(self):
        self.sub_detect_types = [DetectType.SDC.value]

    def parse_cases(self):
        cases = []
        bshn_shape = self.task_config['BSHN']
        for dtype in self.dtype:
            for each in bshn_shape:
                if len(each) != 4:
                    logger.warning(f'Invalid BSHN shape for FusionAttention: {each}')
                B, S, H, N = each[0], each[1], each[2], each[3]
                cases.append(
                    {
                        "dtype": dtype,
                        "B": B,
                        "S": S,
                        "H": H,
                        "N": N
                    }
                )
        return cases

    def create_tensor_info(self, case):
        B = case["B"]
        S = case["S"]
        H = case["H"]
        N = case["N"]
        q_shape = [B, N, S, H]
        k_shape = [B, N, S, H]
        v_shape = [B, N, S, H]
        head_num = N
        input_layout = 'BNSD'
        scale = 2
        atten_mask = [B, 1, S, S]
        keep_prob = 1
        sparse_mode = 0
        gen_mask_parallel = True
        pre_tokens = 2048
        next_tokens = 0
        dtype = case["dtype"]
        tensor_info = {
            "q": TensorInfo(shape=q_shape, dtype=dtype, device=self.device),
            "k": TensorInfo(shape=k_shape, dtype=dtype, device=self.device),
            "v": TensorInfo(shape=v_shape, dtype=dtype, device=self.device),
            "atten_mask": torch.ones(atten_mask, dtype=torch.bool, device=f"npu:{self.device}"),
            "head_num": head_num,
            "input_layout": input_layout,
            "scale": scale,
            "keep_prob": keep_prob,
            "sparse_mode": sparse_mode,
            "gen_mask_parallel": gen_mask_parallel,
            "pre_tokens": pre_tokens,
            "next_tokens": next_tokens,
        }
        return tensor_info

    def run(self, tensor_mapping):
        fa_result = torch_npu.npu_fusion_attention(
            query=tensor_mapping["q"],
            key=tensor_mapping["k"],
            value=tensor_mapping["v"],
            atten_mask=tensor_mapping["atten_mask"],
            head_num=tensor_mapping["head_num"],
            input_layout=tensor_mapping["input_layout"],
            scale=tensor_mapping["scale"],
            keep_prob=tensor_mapping["keep_prob"],
            sparse_mode=tensor_mapping["sparse_mode"],
            gen_mask_parallel=tensor_mapping["gen_mask_parallel"],
            pre_tockens=tensor_mapping["pre_tokens"],
            next_tockens=tensor_mapping["next_tokens"]
        )
        return fa_result[0]
