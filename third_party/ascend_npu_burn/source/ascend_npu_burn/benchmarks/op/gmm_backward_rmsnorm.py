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
import threading
import torch
import torch_npu
from benchmarks.op.op_base import OpBase
from benchmarks.op.template.single import SingleTemplate
from common.log import logger
from common.tensor_info import TensorInfo
from common.enum import DetectType
from ascend_npu_burn.custom_ops import npu_gmm


class RMSNormStressor:
    def __init__(self, device, task_config, timeout=30):
        self.x = None
        self.w = None
        self.device = device
        self.task_config = task_config
        self.timeout = timeout

        self.bg_stream = torch.npu.Stream(device=self.device)
        self.stop_event = threading.Event()
        self._create_tensor()
        self.thread = threading.Thread(target=self._run_loop, daemon=True)

    def _create_tensor(self):
        shape = self.task_config.get("ND", [512, 4096])
        if len(shape) != 2:
            shape = [512, 4096]
        x_shape = shape
        w_shape = [shape[1]]
        self.x = torch.randn(x_shape, dtype=torch.float16, device=f"npu:{self.device}")
        self.w = torch.randn(w_shape, dtype=torch.float16, device=f"npu:{self.device}")

    def _run_loop(self):
        logger.info(f"[{self.device}] Background RMSNorm loop initiated")
        run_count = self.task_config.get("run_count", 100)
        while not self.stop_event.is_set():
            with torch.npu.stream(self.bg_stream):
                for _ in range(run_count):
                    torch_npu.npu_rms_norm(self.x, self.w)
                self.bg_stream.synchronize()
                default_stream = torch.npu.default_stream(device=self.device)
                if not default_stream.query():
                    continue

        logger.info(f"[{self.device}] Background RMSNorm loop terminated.")

    def start(self):
        self.thread.start()

    def stop(self):
        self.stop_event.set()
        self.thread.join(self.timeout)


class GMMBackwardRMSNormOp(OpBase, SingleTemplate):
    def __init__(self):
        super().__init__()
        self.sub_detect_types = [DetectType.SDC.value]

    def run(self, tensor_mapping):
        x = tensor_mapping["x"].clone().detach().requires_grad_(True)
        w = tensor_mapping["w"].clone().detach().requires_grad_(True)
        y = tensor_mapping["y"].clone()
        group_list = [(x.shape[0] // w.shape[0]) * (i + 1) for i in range(w.shape[0])]
        result = npu_gmm(x, w, group_list=group_list)
        result.backward(y)
        return w.grad

    def case_run(self, enable_profiling: bool = False):
        bg_stressor = RMSNormStressor(device=self.device, task_config=self.task_config)
        results = {}
        try:
            logger.info("Primary gmm case executed...")
            results = super().case_run(enable_profiling)
            bg_stressor.start()
        finally:
            bg_stressor.stop()
            logger.info("Background rms_norm thread stopped.")
        logger.info("Primary gmm case executed successfully.")
        return results

    def parse_cases(self):
        cases = []
        dtypes = self.task_config.get("dtype", [])
        patterns = self.task_config.get("pattern", [""])
        shape = self.task_config.get("ESKN", [])
        if not shape or len(shape) != 4:
            return cases
        for dtype in dtypes:
            for pattern in patterns:
                cases.append({"dtype": dtype, "pattern": pattern, "shape": shape})
        return cases

    def create_tensor_info(self, case):
        dtype = case.get("dtype", "")
        pattern = case.get("pattern", "")
        shape = case.get("shape", [])

        e = shape[0]
        s = shape[1]
        k = shape[2]
        n = shape[3]

        x_shape = [s, k]
        w_shape = [e, k, n]
        y_shape = [s, n]

        tensor_info = {
            "x": TensorInfo(shape=x_shape, dtype=dtype, device=self.device, pattern=pattern),
            "w": TensorInfo(shape=w_shape, dtype=dtype, device=self.device, pattern=pattern),
            "y": TensorInfo(
                shape=y_shape,
                dtype=dtype,
                device=self.device,
                creator=lambda p_shape, p_dtype, p_device: torch.ones(
                    p_shape[0], p_shape[1], dtype=p_dtype, device=p_device
                ),
            ),
        }
        return tensor_info
