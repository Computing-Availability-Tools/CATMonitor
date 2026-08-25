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
import pathlib
import re
import torch
import torch_npu
from common.enum import DtypeStr

# 默认版本号
DEFAULT_VERSION = "26.1.0"

# 字符串常量
ROOT_PATH = pathlib.Path(__file__).parents[1].absolute()
OP_BACKGROUND_PATH = ROOT_PATH / "benchmarks" / "op" / "background"
DEFAULT_OUTPUT_PATH = str(pathlib.Path.home() / ".ascend_npu_burn" / "output")
DEFAULT_LOG_PATH = str(pathlib.Path.home() / ".ascend_npu_burn" / "log")
BASE_CONFIG_FILE_PATH = ROOT_PATH / "config" / "base_config.json"
A3_CONFIG_FILE_PATH = ROOT_PATH / "config" / "a3_config.json"

# 数据常量
NUM_TEN = 10
TIMEOUT = 300

# 列表
INT_TYPE_LIST = [torch.int8, torch.int16, torch.int32, torch.int64, torch.uint8, torch.bool]

# 字典
DTYPE_MAPPING = {
    DtypeStr.FLOAT64.value: torch.float64,
    DtypeStr.FLOAT32.value: torch.float32,
    DtypeStr.FLOAT16.value: torch.float16,
    DtypeStr.FLOAT.value: torch.float,
    DtypeStr.BFLOAT16.value: torch.bfloat16,
    DtypeStr.DOUBLE.value: torch.double,
    DtypeStr.INT64.value: torch.int64,
    DtypeStr.INT32.value: torch.int32,
    DtypeStr.INT16.value: torch.int16,
    DtypeStr.INT8.value: torch.int8,
    DtypeStr.UINT8.value: torch.uint8,
    DtypeStr.BOOL.value: torch.bool,
    DtypeStr.FLOAT4_E2M1FN_X2.value: torch_npu.float4_e2m1fn_x2,
    DtypeStr.FLOAT8_E4M3FN.value: torch.float8_e4m3fn,
    DtypeStr.HIFLOAT8.value: torch_npu.hifloat8,
}

# 正则表达
NUM_COMBIN_REGEX = re.compile(r"[^0-9,]")
