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
from pathlib import Path

MODEL_PACKAGE_ROOT = Path(__file__).resolve().parent
REFERENCE_ROOT = MODEL_PACKAGE_ROOT / "reference"
TRACE_ROOT = MODEL_PACKAGE_ROOT / "model_benchmark" / "results"

CONFIG_INT_FIELDS = (
    "run_count",
    "batch_size",
    "seq_len",
    "num_classes",
    "vocab_size",
    "hidden_size",
    "num_hidden_layers",
    "num_attention_heads",
    "intermediate_size",
)
DEFAULT_INT_FIELDS = {
    "num_warmup": 5,
    "random_seed": 1,
    "sample_pool": 4,
}
INTERNAL_FIELDS = {
    "sdc_log_dir",
    "sdc_baseline_dir",
    "sdc_baseline_mode",
    "sdc_baseline_loss_tol",
    "sdc_baseline_grad_tol",
    "sdc_baseline_checksum_tol",
}


class CaseConfiguration:
    def _build_run_case(self):
        run_case = {}
        for field_name in CONFIG_INT_FIELDS:
            run_case[field_name] = self._read_config_int(field_name)
        for field_name, default_value in DEFAULT_INT_FIELDS.items():
            run_case[field_name] = self._read_optional_int(field_name, default_value)

        run_case["learning_rate"] = self._read_config_float("learning_rate")
        run_case["dtype"] = self._read_config_dtype()
        run_case["num_steps"] = run_case["run_count"]

        run_case["sdc_log_dir"] = str(TRACE_ROOT / "step_metrics")
        run_case["sdc_baseline_dir"] = str(REFERENCE_ROOT)
        run_case["sdc_baseline_mode"] = "save" if self.create_reference else "compare"
        run_case["sdc_baseline_loss_tol"] = 0.01
        run_case["sdc_baseline_grad_tol"] = 0.1
        run_case["sdc_baseline_checksum_tol"] = 0.1
        return run_case

    def _read_config_int(self, key):
        value = self._read_config_value(key)
        return int(value)

    def _read_optional_int(self, key, default):
        value = self.task_config.get(key, default)
        return int(value)

    def _read_config_float(self, key):
        value = self._read_config_value(key)
        return float(value)

    def _read_config_dtype(self):
        return str(self._read_config_value("dtype")).lower()

    def _read_config_value(self, key):
        if key not in self.task_config:
            raise ValueError(f"{self.MODEL_KEY} config missing required parameter: {key}")
        return self.task_config[key]

    def _public_case(self, case_data):
        return {key: value for key, value in case_data.items() if key not in INTERNAL_FIELDS}

    def _current_value(self, key, default=None):
        return self._case.get(key, default)

    def _pick_int(self, value, default):
        if value is None:
            return default
        return int(value)

    def _pick_float(self, value, default):
        if value is None:
            return default
        return float(value)

    def _required_case_int(self, key):
        if key not in self._case:
            raise ValueError(f"{self.MODEL_KEY} case missing required parameter: {key}")
        return int(self._case[key])
