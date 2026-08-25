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
import csv
import textwrap

import psutil
import torch.multiprocessing as mp
import torch_npu

from runtime.scheduler import Scheduler
from common.log import logger, setup_logger
from common.utils import EngineConfig


def subprocess_func(index, input_queues, result_queue, args, target_devices):
    setup_logger(args.log_level)
    try:
        true_device_index = target_devices[index]
        logger.info(f"[Worker PID:{os.getpid()}] Init | Bind NPU: {true_device_index}")
        torch_npu.npu.set_device(true_device_index)

        my_queue = input_queues[index]
        result_queue.put(("ready", None))
        while True:
            task = my_queue.get()
            if task is None:
                break
            try:
                scheduler = Scheduler(args, task.name, task.data, true_device_index)
                result = scheduler.run()

                if isinstance(result, dict):
                    error_records = result.get("error_records", [])
                    has_timeout = False
                    for error_record in error_records:
                        if error_record.get("has_timeout", False):
                            has_timeout = True
                            logger.error(
                                f"[Worker PID:{os.getpid()}] NPU:{true_device_index} detected timeout in task {task.name}, "
                                f"stopping further tasks on this device"
                            )
                            break

                    if has_timeout:
                        result_queue.put(({"task_seq": task.seq, "task_name": task.name, "device_id": true_device_index,
                                           "result": result}, None))
                        while True:
                            remaining_task = my_queue.get()
                            if remaining_task is None:
                                break
                            result_queue.put(({
                                                  "task_seq": remaining_task.seq,
                                                  "task_name": remaining_task.name,
                                                  "device_id": true_device_index,
                                                  "error": "Skipped due to previous timeout on this device"
                                              }, None))
                        break
                    else:
                        result_queue.put(({"task_seq": task.seq, "task_name": task.name, "device_id": true_device_index,
                                           "result": result}, None))
                else:
                    result_queue.put(({"task_seq": task.seq, "task_name": task.name, "device_id": true_device_index,
                                       "result": result}, None))
            except Exception as e:
                error_msg = f"{type(e).__name__}: {str(e)}"
                logger.error(f"[Worker PID:{os.getpid()}] Task {task.name} failed with error: {error_msg}")
                result_queue.put(({"task_seq": task.seq, "task_name": task.name, "device_id": true_device_index,
                                   "error": str(e)}, None))
    except Exception as e:
        error_msg = f"Unexpected critical error: {type(e).__name__}: {str(e)}"
        logger.critical(f"[Worker PID:{os.getpid()}] Critical error: {error_msg}")
        raise


def engine_run(spawn_index, engine_config: EngineConfig, tasks, args, result_queue=None):
    """
    引擎运行函数，负责在指定的 NUMA 节点上启动和管理 worker 进程来执行任务。

    主要功能：
    1. 设置当前进程的 CPU 亲和性，绑定到指定的 NUMA 节点
    2. 为该 NUMA 节点上的每个设备创建一个 worker 进程
    3. 将任务广播给所有 worker 进程
    4. 收集并显示执行结果
    5. 将结果发送到结果队列
    """
    setup_logger(args.log_level)

    my_numa_id = engine_config.active_numa_ids[spawn_index]
    logger.info(f"[Engine PID:{os.getpid()}] Started | NUMA ID: {my_numa_id}")

    try:
        my_cpu_affinity = engine_config.numa_configs[my_numa_id]
        p = psutil.Process()
        p.cpu_affinity(my_cpu_affinity)
    except Exception as e:
        logger.error(f"Failed to set cpu affinity for process {os.getpid()}, error msg: {e}")
        raise RuntimeError(f"Failed to set cpu affinity") from e

    if my_numa_id not in engine_config.worker_resources:
        logger.warning(f"[Engine PID:{os.getpid()}] No available resources for NUMA {my_numa_id}")
        # 如果没有可用资源，向队列发送空列表
        if result_queue:
            result_queue.put([])
        return

    input_queues, result_queue_worker = engine_config.worker_resources[my_numa_id]
    target_devices = engine_config.active_numa_map[my_numa_id]
    ori_target_device_count = len(target_devices)
    if ori_target_device_count == 0:
        logger.warning(f"[Engine PID:{os.getpid()}] No available device for NUMA {my_numa_id}")
        # 如果没有可用设备，向队列发送空列表
        if result_queue:
            result_queue.put([])
        return

    ctx = mp.get_context("spawn")
    processes = []
    for i in range(ori_target_device_count):
        p = ctx.Process(target=subprocess_func, args=(i, input_queues, result_queue_worker, args, target_devices),
                        daemon=False)
        p.start()
        processes.append(p)

    ready_cnt = 0
    while ready_cnt < ori_target_device_count:
        msg, _ = result_queue_worker.get()
        if msg == "ready":
            ready_cnt += 1

    sorted_tasks = sorted(tasks, key=lambda x: x.seq)
    logger.info(
        f"[Engine NUMA:{my_numa_id}] Boardcasting {len(sorted_tasks)} tasks to {ori_target_device_count} devices...")

    for task in sorted_tasks:
        for i in range(ori_target_device_count):
            input_queues[i].put(task)

    for i in range(ori_target_device_count):
        input_queues[i].put(None)

    total_expected_results = len(sorted_tasks) * ori_target_device_count
    finished_cnt = 0
    results_list = []
    while finished_cnt < total_expected_results:
        result_data, _ = result_queue_worker.get()
        if result_data == "ready":
            continue
        results_list.append(result_data)
        finished_cnt += 1

    for p in processes:
        p.join()

    result_display(my_numa_id, results_list)
    # 生成CSV报告，所有NUMA节点共享同一个文件
    generate_csv_report(results_list, args.output)

    # 将结果发送到结果队列
    if result_queue:
        result_queue.put(results_list)


