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
import logging
from benchmarks.model.model_base import ModelFactory
from benchmarks.op.op_base import OpFactory

logger = logging.getLogger("npu-burn")


class Scheduler:
    def __init__(self, args, task_name, task_config, device):
        self.args = args
        self.task_name = task_name
        self.task_config = task_config
        self.device = device

    def run(self):
        # 获取算子实例
        op_instance = OpFactory.get_op_instance(self.task_name)
        if op_instance is not None:
            op_instance.detect_type = self.args.detect
            op_instance.device = self.device
            op_instance.task_config = self.task_config
            op_instance.chip_generation = self.args.chip_generation
            return op_instance.case_run(enable_profiling=self.args.enable_profiling)

        model_instance = ModelFactory.get_model_instance(self.task_name)
        if model_instance is not None:
            model_instance.detect_type = self.args.detect
            model_instance.device = self.device
            model_instance.task_config = self.task_config
            model_instance.enable_profiling = self.args.enable_profiling
            model_instance.create_reference = bool(getattr(self.args, "create_reference", None))
            return model_instance.run()
        else:
            return "op_instance not found"
