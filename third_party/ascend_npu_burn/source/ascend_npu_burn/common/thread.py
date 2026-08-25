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
import datetime
import os
import threading

import torch

from common.const import NUM_TEN, TIMEOUT
from common.log import logger
from detect.sdc.sdc_detect import SDCDetect


class OpThread(threading.Thread):
    def __init__(self, check_stream, stream, run_count, device, op_func, params):
        super().__init__()
        self.check_stream = check_stream
        self.stream = stream
        self.run_count = run_count
        self.device = device
        self.op_func = op_func
        self.params = params

        self.detect_instance = SDCDetect()
        self.result = []

    def run(self):
        torch.npu.set_device(f"npu:{self.device}")
        op_name = self.op_func.__class__.__name__
        with (torch.npu.stream(self.stream)):
            golden_output = self.op_func(*self.params)
            if stream_sync_timeout(torch.npu.current_stream(), TIMEOUT):
                err_info = f"Stream synchronization timed out after {TIMEOUT} seconds"
                self._make_record(op_name, "golden", err_info, has_timeout=True)
                return

            time_list = []
            cur_output_list = [None] * NUM_TEN
            check_events = [torch.npu.Event() for _ in range(NUM_TEN)]
            res_tensor = torch.zeros(self.run_count, dtype=torch.bool, device=f"npu:{self.device}")

            for event in check_events:
                event.record(self.stream)

            for i in range(self.run_count):
                check_events[i % NUM_TEN].synchronize()
                cur_output_list[i % NUM_TEN] = self.op_func(*self.params)
                if stream_sync_timeout(torch.npu.current_stream(), TIMEOUT):
                    err_info = f"Stream synchronization timed out after {TIMEOUT} seconds"
                    self._make_record(op_name, i, err_info, has_timeout=True)
                    return

                forward_event = torch.npu.Event()
                self.stream.record_event(forward_event)
                time_list.append(datetime.datetime.now(tz=datetime.timezone.utc).strftime("%Y-%m-%d %H:%M:%S.%f"))

                with torch.npu.stream(self.check_stream):
                    self.check_stream.wait_event(forward_event)
                    res_tensor[i] = self.detect_instance.core_detect(cur_output_list[i % NUM_TEN], golden_output)
                    self.check_stream.record_event(check_events[i % NUM_TEN])
            torch.npu.synchronize()

            if torch.any(res_tensor):
                err_steps = torch.nonzero(res_tensor).flatten().tolist()
                err_count = len(err_steps)
                sample_steps = err_steps[:3]  # 只记录前3个步数，避免日志过长
                logger.warning(
                    f"[{op_name}] NPU:{self.device} Failed! "
                    f"Error count: {err_count}/{self.run_count}. First few err steps: {sample_steps}")

                for non_zero_index in err_steps:
                    self.result.append(
                        {
                            "op": op_name,
                            "detect_type": self.detect_instance.SUB_DETECT_TYPE,
                            "step": non_zero_index,
                            "timestamp": time_list[non_zero_index],
                            "result": False
                        }
                    )

    def _make_record(self, op_name, step, error=None, has_timeout=False):
        self.result.append({
            "op": op_name,
            "detect_type": self.detect_instance.SUB_DETECT_TYPE,
            "step": step,
            "timestamp": datetime.datetime.now(tz=datetime.timezone.utc).strftime("%Y-%m-%d %H:%M:%S.%f"),
            "result": error is None,
            **({"err_info": str(error)} if error else {}),
            "has_timeout": has_timeout
        })


def stream_sync(stream):
    stream.synchronize()


def stream_sync_timeout(stream, timeout=300):
    os.environ["ACL_DEVICE_SYNC_TIMEOUT"] = str(timeout)
    thread = threading.Thread(
        target=stream_sync,
        args=(stream,)
    )
    thread.daemon = True
    thread.start()
    thread.join(timeout=timeout)
    if thread.is_alive():
        logger.error(f"Stream synchronization timed out after {timeout} seconds")
        return True
    return False
