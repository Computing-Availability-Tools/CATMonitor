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
from typing import Optional
from benchmarks.op.strategy.base_strategy import OpChipStrategy
from benchmarks.op.strategy.quant_matmul_a3 import QuantMatmulA3Strategy
from benchmarks.op.strategy.quant_matmul_a5 import QuantMatmulA5Strategy
from common.enum import ChipGeneration
from common.log import logger


class ChipGenerationStrategy:
    _strategy_registry = {}

    @classmethod
    def register(cls, op_name, chip_generation, strategy_class):
        if op_name not in cls._strategy_registry:
            cls._strategy_registry[op_name] = {}
        if issubclass(strategy_class, OpChipStrategy):
            cls._strategy_registry[op_name][chip_generation] = strategy_class

    @classmethod
    def get_strategy(cls, op_name: str, chip_generation: str) -> Optional[object]:
        strategies = cls._strategy_registry.get(op_name, {})
        strategy_cls = strategies.get(chip_generation)
        if strategy_cls is None:
            logger.error(f"No strategy found for {op_name} on {chip_generation}.")
            return None
        return strategy_cls()


ChipGenerationStrategy.register("QuantMatmulOp", ChipGeneration.A3.value, QuantMatmulA3Strategy)
ChipGenerationStrategy.register("QuantMatmulOp", ChipGeneration.A5.value, QuantMatmulA5Strategy)
