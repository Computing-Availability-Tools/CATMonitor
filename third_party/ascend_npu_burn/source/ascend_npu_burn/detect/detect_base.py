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

class DetectBase:
    DETECT_TYPE = ""
    SUB_DETECT_TYPE = ""

    def __init__(self):
        pass

    def __init_subclass__(cls, **kwargs):
        super().__init_subclass__(**kwargs)
        DetectFactory.register_detect(f"{cls.DETECT_TYPE}_{cls.SUB_DETECT_TYPE}", cls)

    def core_detect(self, *args, **kwargs):
        pass


class DetectFactory:
    __detect_instances = {}

    @classmethod
    def register_detect(cls, detect_name, detect_class):
        if issubclass(detect_class, DetectBase):
            cls.__detect_instances[detect_name] = detect_class

    @classmethod
    def get_detect_instances(cls, detect_type, sub_detect_types=None):
        if sub_detect_types is None:
            sub_detect_types = []
        detect_instances = []
        for name, detect_class in cls.__detect_instances.items():
            if name.startswith(detect_type):
                if not sub_detect_types or detect_class.SUB_DETECT_TYPE in sub_detect_types:
                    detect_instances.append(detect_class())
        return detect_instances
