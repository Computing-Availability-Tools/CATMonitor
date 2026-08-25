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
import math
import statistics
import time
import warnings


class TrainingRuntime:
    def _prepare_runtime(self, torch_module):
        if str(self._current_value("dtype")).lower() != "bfloat16":
            raise ValueError(f"{self.MODEL_KEY} only supports bfloat16 precision now.")
        chosen_dtype = torch_module.bfloat16

        import torch_npu

        warnings.filterwarnings(
            "ignore",
            message=r".*Cannot create tensor with interal format while allow_internel_format=False.*",
            category=UserWarning,
        )

        device_index = self._pick_int(self.device, 0)
        torch_npu.npu.set_device(device_index)
        self._run_device = torch_module.device("npu", device_index)
        self._run_device_name = f"npu:{device_index}"

        seed_value = self._pick_int(self._current_value("random_seed", 1), 1)
        torch_module.manual_seed(seed_value)
        torch_module.use_deterministic_algorithms(True)

        return chosen_dtype

    def _move_to_runtime(self, torch_module, payload):
        return payload.to(self._run_device)

    def _runtime_time(self, torch_module):
        torch_module.npu.synchronize()
        return time.perf_counter()

    def _prime_sample_bank(self, torch_module, vocab_size, class_count):
        batch_size = self._pick_int(self._current_value("batch_size", 2), 2)
        seq_len = self._pick_int(self._current_value("seq_len", 1024), 1024)
        pool_size = max(1, self._pick_int(self._current_value("sample_pool", 4), 4))
        seed_value = self._pick_int(self._current_value("random_seed", 1), 1)

        sample_generator = torch_module.Generator()
        sample_generator.manual_seed(seed_value)

        self._sample_bank = []
        for _ in range(pool_size):
            token_ids = torch_module.randint(
                0,
                int(vocab_size),
                (batch_size, seq_len),
                dtype=torch_module.long,
                generator=sample_generator,
            )
            class_ids = torch_module.randint(
                0,
                int(class_count),
                (batch_size,),
                dtype=torch_module.long,
                generator=sample_generator,
            )
            self._sample_bank.append((token_ids, class_ids))

    def _take_sample(self, sample_index):
        return self._sample_bank[sample_index % len(self._sample_bank)]

    def _read_checksum(self):
        total_checksum = 0.0
        parameter_scale = 1.0
        for parameter in self._network.parameters():
            detached_tensor = parameter.detach().float()
            total_checksum += detached_tensor.mean().item() * parameter_scale
            total_checksum += detached_tensor.abs().sum().item() * 1e-9
            parameter_scale += 1.0
        return total_checksum

    def _read_grad_norm(self):
        total_norm = 0.0
        for parameter in self._network.parameters():
            if parameter.grad is None:
                continue
            partial_norm = parameter.grad.detach().float().norm(2).item()
            total_norm += partial_norm * partial_norm
        return total_norm ** 0.5

    def _extract_number(self, value):
        if hasattr(value, "detach"):
            return float(value.detach().float().item())
        return float(value)

    def _ensure_finite(self, name, value, step_number):
        if not math.isfinite(value):
            raise RuntimeError(f"non-finite {name} detected at step {step_number}")

    def _build_metrics(self, step_times, grad_norms, loss_values, checksum_values):
        mean_step = statistics.mean(step_times)
        metrics = {
            "precision": self._current_value("dtype"),
            "device": self._run_device_name,
            "steps": len(step_times),
            "avg_step_ms": round(mean_step, 6),
            "median_step_ms": round(statistics.median(step_times), 6),
            "throughput_sps": round(
                self._pick_int(self._current_value("batch_size", 1), 1) * 1000.0 / mean_step,
                6,
            ),
            "loss_avg": round(statistics.mean(loss_values), 6),
            "loss_max": round(max(loss_values), 6),
            "loss_last": round(loss_values[-1], 6),
            "grad_norm_last": round(grad_norms[-1], 6),
            "checksum_last": round(checksum_values[-1], 6),
        }

        if len(step_times) > 1:
            metrics["step_std_ms"] = round(statistics.pstdev(step_times), 6)
        metrics["grad_norm_avg"] = round(statistics.mean(grad_norms), 6)
        metrics["grad_norm_max"] = round(max(grad_norms), 6)
        return metrics
