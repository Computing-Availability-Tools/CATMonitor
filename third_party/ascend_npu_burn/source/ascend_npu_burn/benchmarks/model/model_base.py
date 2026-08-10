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
import time
import torch

from benchmarks.model.result_reporter import ResultReporting
from benchmarks.model.run_config import CaseConfiguration
from benchmarks.model.runtime_env import TrainingRuntime
from benchmarks.model.sdc_guard import SilentDataCorruptionCheck
from benchmarks.model.trace_writer import StepTraceWriting

logger = logging.getLogger("ascend-npu-burn")


def build_training_head(torch_module, decoder, hidden_size, class_count):
    class TrainingHead(torch_module.nn.Module):
        def __init__(self, decoder_layer, head_width, output_size):
            super().__init__()
            self.decoder = decoder_layer
            self.classifier = torch_module.nn.Linear(head_width, output_size)

        def forward(self, token_ids):
            hidden_states = self.decoder(token_ids).last_hidden_state
            return self.classifier(hidden_states)

    return TrainingHead(decoder, hidden_size, class_count)


class ModelBase(
    CaseConfiguration,
    TrainingRuntime,
    SilentDataCorruptionCheck,
    StepTraceWriting,
    ResultReporting,
):
    MODEL_KEY = ""

    def __init__(self):
        self.device = None
        self.task_config = None
        self.create_reference = False

    def __init_subclass__(cls, **kwargs):
        super().__init_subclass__(**kwargs)
        ModelFactory.register_model(cls.__name__.lower(), cls)

    def run(self):
        self._result = self._fresh_result()
        self._case = {}
        self._preprocess()
        self._benchmark()
        return self._result

    def _preprocess(self):
        self._case = self._build_run_case()

    def _benchmark(self):
        begin = time.perf_counter()
        self._prepare_case_outputs()
        metrics = self._train()
        self._append_case_result(True, time.perf_counter() - begin, metrics)

    def _train(self):
        self._create_model()
        self._create_optimizer()
        return self._train_step()

    def _create_model(self):
        raise NotImplementedError

    def _create_optimizer(self):
        learning_rate = self._pick_float(self._current_value("learning_rate", 1e-5), 1e-5)
        self._optimizer = torch.optim.AdamW(
            self._network.parameters(),
            lr=learning_rate,
            betas=(0.9, 0.95),
            eps=1e-8,
        )

    def _train_step(self):
        warmup_steps = max(0, self._pick_int(self._current_value("num_warmup", 5), 5))
        measure_steps = max(1, self._pick_int(self._current_value("num_steps", 1), 1))
        total_steps = warmup_steps + measure_steps
        elapsed_history = []
        grad_norm_history = []
        loss_history = []
        checksum_history = []

        for step_number in range(1, total_steps + 1):
            step_result = self._run_training_step(torch, step_number)
            self._record_step(
                step_number,
                "measure" if step_number > warmup_steps else "warmup",
                step_result["elapsed_ms"],
                step_result["loss"],
                step_result["grad_norm"],
                step_result["checksum"],
            )

            if step_number > warmup_steps:
                elapsed_history.append(step_result["elapsed_ms"])
                grad_norm_history.append(step_result["grad_norm"])
                loss_history.append(self._extract_number(step_result["loss"]))
                checksum_history.append(step_result["checksum"])

        return self._build_metrics(
            elapsed_history,
            grad_norm_history,
            loss_history,
            checksum_history,
        )

    def _run_training_step(self, torch_module, step_number):
        token_ids, targets = self._take_sample(step_number - 1)
        runtime_tokens = self._move_to_runtime(torch_module, token_ids)
        runtime_targets = self._move_to_runtime(torch_module, targets)

        start_time = self._runtime_time(torch_module)
        self._optimizer.zero_grad(set_to_none=True)
        class_scores = self._network(runtime_tokens)
        loss_value = self._criterion(class_scores[:, -1, :], runtime_targets)
        loss_value.backward()
        grad_norm = self._read_grad_norm()
        self._optimizer.step()
        end_time = self._runtime_time(torch_module)
        checksum = self._read_checksum()

        return {
            "elapsed_ms": (end_time - start_time) * 1000.0,
            "loss": loss_value,
            "grad_norm": grad_norm,
            "checksum": checksum,
        }

class ModelFactory:
    _model_instances = {}

    @classmethod
    def register_model(cls, model_name, model_class):
        if issubclass(model_class, ModelBase):
            cls._model_instances[model_name] = model_class

    @classmethod
    def get_model_instance(cls, model_name):
        model_class = cls._model_instances.get(f"{str(model_name).replace('_', '').lower()}model")
        if model_class is not None:
            return model_class()

        logger.warning("Model %s not registered.", model_name)
        return None
