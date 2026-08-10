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
import os
import logging
from logging.handlers import RotatingFileHandler
from common.const import DEFAULT_LOG_PATH

logger = logging.getLogger("ascend-npu-burn")


def setup_logger(loglevel: str):
    os.makedirs(DEFAULT_LOG_PATH, exist_ok=True)
    file_handler = RotatingFileHandler(
        os.path.join(DEFAULT_LOG_PATH, "npu_burn.log"),
        maxBytes=20 * 1024 * 1024,
        backupCount=5,
        encoding="utf-8"
    )

    # Modified log format with logger name and process ID for uniqueness
    fmt = logging.Formatter(
        fmt="%(asctime)s.%(msecs)03d [%(name)s] %(process)d %(filename)s:%(lineno)d [%(levelname)s]: %(message)s",
        datefmt="%Y-%m-%d %H:%M:%S",
    )
    file_handler.setFormatter(fmt)

    # Add check to prevent duplicate handlers if setup_logger is called multiple times
    if not logger.handlers:
        logger.addHandler(file_handler)

    logger.setLevel(loglevel.upper())
    logger.propagate = False
