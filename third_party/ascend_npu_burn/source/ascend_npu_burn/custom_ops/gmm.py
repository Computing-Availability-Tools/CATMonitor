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
from . import custom_ops_lib


class GMMFunction(torch.autograd.Function):
    @staticmethod
    def forward(ctx, original_weight, x, weight, bias, group_args):
        group_list, group_type, gemm_fusion, group_list_type, group_list_data_type = group_args
        bias = [] if bias is None else [bias]

        outputs = custom_ops_lib.npu_gmm_forward([x], [weight], bias, group_list, group_type, group_list_type)

        ctx.save_for_backward(x, weight)
        ctx.group_list = group_list
        ctx.group_list_type = group_list_type
        return outputs[0]

    @staticmethod
    def backward(ctx, grad_outputs):
        x, weight = ctx.saved_tensors
        group_list = ctx.group_list

        dx, dw, dbias = custom_ops_lib.npu_gmm_backward([grad_outputs], [x], [weight], group_list, ctx.group_list_type)

        dbias = None if len(dbias) == 0 else dbias[0]

        return None, dx[0], dw[0], dbias, None


def npu_gmm(x, weight, *, bias=None, group_list=None, group_type=0, gemm_fusion=False, original_weight=None):
    if gemm_fusion:
        raise ValueError("gemm_fusion=True is not supported in this migrated version.")
    if bias is not None:
        raise ValueError("bias is not supported in this migrated version.")
    if group_type != 0:
        raise ValueError("group_type must be 0 in this migrated version.")

    group_args = (group_list, group_type, gemm_fusion, 0, 0)
    return GMMFunction.apply(original_weight, x, weight, bias, group_args)