def generate_csv_report(results_list, output_dir="output", output_file="npu_burn_results.csv",
                        error_file="npu_burn_errors.csv"):
    """
    生成CSV报告，包含每个case的执行时间、错误数量等统计信息
    将所有case配置合并到单个case_config列中
    
    Args:
        results_list: 所有任务的结果列表
        output_dir: 报告输出目录
        output_file: CSV输出文件名
        error_file: 错误报告输出文件名
    """
    try:
        os.makedirs(output_dir, exist_ok=True)

        headers = ["task", "device_id", "case_idx", "run_count", "stream_count", "exetime", "err_count", "result",
                   "case_config"]
        output_path = os.path.join(output_dir, output_file)

        # 检查文件是否存在，决定是否写入表头
        file_exists = os.path.isfile(output_path)
        with open(output_path, mode='a', newline='', encoding='utf-8') as csv_file:
            writer = csv.DictWriter(csv_file, fieldnames=headers)
            if not file_exists:
                writer.writeheader()

            for res in results_list:
                if "error" in res:
                    continue
                task_result = res.get("result", {})
                device_id = res.get("device_id", "N/A")

                # 处理case_statistics
                case_statistics = task_result.get("case_statistics", [])
                for case_stat in case_statistics:
                    if "case" in case_stat and "run_count" in case_stat:
                        case_info = case_stat["case"]
                        case_config_str = ", ".join([f"{k}={v}" for k, v in case_info.items()])
                        row = {
                            "task": case_stat.get("task", "Unknown"),
                            "device_id": device_id,
                            "case_idx": case_stat.get("case_idx", -1),
                            "run_count": case_stat.get("run_count", 0),
                            "stream_count": case_stat.get("stream_count", 1),
                            "exetime": case_stat.get("exetime", 0.0),
                            "err_count": case_stat.get("err_count", 0),
                            "result": "PASS" if case_stat.get("result", False) else "FAIL",
                            "case_config": case_config_str
                        }
                        writer.writerow(row)

        logger.info(f"✅ CSV report has been generated: {output_path}")

        # 生成错误记录报告
        generate_error_report(results_list, output_dir, error_file)

    except Exception as e:
        logger.error(f"❌ Failed to generate CSV report: {str(e)}")


def generate_error_report(results_list, output_dir="output", output_file="npu_burn_errors.csv"):
    """
    生成错误记录报告，包含每个错误的详细信息
    
    Args:
        results_list: 所有任务的结果列表
        output_dir: 报告输出目录
        output_file: 错误报告输出文件名
    """
    try:
        os.makedirs(output_dir, exist_ok=True)

        # 定义错误记录的列头，兼容不同格式的错误记录
        headers = ["task", "device_id", "detect_type", "step", "timestamp", "fail_num", "result","err_info"]
        output_path = os.path.join(output_dir, output_file)

        error_count = 0

        # 检查文件是否存在，决定是否写入表头
        file_exists = os.path.isfile(output_path)
        with open(output_path, mode='a', newline='', encoding='utf-8') as csv_file:
            writer = csv.DictWriter(csv_file, fieldnames=headers)
            if not file_exists:
                writer.writeheader()

            for res in results_list:
                if "error" in res:
                    continue
                task_result = res.get("result", {})
                device_id = res.get("device_id", "N/A")

                # 处理error_records
                error_records = task_result.get("error_records", [])
                for error_record in error_records:
                    error_count += 1
                    row = {
                        "task": res.get("task_name", "Unknown"),
                        "device_id": device_id,
                        "detect_type": error_record.get("detect_type", "N/A"),
                        "step": error_record.get("step", "N/A"),
                        "timestamp": error_record.get("timestamp", "N/A"),
                        "fail_num": error_record.get("fail_num", "N/A"),
                        "result": error_record.get("result", "N/A"),
                        "err_info": error_record.get("err_info", "N/A")
                    }
                    writer.writerow(row)

        if error_count > 0:
            logger.info(f"✅ Error report has been generated: {output_path}")
        elif not file_exists:
            # 如果没有错误且文件不存在，则不创建文件
            os.remove(output_path)

    except Exception as e:
        logger.error(f"❌ Failed to generate error report: {str(e)}")


