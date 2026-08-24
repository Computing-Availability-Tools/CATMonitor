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
from benchmarks.op.op_base import OpBase
from benchmarks.op.strategy.chip_strategy import ChipGenerationStrategy
from benchmarks.op.template.single import SingleTemplate
from common.enum import DetectType


class QuantMatmulOp(OpBase, SingleTemplate):
    def __init__(self):
        super().__init__()
        self.strategy = None
        self.sub_detect_types = [DetectType.SDC.value]

    def get_strategy(self):
        if self.strategy is None:
            self.strategy = ChipGenerationStrategy.get_strategy(self.__class__.__name__, self.chip_generation)
        return self.strategy

    def run(self, tensor_mapping):
        strategy = self.get_strategy()
        if strategy is None:
            return None
        return self.strategy.run(tensor_mapping)

    def parse_cases(self):
        strategy = self.get_strategy()
        if strategy is None:
            return []
        return self.strategy.parse_cases(self.task_config)

    def create_tensor_info(self, case):
        strategy = self.get_strategy()
        if strategy is None:
            return {}
        return self.strategy.create_tensor_info(case, self.device)
