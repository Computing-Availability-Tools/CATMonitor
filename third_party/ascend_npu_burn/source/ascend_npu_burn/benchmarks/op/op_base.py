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

logger = logging.getLogger("ascend-npu-burn")


class OpBase:
    def __init__(self):
        self.detect_type = ""
        self.sub_detect_types = []
        self.device = None
        self.task_config = None
        self.chip_generation = ""

    def __init_subclass__(cls, **kwargs):
        super().__init_subclass__(**kwargs)
        OpFactory.register_op(cls.__name__.lower(), cls)

    def set_sub_detect_types(self):
        pass

    def run(self, *args, **kwargs):
        pass


class OpFactory:
    __op_instances = {}

    @classmethod
    def register_op(cls, op_name, op_class):
        if issubclass(op_class, OpBase):
            cls.__op_instances[op_name] = op_class

    @classmethod
    def get_op_instance(cls, op_name):
        op_class = cls.__op_instances.get(f'{op_name.replace("_", "")}op')
        if op_class is None:
            logger.warning("Op %s not registered.", op_name)
            return None
        return op_class()
