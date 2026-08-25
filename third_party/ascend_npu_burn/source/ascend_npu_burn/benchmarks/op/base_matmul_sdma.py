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
import time
from typing import Any, Dict, Type
import torch

from benchmarks.op.op_base import OpBase
from benchmarks.op.template.single import SingleTemplate
from common.log import logger
from common.tensor_info import TensorInfo, create_tensor
from common.enum import TensorType


class SDMABackgroundStressor:
    def __init__(
        self,
        device=0,
        buffer_mb=64,
        target_gbps=1330.0,
        target_burst_sec=2.0,
        sleep_sec=1.0,
        timeout_ms=5000,
        stress_func=None,
        stress_func_kwargs=None,
        min_buffer_mb=0,
        desc_prefix="",
    ):
        self.device = device
        self.sleep_sec = sleep_sec
        self.timeout_ms = timeout_ms
        self.stop_event = threading.Event()
        self.stress_func = stress_func
        self.stress_func_kwargs = stress_func_kwargs or {}

        self.buffer_size = buffer_mb * 1024 * 1024
        copy_size = (self.buffer_size * 7) // 8
        target_bytes = target_gbps * 1e9 * target_burst_sec
        self.loop_num = int(target_bytes / (2.0 * copy_size))

        logger.info(f"[{self.device}] Allocating {buffer_mb}MB tensor{desc_prefix}.")

        self.stress_tensor = self._generate_pattern()

        if not self.stress_tensor.is_contiguous():
            logger.error("Tensor must be contiguous for direct SDMA execution.")
            raise ValueError("Tensor must be contiguous for direct SDMA execution.")

        if min_buffer_mb > 0:
            actual_size = self.stress_tensor.numel() * self.stress_tensor.element_size()
            if actual_size < min_buffer_mb * 1024 * 1024:
                logger.error(f"Tensor size must be at least {min_buffer_mb}MB, got {actual_size / 1024 / 1024}MB")
                raise ValueError(f"Tensor size must be at least {min_buffer_mb}MB for power jump test.")
        else:
            if self.stress_tensor.numel() * self.stress_tensor.element_size() != self.buffer_size:
                logger.error("Buffer capacity mismatch.")
                raise ValueError("Buffer capacity mismatch.")

        self.bg_stream = torch.npu.Stream(device=self.device)
        self.thread = threading.Thread(target=self._worker_loop)

    def _generate_pattern(self) -> torch.Tensor:
        size_bytes = self.buffer_size
        size_floats = size_bytes // 4
        tensor_info = {
            "stress": TensorInfo(shape=(size_floats,), dtype="float32", device=self.device, pattern="gauss_random")
        }
        tensor_mapping = create_tensor(tensor_info, self.device)
        stress_tensor = tensor_mapping["stress"]
        return stress_tensor.view(torch.uint8)

    def _worker_loop(self):
        raise NotImplementedError("Subclasses must implement _worker_loop")

    def start(self):
        self.thread.start()

    def stop(self):
        self.stop_event.set()
        self.thread.join()


class BurstStressor(SDMABackgroundStressor):
    def __init__(
        self,
        device=0,
        buffer_mb=64,
        target_gbps=1330.0,
        target_burst_sec=2.0,
        sleep_sec=1.0,
        timeout_ms=5000,
        stress_func=None,
        stress_func_kwargs=None,
    ):
        super().__init__(
            device=device,
            buffer_mb=buffer_mb,
            target_gbps=target_gbps,
            target_burst_sec=target_burst_sec,
            sleep_sec=sleep_sec,
            timeout_ms=timeout_ms,
            stress_func=stress_func,
            stress_func_kwargs=stress_func_kwargs,
            desc_prefix=" for SDMA burst test",
        )

    def _worker_loop(self):
        logger.info(f"[{self.device}] Async SDMA loop initiated. (Iterations per burst: {self.loop_num})")
        while not self.stop_event.is_set():
            with torch.npu.stream(self.bg_stream):
                if self.stress_func:
                    self.stress_func(self.stress_tensor, self.loop_num, **self.stress_func_kwargs)
                else:
                    raise RuntimeError("stress_func is not provided for BurstStressor")

            if not self.stop_event.is_set():
                time.sleep(self.sleep_sec)

        logger.info(f"[{self.device}] Async SDMA loop terminated.")


