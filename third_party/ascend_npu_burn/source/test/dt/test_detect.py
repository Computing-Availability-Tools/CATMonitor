#!/usr/bin/env python3
# -*- coding: utf-8 -*-
# Copyright 2026 Huawei Technologies Co., Ltd
import pytest
import torch

from detect.detect_base import DetectBase, DetectFactory
from detect.sdc.sdc_detect import SDCDetect


@pytest.mark.unit
class TestDetectBase:

    def test_subclass_auto_registration(self):
        class TestDetect(DetectBase):
            DETECT_TYPE = "test_type"
            SUB_DETECT_TYPE = "test_sub"

        instances = DetectFactory.get_detect_instances("test_type", ["test_sub"])
        assert len(instances) >= 1
        assert isinstance(instances[0], TestDetect)

    def test_core_detect_default(self):
        base = DetectBase()
        result = base.core_detect()
        assert result is None


@pytest.mark.unit
class TestDetectFactory:

    def test_sdc_registered(self):
        instances = DetectFactory.get_detect_instances("sdc")
        assert len(instances) > 0
        assert isinstance(instances[0], SDCDetect)

    def test_get_instances_with_sub_types(self):
        instances = DetectFactory.get_detect_instances("sdc", ["sdc"])
        assert len(instances) > 0

    def test_invalid_type_returns_empty(self):
        instances = DetectFactory.get_detect_instances("nonexistent")
        assert len(instances) == 0

    def test_instances_are_new_objects(self):
        inst1 = DetectFactory.get_detect_instances("sdc")
        inst2 = DetectFactory.get_detect_instances("sdc")
        assert inst1[0] is not inst2[0]
