#!/usr/bin/env python3
# -*- coding: utf-8 -*-
# Copyright 2026 Huawei Technologies Co., Ltd
import pytest

from common.enum import TemplateType, DetectType, TensorType, DtypeStr
from common.const import DTYPE_MAPPING, INT_TYPE_LIST, TIMEOUT, NUM_TEN


@pytest.mark.unit
class TestTemplateType:

    def test_values(self):
        assert TemplateType.SINGLE.value == "single"
        assert TemplateType.GROUP.value == "group"

    def test_from_value(self):
        assert TemplateType("single") == TemplateType.SINGLE
        assert TemplateType("group") == TemplateType.GROUP
