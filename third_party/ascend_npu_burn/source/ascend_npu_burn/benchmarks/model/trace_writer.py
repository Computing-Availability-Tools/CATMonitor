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
import json
from datetime import datetime, timezone
from pathlib import Path

from benchmarks.model.run_config import TRACE_ROOT


class StepTraceWriting:
    def _prepare_case_outputs(self):
        stamp = datetime.now(timezone.utc).strftime("%Y%m%dT%H%M%S%fZ")
        case_folder = f"device_{self._pick_int(self.device, 0)}"

        step_root = Path(str(self._current_value("sdc_log_dir", TRACE_ROOT / "step_metrics")))

        self._step_log_path = step_root / self.MODEL_KEY / case_folder / f"{stamp}.json"
        self._step_log_path.parent.mkdir(parents=True, exist_ok=True)
        self._recorded_step_count = 0
        self._sdc_error_count = 0
        self._prepare_sdc_baseline()
        self._log_case_output_plan()

    def _record_step(
        self,
        step_number,
        phase_name,
        elapsed_ms,
        loss_value,
        grad_norm_value,
        checksum_value,
    ):
        loss_scalar = self._extract_number(loss_value)
        grad_scalar = self._extract_number(grad_norm_value)
        checksum_scalar = self._extract_number(checksum_value)

        self._ensure_finite("loss", loss_scalar, step_number)
        self._ensure_finite("grad_norm", grad_scalar, step_number)
        self._ensure_finite("checksum", checksum_scalar, step_number)

        payload = {
            "task": self.MODEL_KEY,
            "device": self._run_device_name,
            "detect_type": "sdc",
            "phase": phase_name,
            "step": step_number,
            "precision": self._current_value("dtype"),
            "checksum_mode": "parameter_signature_v2",
            "loss": round(loss_scalar, 8),
            "grad_norm": round(grad_scalar, 8),
            "checksum": round(checksum_scalar, 8),
            "step_time_ms": round(float(elapsed_ms), 8),
            "timestamp": self._timestamp(),
        }

        baseline_ok = self._sync_sdc_baseline(payload)
        if not baseline_ok:
            payload["result"] = False
        else:
            payload["result"] = True

        self._write_step_metrics(
            self._step_log_path,
            step_number,
            payload["loss"],
            payload["grad_norm"],
            payload["checksum"],
        )
        self._recorded_step_count += 1

    def _write_step_metrics(self, file_path, step_number, loss_value, grad_norm_value, checksum_value):
        saved_payload = {}
        if file_path.exists():
            with file_path.open("r", encoding="utf-8") as step_log:
                existing_text = step_log.read().strip()
            if existing_text:
                saved_payload = json.loads(existing_text)

        saved_payload[f"step_{step_number}"] = {
            "loss": loss_value,
            "grad_norm": grad_norm_value,
            "checksum": checksum_value,
        }

        with file_path.open("w", encoding="utf-8") as step_log:
            json.dump(saved_payload, step_log, ensure_ascii=False, indent=2, sort_keys=True)
