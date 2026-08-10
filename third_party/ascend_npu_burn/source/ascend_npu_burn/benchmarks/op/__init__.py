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
from benchmarks.op.matmul import MatmulOp  # noqa: F401
from benchmarks.op.matmul_atomic import MatmulAtomicOp  # noqa: F401
from benchmarks.op.conv2d import Conv2dOp  # noqa: F401
from benchmarks.op.conv3d import Conv3dOp  # noqa: F401
from benchmarks.op.quant_matmul import QuantMatmulOp  # noqa: F401
from benchmarks.op.permute import PermuteOp  # noqa: F401
from benchmarks.op.linear_softmax import LinearSoftmaxOp  # noqa: F401
from benchmarks.op.fusion_attention import FusionAttentionOp  # noqa: F401
from benchmarks.op.fusion_attention_grad import FusionAttentionGradOp  # noqa: F401
from benchmarks.op.simplified_transformer import SimplifiedTransformerOp  # noqa: F401
from benchmarks.op.matmul_axpy import MatmulAxpyOp  # noqa: F401
from benchmarks.op.matmul_sdma import MatmulSDMAOp  # noqa: F401
from benchmarks.op.matmul_sdma_jump import MatmulSDMAJumpOp  # noqa: F401
from benchmarks.op.matmul_pow import MatmulPowOp  # noqa: F401
from benchmarks.op.matmul_add_fp32 import MatmulAddFp32Op  # noqa: F401
from benchmarks.op.weight_quant_batchmatmul import WeightQuantBatchMatmulOp  # noqa: F401
from benchmarks.op.cube_vector_sdma import CubeVectorSdmaOp  # noqa: F401
from benchmarks.op.gmm_backward_rmsnorm import GMMBackwardRMSNormOp  # noqa: F401
