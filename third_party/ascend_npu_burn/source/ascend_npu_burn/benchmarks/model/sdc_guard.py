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
from hashlib import sha256
from pathlib import Path

from benchmarks.model.run_config import REFERENCE_ROOT


class SilentDataCorruptionCheck:
    def _prepare_sdc_baseline(self):
        self._sdc_baseline_path = None
        self._sdc_baseline_records = {}
        self._sdc_baseline_mode = str(self._current_value("sdc_baseline_mode", "compare")).lower()
        self._sdc_baseline_writer = False
        self._sdc_baseline_checker = False
        self._sdc_baseline_saved_count = 0
        self._sdc_baseline_checked_count = 0

        if self._sdc_baseline_mode in ("off", "none", "local"):
            return True

        baseline_root = Path(str(self._current_value("sdc_baseline_dir", REFERENCE_ROOT)))
        self._sdc_baseline_path = baseline_root / self.MODEL_KEY / f"{self._sdc_case_digest()}.json"
        self._sdc_baseline_path.parent.mkdir(parents=True, exist_ok=True)

        baseline_exists = self._sdc_baseline_path.exists()
        if self._sdc_baseline_mode == "save":
            self._sdc_baseline_writer = True
            if baseline_exists:
                self._sdc_baseline_path.unlink()
            return True

        if self._sdc_baseline_mode == "compare":
            if not baseline_exists:
                self._sdc_error_count += 1
                self._append_error(f"sdc baseline not found: {self._sdc_baseline_path}", 0)
                return False
            self._sdc_baseline_checker = self._load_sdc_baseline()
            return self._sdc_baseline_checker

        self._sdc_error_count += 1
        self._append_error(f"unsupported sdc baseline mode: {self._sdc_baseline_mode}", 0)
        return False

    def _sdc_case_digest(self):
        case_view = {}
        for key, value in self._case.items():
            if key.startswith("sdc_"):
                continue
            case_view[key] = value
        serialized_case = json.dumps(case_view, sort_keys=True, separators=(",", ":"))
        return sha256(serialized_case.encode("utf-8")).hexdigest()[:16]

    def _load_sdc_baseline(self):
        loaded_records = {}
        with self._sdc_baseline_path.open("r", encoding="utf-8") as baseline_file:
            baseline_text = baseline_file.read().strip()
        if baseline_text:
            baseline_records = json.loads(baseline_text)
            for step_key, record_data in baseline_records.items():
                step_number = self._pick_int(str(step_key).replace("step_", ""), -1)
                loaded_records[step_number] = record_data
        if not loaded_records:
            self._sdc_error_count += 1
            self._append_error(f"sdc baseline is empty: {self._sdc_baseline_path}", 0)
            return False
        self._sdc_baseline_records = loaded_records
        return True

    def _sync_sdc_baseline(self, payload):
        if self._sdc_baseline_path is None:
            return True
        payload["sdc_compare_base"] = "reference"
        if self._sdc_baseline_writer:
            return self._save_sdc_baseline_step(payload)
        if self._sdc_baseline_checker:
            return self._compare_sdc_baseline_step(payload)
        payload["sdc_status"] = "pass"
        return True

    def _save_sdc_baseline_step(self, payload):
        self._write_step_metrics(
            self._sdc_baseline_path,
            payload.get("step"),
            payload.get("loss"),
            payload.get("grad_norm"),
            payload.get("checksum"),
        )
        payload["sdc_baseline_mode"] = "save"
        payload["sdc_baseline_path"] = str(self._sdc_baseline_path)
        payload["sdc_status"] = "pass"
        self._sdc_baseline_saved_count += 1
        return True

    def _compare_sdc_baseline_step(self, payload):
        baseline_payload = self._sdc_baseline_records.get(payload.get("step"))
        payload["sdc_baseline_mode"] = "compare"
        payload["sdc_baseline_path"] = str(self._sdc_baseline_path)
        if baseline_payload is None:
            return self._mark_sdc_failure(payload, f"baseline step {payload.get('step')} not found")

        reasons = self._build_sdc_baseline_reasons(payload, baseline_payload)
        payload["sdc_baseline_loss"] = baseline_payload.get("loss")
        payload["sdc_baseline_grad_norm"] = baseline_payload.get("grad_norm")
        payload["sdc_baseline_checksum"] = baseline_payload.get("checksum")
        self._sdc_baseline_checked_count += 1

        if reasons:
            return self._mark_sdc_failure(payload, "; ".join(reasons))
        payload["sdc_status"] = "pass"
        return True

    def _build_sdc_baseline_reasons(self, payload, baseline_payload):
        compare_plan = (
            ("loss", "sdc_baseline_loss_tol"),
            ("grad_norm", "sdc_baseline_grad_tol"),
            ("checksum", "sdc_baseline_checksum_tol"),
        )
        reasons = []
        for value_key, limit_key in compare_plan:
            value_now = self._pick_float(payload.get(value_key), 0.0)
            value_ref = self._pick_float(baseline_payload.get(value_key), 0.0)
            value_delta = round(value_now - value_ref, 8)
            payload[f"sdc_baseline_{value_key}_delta"] = value_delta
            limit_value = self._pick_float(self._current_value(limit_key, -1.0), -1.0)
            if limit_value >= 0 and abs(value_delta) > limit_value:
                reasons.append(f"{value_key}_baseline_delta={value_delta} exceeds {limit_value}")
        return reasons

    def _mark_sdc_failure(self, payload, reason):
        payload["sdc_status"] = "fail"
        reasons = payload.get("sdc_reasons", [])
        if not isinstance(reasons, list):
            reasons = [str(reasons)]
        reasons.append(reason)
        payload["sdc_reasons"] = reasons
        self._sdc_error_count += 1
        self._log_model_error(
            "%s SDC check failed at step %s on %s: %s",
            self.MODEL_KEY,
            payload.get("step", 0),
            self._run_device_name,
            reason,
        )
        self._append_error(reason, payload.get("step", 0))
        return False