def result_display(my_numa_id, results_list):
    logger.info("")
    logger.info(f"========== [Engine NUMA:{my_numa_id} Report] ==========")
    # 定义表格列宽
    col_task = 20
    col_npu = 5
    col_status = 12
    col_details = 35
    header = f"| {'Task Name'.ljust(col_task)} | {'NPU'.ljust(col_npu)} " \
             f"| {'Status'.ljust(col_status)} | {'Error Details'.ljust(col_details)} |"
    separator = "-" * len(header)
    logger.info(separator)
    logger.info(header)
    logger.info(separator)
    for res in sorted(results_list, key=lambda x: (x.get('task_seq', 0), x.get('device_id', 0))):
        task_name = str(res.get('task_name', 'Unknown'))[:col_task].ljust(col_task)
        dev_id = str(res.get('device_id', 'N/A')).ljust(col_npu)

        if "error" in res:
            if "Skipped" in res["error"]:
                status = "⚠️ SKIP".ljust(col_status - 1)
            else:
                status = "❌ ERROR".ljust(col_status - 1)  # emoji占宽微调
            err_msg = str(res["error"]).replace('\n', ' ')
            details = (err_msg[:col_details - 3] + "..") if len(err_msg) > col_details else err_msg.ljust(col_details)
        else:
            task_result = res.get("result", {})
            # 统计错误数量
            total_err_count = 0

            # 从case_statistics统计错误
            case_statistics = task_result.get("case_statistics", [])
            for case_stat in case_statistics:
                if "err_count" in case_stat:
                    total_err_count += case_stat["err_count"]

            if total_err_count > 0:
                status = "⚠️ FAIL".ljust(col_status - 1)
                details = f"{total_err_count} error(s) detected".ljust(col_details)
            else:
                status = "✅ PASS".ljust(col_status - 1)
                details = "-".ljust(col_details)

        row = f"| {task_name} | {dev_id} | {status} | {details} |"
        logger.info(row)
    logger.info(separator)
    logger.info("")


def npu_result_display(results_list):
    if not results_list:
        print("No results to display.")
        return

    # 1. 按NPU ID分组结果
    npu_results = {}
    for res in results_list:
        device_id = res.get('device_id', 'N/A')
        if device_id not in npu_results:
            npu_results[device_id] = []
        npu_results[device_id].append(res)

    col_npu = 6
    col_status = 10
    col_error_task = 80

    # | {:<6} | {:<10} | {:<80} |
    row_format = "| {:<" + str(col_npu) + "} | {:<" + str(col_status) + "} | {:<" + str(col_error_task) + "} |"

    # 生成表头和分隔线
    header = row_format.format("NPU", "Status", "Fail tasks")
    separator = "-" * len(header)

    print("\n" + separator)
    print(header)
    print(separator)

    # 3. 排序并遍历
    sorted_ids = sorted(npu_results.keys(), key=lambda x: int(x) if str(x).isdigit() else -1)

    for device_id in sorted_ids:
        npu_res_list = npu_results[device_id]
        error_tasks = []
        total_err_count = 0

        for res in npu_res_list:
            task_name = res.get('task_name', 'Unknown')
            if "error" in res:
                if "Skipped" not in res["error"]:
                    error_tasks.append(f"{task_name} (1/1)")
                    total_err_count += 1
            else:
                task_result = res.get("result", {})
                case_stats = task_result.get("case_statistics", [])
                t_err = sum(c.get("err_count", 0) for c in case_stats)
                t_run = sum(c.get("run_count", 0) for c in case_stats)
                t_stream = max(c.get("stream_count", 1) for c in case_stats)
                if t_err > 0:
                    error_tasks.append(f"{task_name}")
                    total_err_count += t_err

        status_str = "FAIL" if total_err_count > 0 else "PASS"

        if not error_tasks:
            print(row_format.format(str(device_id), status_str, "-"))
        else:
            task_str_all = ", ".join(error_tasks)
            wrapped_tasks = textwrap.wrap(task_str_all, width=col_error_task)
            # 打印第一行
            print(row_format.format(str(device_id), status_str, wrapped_tasks[0]))
            # 打印后续折行
            for extra_line in wrapped_tasks[1:]:
                print(row_format.format("", "", extra_line))

        print(separator)