class JumpStressor(SDMABackgroundStressor):
    def __init__(
        self,
        device=0,
        buffer_mb=256,
        target_gbps=1330.0,
        target_burst_sec=2.0,
        sleep_ms=1000,
        timeout_ms=30000,
        stress_func=None,
        stress_func_kwargs=None,
    ):
        self.sleep_ms = sleep_ms
        super().__init__(
            device=device,
            buffer_mb=buffer_mb,
            target_gbps=target_gbps,
            target_burst_sec=target_burst_sec,
            sleep_sec=sleep_ms / 1000.0,
            timeout_ms=timeout_ms,
            stress_func=stress_func,
            stress_func_kwargs=stress_func_kwargs,
            min_buffer_mb=200,
            desc_prefix=" for SDMA jump test",
        )

    def _worker_loop(self):
        logger.info(f"[{self.device}] SDMA Jump loop initiated.")
        logger.info(
            f"[{self.device}] Buffer: {self.buffer_size / 1024 / 1024}MB, "
            f"Loops: {self.loop_num}, Sleep: {self.sleep_ms}ms"
        )

        while not self.stop_event.is_set():
            with torch.npu.stream(self.bg_stream):
                if self.stress_func:
                    self.stress_func(self.stress_tensor, self.loop_num, self.sleep_ms, **self.stress_func_kwargs)
                else:
                    raise RuntimeError("stress_func is not provided for JumpStressor")

        logger.info(f"[{self.device}] SDMA Jump loop terminated.")


class DummyStressor:
    def __init__(self, **kwargs: Any) -> None:
        pass

    def start(self) -> None:
        pass

    def stop(self) -> None:
        pass


class BaseMatmulSdma(OpBase, SingleTemplate):
    STRESSOR_CLASS: Type[Any] = DummyStressor
    STRESSOR_KWARGS: Dict[str, Any] = {}
    STRESS_FUNC = None
    STRESS_FUNC_KWARGS: Dict[str, Any] = {}

    def run(self, tensor_mapping):
        a = tensor_mapping["a"]
        b = tensor_mapping["b"]
        output = torch.matmul(a, b)
        return output

    def case_run(self, enable_profiling: bool = False):
        cls = type(self)
        stressor_class = cls.STRESSOR_CLASS
        stressor_kwargs = cls.STRESSOR_KWARGS.copy()
        stressor_kwargs.update(
            {"device": self.device, "stress_func": cls.STRESS_FUNC, "stress_func_kwargs": cls.STRESS_FUNC_KWARGS}
        )

        stressor = stressor_class(**stressor_kwargs)
        stressor.start()

        try:
            time.sleep(2)
            logger.info("\nDeploying primary compute workloads...")
            results = super().case_run(enable_profiling=enable_profiling)
            logger.info("Primary workloads executed successfully.\n")
        finally:
            logger.info("Initiating background stressor teardown...")
            stressor.stop()

        return results

    def create_tensor_info(self, case):
        shapes = case.get("shape")
        dtype = case.get("dtype")
        pattern = case.get("pattern")
        if shapes is None or len(shapes) < 2:
            raise ValueError(f"case['shape'] must contain at least 2 elements, got {shapes}")
        if dtype is None:
            raise ValueError("case['dtype'] is required")
        if pattern is None:
            raise ValueError("case['pattern'] is required")
        a_shape = shapes[0]
        b_shape = shapes[1]
        c_shape = [a_shape[0], b_shape[1]]
        tensor_info = {
            "a": TensorInfo(shape=a_shape, dtype=dtype, device=self.device, pattern=pattern),
            "b": TensorInfo(shape=b_shape, dtype=dtype, device=self.device, pattern=pattern),
            "c": TensorInfo(shape=c_shape, dtype=dtype, tensor_type=TensorType.OUTPUT.value),
        }
        return tensor_info
