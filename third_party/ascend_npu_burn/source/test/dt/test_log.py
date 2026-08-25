#!/usr/bin/env python3
# -*- coding: utf-8 -*-
# Copyright 2026 Huawei Technologies Co., Ltd
import pytest
import logging

from common.log import logger, setup_logger


@pytest.mark.unit
class TestLogger:

    def test_logger_name(self):
        assert logger.name == "ascend-npu-burn"

    def test_setup_logger_info(self):
        setup_logger("info")
        assert logger.level == logging.INFO
