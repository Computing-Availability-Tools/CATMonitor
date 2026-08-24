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
from datetime import datetime, timezone

logger = logging.getLogger("ascend-npu-burn")


class ResultReporting:
    def _fresh_result(self):
        return {"error_records": [], "case_statistics": []}

    def _append_error(self, error, step_index):
        message = str(error)
        self._result["error_records"].append(
            {
                "task": self.MODEL_KEY,
                "detect_type": "sdc",
                "step": step_index,
                "timestamp": self._timestamp(),
                "result": False,
                "err_info": message,
            }
        )

    def _append_case_result(self, passed, elapsed, metrics):
        case_view = self._public_case(self._case)
        merged_metrics = dict(metrics or {})
        effective_passed = bool(passed) and self._sdc_error_count == 0

        if self._step_log_path is not None and self._step_log_path.exists():
            merged_metrics["step_trace_path"] = str(self._step_log_path)
            merged_metrics["trace_steps"] = self._recorded_step_count
        merged_metrics["sdc_failure_count"] = self._sdc_error_count
        if self._sdc_baseline_path is not None:
            merged_metrics["sdc_baseline_path"] = str(self._sdc_baseline_path)
            merged_metrics["sdc_baseline_mode"] = self._sdc_baseline_mode
            merged_metrics["sdc_baseline_saved_steps"] = self._sdc_baseline_saved_count
            merged_metrics["sdc_baseline_checked_steps"] = self._sdc_baseline_checked_count

        self._result["case_statistics"].append(
            {
                "task": self.MODEL_KEY,
                "case_idx": 0,
                "case": case_view,
                "run_count": self._pick_int(case_view.get("run_count", 1), 1),
                "stream_count": 1,
                "exetime": round(float(elapsed), 6),
                "err_count": 0 if effective_passed else max(1, self._sdc_error_count),
                "result": effective_passed,
                "metrics": merged_metrics,
            }
        )
        self._log_case_result(effective_passed, merged_metrics)

    def _log_case_output_plan(self):
        self._log_model_info(
            "%s uses logical device %s, step trace: %s",
            self.MODEL_KEY,
            self._pick_int(self.device, 0),
            self._step_log_path,
        )
        if self._sdc_baseline_path is None:
            return

        if self._sdc_baseline_checker:
            baseline_action = "compare"
        elif self._sdc_baseline_writer:
            baseline_action = "save"
        else:
            baseline_action = self._sdc_baseline_mode
        self._log_model_info(
            "%s SDC baseline mode: %s, path: %s",
            self.MODEL_KEY,
            baseline_action,
            self._sdc_baseline_path,
        )

    def _log_case_result(self, passed, metrics):
        self._log_model_info(
            "%s finished on %s: result=%s, steps=%s, avg_step_ms=%s",
            self.MODEL_KEY,
            metrics.get("device", self._run_device_name),
            "PASS" if passed else "FAIL",
            metrics.get("steps", metrics.get("trace_steps", 0)),
            metrics.get("avg_step_ms", "N/A"),
        )
        self._log_model_info(
            "%s SDC summary: failures=%s, baseline_mode=%s, saved_steps=%s, checked_steps=%s",
            self.MODEL_KEY,
            self._sdc_error_count,
            metrics.get("sdc_baseline_mode", self._sdc_baseline_mode),
            metrics.get("sdc_baseline_saved_steps", 0),
            metrics.get("sdc_baseline_checked_steps", 0),
        )

    def _log_model_info(self, message_template, *message_args):
        logger.info(self._device_log_template(message_template), self._device_log_id(), *message_args)

    def _log_model_error(self, message_template, *message_args):
        logger.error(self._device_log_template(message_template), self._device_log_id(), *message_args)

    def _device_log_template(self, message_template):
        return "device_%s " + str(message_template)

    def _device_log_id(self):
        return self._pick_int(self.device, 0)

    def _timestamp(self):
        return datetime.now(timezone.utc).strftime("%Y-%m-%d %H:%M:%S.%f")
