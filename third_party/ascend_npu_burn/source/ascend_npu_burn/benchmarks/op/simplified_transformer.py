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
import random
import datetime
from datetime import timezone
import os
os.environ['PYTORCH_NPU_ALLOC_CONF'] = 'max_split_size_mb:512'

import torch
import torch_npu
import numpy as np

from benchmarks.op.op_base import OpBase
from common.enum import DetectType
from common.tensor_info import TensorInfo
from benchmarks.op.template.single import monitor_and_profile
from common.log import logger
from common.tensor_info import create_tensor


class SimplifiedTransformerOp(OpBase):
    def __init__(self):
        super().__init__()

    @property
    def run_count(self):
        return self.task_config.get("run_count", 0)

    def check_flash_attention_grad(self, case):
        tensor_info = self.create_tensor_info(case)
        tensor_mapping = create_tensor(tensor_info, self.device)

        query = tensor_mapping["query"]
        attention_mask = tensor_mapping["attention_mask"]
        attn_out = tensor_mapping["attn_out"]
        softmax_max = tensor_mapping["softmax_max"]
        softmax_sum = tensor_mapping["softmax_sum"]
        m0 = tensor_mapping["m0"]
        m1 = tensor_mapping["m1"]
        m2 = tensor_mapping["m2"]
        mc0 = tensor_mapping["mc0"]
        mat1 = tensor_mapping["mat1"]
        matc0 = tensor_mapping["matc0"]
        mat2 = tensor_mapping["mat2"]

        bsz = case["batch_size"]
        seq_length = case["seq_length"]
        head_num = case["head_num"]
        head_dim = case["head_dim"]

        # shape_order = "SBH"
        shape_order = "BNSD"
        scale = 1.0 / math.sqrt(head_dim)
        pre_tockens = 65536 * 16
        next_tockens = 0
        keep_prob = 1.0

        def single_pipe():

            m01 = torch.addmm(mc0, m0, m1, beta=1.2, alpha=1.3)
            m012 = torch.addmm(m0, m01, m2, beta=2.1, alpha=3.2)
            fag0_out = torch_npu.npu_fusion_attention_grad(
                query, query, query, m012.view(bsz, head_num, seq_length, head_dim), head_num, shape_order,
                pse=None, padding_mask=None,
                atten_mask=attention_mask,
                softmax_max=softmax_max,
                softmax_sum=softmax_sum,
                attention_in=attn_out,
                scale_value=scale,
                pre_tockens=pre_tockens,
                next_tockens=next_tockens,
                sparse_mode=0,
                keep_prob=keep_prob,
                seed=1,
                offset=1,
                numels=1)
            mat0 = fag0_out[1].view(bsz * seq_length, head_num * head_dim) + \
                   fag0_out[2].view(bsz * seq_length, head_num * head_dim) + m012

            mat01 = torch.addmm(matc0, mat0, mat1, beta=1.0, alpha=1.0)
            mat012 = torch.addmm(mat0, mat01, mat2, beta=1.0, alpha=1.0)
            fag1_out = torch_npu.npu_fusion_attention_grad(
                query, query, query, mat012.view(bsz, head_num, seq_length, head_dim), head_num, shape_order,
                pse=None, padding_mask=None,
                atten_mask=attention_mask,
                softmax_max=softmax_max,
                softmax_sum=softmax_sum,
                attention_in=attn_out,
                scale_value=scale,
                pre_tockens=pre_tockens,
                next_tockens=next_tockens,
                sparse_mode=0,
                keep_prob=keep_prob,
                seed=1,
                offset=1,
                numels=1)
            ret = fag1_out[1].view(bsz * seq_length, head_num * head_dim) + \
                  fag1_out[2].view(bsz * seq_length, head_num * head_dim) + mat012

            return ret

        golden_mat = single_pipe()

        res_tensor = torch.zeros(self.run_count, dtype=torch.bool, device=f"npu:{self.device}")
        time_list = []
        for i in range(self.run_count):
            mat_out = single_pipe()
            eq_val = is_equal_tensor(golden_mat, mat_out)
            time_list.append(datetime.datetime.now(tz=timezone.utc).strftime("%Y-%m-%d %H:%M:%S.%f"))
            if not eq_val:
                res_tensor[i] = True

        return res_tensor, time_list

    def run(self, case):

        set_deterministic(seed=0)

        torch.use_deterministic_algorithms(True)
        torch_npu.npu.set_device(self.device)

        res_tensor, time_list = self.check_flash_attention_grad(case)
        return res_tensor, time_list

    def create_tensor_info(self, case):
        dtype = case["dtype"]
        seq_length = case["seq_length"]
        batch_size = case["batch_size"]
        head_num = case["head_num"]
        head_dim = case["head_dim"]
        base_shape = [batch_size, head_num, seq_length, head_dim]
        mask_shape = [batch_size, 1, seq_length, seq_length]
        softmax_shape = [batch_size, head_num, seq_length, 8]
        L1, L2, L3 = batch_size * seq_length, head_num * head_dim, 128

        tensor_info = {
            "query": TensorInfo(shape=base_shape, dtype=dtype, device=self.device,
                                creator=lambda p_shape, p_dtype, p_device:
                                torch.randn(p_shape, dtype=p_dtype, device=p_device)),
            "attention_mask": TensorInfo(shape=mask_shape, dtype="bool", device=self.device,
                                         creator=lambda p_shape, p_dtype, p_device:
                                         torch.zeros(p_shape, dtype=p_dtype, device=p_device)),
            "attn_out": TensorInfo(shape=base_shape, dtype=dtype, device=self.device,
                                   creator=lambda p_shape, p_dtype, p_device:
                                   torch.randn(p_shape, dtype=p_dtype, device=p_device)),
            "softmax_max": TensorInfo(shape=softmax_shape, dtype=dtype, device=self.device,
                                      creator=lambda p_shape, p_dtype, p_device:
                                      torch.randn(p_shape, dtype=p_dtype, device=p_device)),
            "softmax_sum": TensorInfo(shape=softmax_shape, dtype=dtype, device=self.device,
                                      creator=lambda p_shape, p_dtype, p_device:
                                      torch.randn(p_shape, dtype=p_dtype, device=p_device)),
            "m0": TensorInfo(shape=[L1, L2], dtype=dtype, device=self.device, creator=lambda p_shape, p_dtype, p_device:
            torch.randn(p_shape, dtype=p_dtype, device=p_device)),
            "m1": TensorInfo(shape=[L2, L3], dtype=dtype, device=self.device, creator=lambda p_shape, p_dtype, p_device:
            torch.randn(p_shape, dtype=p_dtype, device=p_device)),
            "mc0": TensorInfo(shape=[L1, L3], dtype=dtype, device=self.device,
                              creator=lambda p_shape, p_dtype, p_device:
                              torch.randn(p_shape, dtype=p_dtype, device=p_device)),
            "m2": TensorInfo(shape=[L3, L2], dtype=dtype, device=self.device, creator=lambda p_shape, p_dtype, p_device:
            torch.randn(p_shape, dtype=p_dtype, device=p_device)),
            "mat1": TensorInfo(shape=[L2, L3], dtype=dtype, device=self.device,
                               creator=lambda p_shape, p_dtype, p_device:
                               torch.randn(p_shape, dtype=p_dtype, device=p_device)),
            "matc0": TensorInfo(shape=[L1, L3], dtype=dtype, device=self.device,
                                creator=lambda p_shape, p_dtype, p_device:
                                torch.randn(p_shape, dtype=p_dtype, device=p_device)),
            "mat2": TensorInfo(shape=[L3, L2], dtype=dtype, device=self.device,
                               creator=lambda p_shape, p_dtype, p_device:
                               torch.randn(p_shape, dtype=p_dtype, device=p_device))
        }
        return tensor_info

    def parse_cases(self):
        cases = []
        seq_lengths = self.task_config.get("seq_len", [1024])
        batch_sizes = self.task_config.get("batch_size", [16])
        run_count = self.task_config.get("run_count", 100)
        head_nums = self.task_config.get("head_num", [128])
        head_dims = self.task_config.get("head_dim", [128])
        for dtype in self.task_config.get("dtype", ["float32"]):
            for i, x in enumerate(seq_lengths):
                cases.append(
                    {
                        "dtype": dtype,
                        "seq_length": x,
                        "batch_size": batch_sizes[i],
                        "run_count": run_count,
                        "head_num": head_nums[i],
                        "head_dim": head_dims[i]
                    }
                )
        return cases

    def case_run(self, enable_profiling: bool = False):
        error_records = []
        case_statistics = []
        cases = self.parse_cases()
        op_name = self.__class__.__name__

        logger.info(f"[{op_name}] NPU:{self.device} | Start running {len(cases)} case(s)")
        for case_idx, case in enumerate(cases):
            logger.info(f"[{op_name}] NPU:{self.device} | Running case {case_idx + 1}/{len(cases)}")
            err_count = 0
            with monitor_and_profile(op_name, case_idx, self.device, enable_profiling) as metrics:
                res_tensor, time_list = self.run(case)
                if torch.any(res_tensor):
                    err_steps = torch.nonzero(res_tensor).flatten().tolist()
                    err_count += len(err_steps)
                    sample_steps = err_steps[:3]
                    logger.warning(
                        f"[{op_name}] NPU:{self.device} | ⚠️ Case {case_idx + 1} Failed! "
                        f"Error count: {len(err_steps)}/{self.run_count}. First few err steps: {sample_steps}")

                    for non_zero_index in err_steps:
                        err_record = {
                            "op": op_name,
                            "detect_type": DetectType.SDC.value,
                            "step": non_zero_index,
                            "timestamp": time_list[non_zero_index],
                            "result": False
                        }
                        error_records.append(err_record)

            case_stats = {
                "op": op_name,
                "task": op_name,
                "case_idx": case_idx,
                "case": case,
                "run_count": self.run_count,
                "err_count": err_count,
                "exetime": metrics['execution_time'],
                "result": err_count == 0
            }
            case_statistics.append(case_stats)
        return {
            "error_records": error_records,
            "case_statistics": case_statistics
        }

def is_equal_tensor(a, b):
    ret = torch.eq(a, b).all().item()
    if not ret:
        if not (torch.isfinite(a).all().item() and torch.isfinite(b).all().item()):
            ret = True
            logger.warning("⚠️ Found non-finite element in out tensor")
    return ret

def set_deterministic(seed=42):
    torch.manual_seed(seed)
    torch_npu.npu.manual_seed(seed)
    torch.use_deterministic_algorithms(True)  # 强制报错非确定性操作
    np.random.seed(seed)
    random.seed(seed)
    os.environ['CUBLAS_WORKSPACE_CONFIG'] = ':4096:8'  # 确保确定性