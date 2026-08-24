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
from enum import Enum


class TemplateType(Enum):
    SINGLE = "single"
    GROUP = "group"


class DetectType(Enum):
    SDC = "sdc"


class TensorType(Enum):
    INPUT = "input"
    OUTPUT = "output"


class DtypeStr(Enum):
    FLOAT64 = "float64"
    FLOAT32 = "float32"
    FLOAT16 = "float16"
    FLOAT = "float"
    BFLOAT16 = "bfloat16"
    DOUBLE = "double"
    INT64 = "int64"
    INT32 = "int32"
    INT16 = "int16"
    INT8 = "int8"
    UINT64 = "uint64"
    UINT32 = "uint32"
    UINT16 = "uint16"
    UINT8 = "uint8"
    BOOL = "bool"
    COMPLEX64 = "complex64"
    FLOAT4_E2M1FN_X2 = "mxfloat4"
    FLOAT8_E4M3FN = "float8"
    HIFLOAT8 = "hifloat8"


class ChipGeneration(Enum):
    A5 = "A5"
    A3 = "A3"
