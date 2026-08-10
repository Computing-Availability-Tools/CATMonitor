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
import time
import signal
import sys
import pathlib
import argparse
import traceback
import torch.multiprocessing as mp

FILE_DIR = pathlib.Path(__file__).parent.absolute()
ASCEND_NPU_BURN_ROOT = FILE_DIR

if str(FILE_DIR) not in sys.path:
    sys.path.append(str(FILE_DIR))

from common.const import NUM_COMBIN_REGEX, DEFAULT_OUTPUT_PATH, A3_CONFIG_FILE_PATH, BASE_CONFIG_FILE_PATH  # noqa: E402
from common.utils import ConfigManager, get_numa_configs, get_npu_numa_topology  # noqa: E402
from common.log import logger, setup_logger  # noqa: E402
from common.utils import EngineConfig  # noqa: E402
from common.const import DEFAULT_VERSION  # noqa: E402
from common.enum import ChipGeneration  # noqa: E402
from runtime.engine import npu_result_display  # noqa: E402
from runtime.engine import engine_run  # noqa: E402

numa_configs = get_numa_configs()
avail_numa_node = []
for i, numa_config in enumerate(numa_configs):
    avail_numa_node.append(i)
numa_npu_topo = get_npu_numa_topology()


def get_version():
    version_file = FILE_DIR / "version.info"
    if version_file.exists():
        try:
            with open(version_file, 'r', encoding='utf-8') as f:
                lines = f.readlines()
                if lines:
                    return lines[0].strip()
        except (PermissionError, UnicodeDecodeError, OSError):
            pass
    return DEFAULT_VERSION


def parse_args():
    parser = argparse.ArgumentParser()
    parser.add_argument(
        "-v",
        "--version",
        action="store_true",
        default=False,
        help="Show version information",
    )
    parser.add_argument(
        "-r",
        "--run_case",
        default=None,
        help="The task going to be evaluated, supports formats like matmul,conv2d or 1,2, refs to config/",
    )
    parser.add_argument("-g", "--group", type=str, help="Use case group defined in config file")
    parser.add_argument("-d", "--device", type=str, default="all", help="Device id, separated by comma, default is all")
    parser.add_argument("--sdc_detect", action="store_true", default=False, help="Detect type: sdc")
    parser.add_argument(
        "-c",
        "--create_reference",
        default=None,
        help="Generate SDC reference data for a model case, supports formats like gpt3 or llama2",
    )
    parser.add_argument(
        "-m",
        "--mode",
        type=str,
        choices=["concurrent", "distributed", "sequential"],
        help="Execution mode: concurrent, distributed, or sequential",
    )
    # report dir
    parser.add_argument(
        "--output", type=str, default=DEFAULT_OUTPUT_PATH, help=f"Report dir, default is {DEFAULT_OUTPUT_PATH}"
    )
    parser.add_argument(
        "--log_level", type=str, choices=["info", "debug", "warning", "error", "fatal", "critical"], default="info"
    )
    # 开启profiling
    parser.add_argument(
        "--enable_profiling", action="store_true", default=False, help="Enable performance profiling (default: False)"
    )
    # tensor dump
    parser.add_argument("--tensor_dump", action="store_true", default=False, help="Enable tensor dump (default: False)")
    # timeout阈值
    parser.add_argument("--timeout", type=int, default=300, help="Timeout threshold in seconds (default: 300)")
    # 用例整体运行次数
    parser.add_argument(
        "--exec_count", type=int, default=1, help="Number of times to execute the test case (default: 1)"
    )
    # 芯片代际
    parser.add_argument(
        "--chip_generation", type=str, choices=["A3", "A5"], default="A5", help="NPU chip generation (default: A5)"
    )

    args = parser.parse_args()
    args_check(args, parser)
    setup_logger(args.log_level)
    return args


def args_output_check(args, parser):
    # 路径检查
    if args.output == DEFAULT_OUTPUT_PATH:
        return
    output_path = pathlib.Path(args.output)
    if not output_path.is_absolute():
        parser.error(f"--output path must be an absolute path: {output_path}")
    if output_path.exists():
        parser.error(f"--output path does not exist: {args.output}")
    if not output_path.is_dir():
        parser.error(f"--output path is not a directory: {output_path}")

    resolved = output_path.resolve()
    if output_path.is_symlink() and resolved != output_path:
        parser.error(f"--output symlink target mismatch: {output_path} -> {resolved}")
    if not os.access(str(resolved), os.W_OK):
        parser.error(f"--output directory is not writable: {resolved}")


def args_check(args, parser):
    # -r 和 -g 不能同时存在
    if args.run_case is not None and args.group is not None:
        parser.error("-r/--run_case and -g/--group cannot be used simultaneously")
    # 验证设备参数
    if args.device != "all" and NUM_COMBIN_REGEX.search(args.device):
        parser.error("-d/--device should be device ids, like 0,1,2,3")
    if args.sdc_detect:
        args.detect = "sdc"
    if args.create_reference and args.detect and args.detect != "sdc":
        parser.error("-c/--create_reference only supports --sdc_detect")
    args_output_check(args, parser)


def get_active_numa_info(args):
    active_numa_map = {}  # numa_id:device_id_list
    for numa_id, node_devices in numa_npu_topo.items():
        if args.device == "all":
            if len(node_devices) > 0:
                active_numa_map[numa_id] = node_devices
        else:
            target_devs = []
            try:
                user_req_devs = [int(d) for d in args.device.split(",") if d.isdigit()]
                for d in user_req_devs:
                    if d in node_devices:
                        target_devs.append(d)
            except Exception as e:
                logger.warning(f"Failed to parse device ID: {e}")
                pass
            if len(target_devs) > 0:
                active_numa_map[numa_id] = target_devs
    active_numa_ids = sorted(active_numa_map.keys())
    active_process_cnt = len(active_numa_ids)
    return active_numa_ids, active_numa_map, active_process_cnt


def generate_run_cases(args):
    """
    根据输入的命令行参数（--run_case 或 --group）从配置文件中加载对应的测试用例，
    支持通过序号、名称指定单个/多个用例，或通过组名指定一组用例。

    Args:
        args: 解析后的命令行参数对象，需包含以下属性：
            - run_case (str): 用例指定字符串，格式可为用例序号（数字）或用例名称，多个用例用逗号分隔
            - group (str): 用例组名称，用于批量加载一组用例

    Returns:
        list: 加载成功的测试用例对象列表
    """
    run_cases = []
    cfg = ConfigManager(str(BASE_CONFIG_FILE_PATH))
    if args.chip_generation == ChipGeneration.A3.value:
        cfg = ConfigManager(str(BASE_CONFIG_FILE_PATH), str(A3_CONFIG_FILE_PATH))
    if args.create_reference and (args.run_case is not None or args.group is not None):
        logger.error("Error: -c/--create_reference cannot be used with -r/--run_case or -g/--group.")
    elif args.run_case is not None and args.group is not None:
        logger.error("Error: -r/--run_case and -g/--group cannot be used simultaneously.")
    elif args.create_reference or args.run_case:
        case_names = args.create_reference if args.create_reference else args.run_case
        case_specs = case_names.split(",")
        for spec in case_specs:
            spec = spec.strip()
            if spec.isdigit():  # 如果是数字，按序号查找
                seq_num = int(spec)
                case_obj = cfg.get_by_seq(seq_num)
                if case_obj:
                    run_cases.append(case_obj)
                else:
                    logger.warning(f"Case with sequence number {seq_num} not found")
            else:  # 否则按名称查找
                case_obj = cfg.get_by_name(spec)
                if case_obj:
                    run_cases.append(case_obj)
                else:
                    logger.warning(f"Case with name '{spec}' not found")
    elif args.group:
        group_cases = cfg.get_by_group(args.group)
        if group_cases:
            run_cases.extend(group_cases)
        else:
            logger.error(f"Group '{args.group}' not found or empty")
    else:
        logger.error("Error: -r/--run_case, -g/--group or -c/--create_reference must be assigned.")
    return run_cases


def main():
    start_time = time.time()

    # 设置临时环境变量LANG=en_US.UTF-8
    os.environ['LANG'] = 'en_US.UTF-8'

    args = parse_args()

    if args.version:
        version = get_version()
        print(f"ascend_npu_burn {version}")
        sys.exit(0)

    # 检查输出目录，删除已存在的报告文件
    os.makedirs(args.output, exist_ok=True)
    output_file = os.path.join(args.output, "npu_burn_results.csv")
    error_file = os.path.join(args.output, "npu_burn_errors.csv")
    if os.path.isfile(output_file):
        os.remove(output_file)
    if os.path.isfile(error_file):
        os.remove(error_file)

    run_cases = generate_run_cases(args)
    if len(run_cases) == 0:
        logger.error("No run case found")
        sys.exit(-1)
    active_numa_ids, active_numa_map, active_process_cnt = get_active_numa_info(args)
    if active_process_cnt == 0:
        logger.error("No available device for current process")
        sys.exit(-1)
    logger.info(f"Loaded {len(run_cases)} run case(s).")
    logger.info(f"Active NUMA Nodes: {active_numa_ids}")
    logger.info(f"Device Assignment: {active_numa_map}")

    try:
        mp.set_start_method("spawn", force=True)
    except Exception as err:
        logger.critical(f"Failed to initialize multiprocessing context: {err}", exc_info=True)
        sys.exit(-1)

    cur_process_id = os.getpid()
    subprocess_pids = []  #

    def handle_termination_signal(sig_num, _frame):
        logger.warning(f"[Main PID:{cur_process_id}] Caught signal {sig_num}. Initiating graceful shutdown...")
        if subprocess_pids:
            logger.info(f"Sending SIGTERM to child processes: {subprocess_pids}")
            for child_pid in subprocess_pids:
                try:
                    os.kill(child_pid, signal.SIGTERM)
                except OSError:
                    pass
        sys.exit(0)

    for sig in (signal.SIGINT, signal.SIGTERM):
        signal.signal(sig, handle_termination_signal)

    with mp.Manager() as global_manager:
        worker_resources = {}
        for numa_id in active_numa_ids:
            target_devs = active_numa_map[numa_id]
            count = len(target_devs)
            iq = [global_manager.Queue() for _ in range(count)]
            rq = global_manager.Queue()
            worker_resources[numa_id] = (iq, rq)
        engine_config = EngineConfig(
            active_numa_ids=active_numa_ids,
            worker_resources=worker_resources,
            active_numa_map=active_numa_map,
            numa_configs=numa_configs,
        )

        result_queue = global_manager.Queue()
        try:
            engine_process = mp.spawn(
                fn=engine_run,
                args=(engine_config, run_cases, args, result_queue),
                nprocs=active_process_cnt,
                join=False,
                daemon=False,
            )
            for process in engine_process.processes:
                subprocess_pids.append(process.pid)
            logger.info(f"Main process ID: {cur_process_id}")
            logger.info(f"Engine process IDs: {subprocess_pids}")

            # 等待所有子进程完成
            all_results = []
            for _ in range(active_process_cnt):
                all_results.extend(result_queue.get())
            npu_result_display(all_results)

            for process in engine_process.processes:
                process.join()
        except Exception as e:
            log_file = os.path.join(FILE_DIR, "npu_burn.log")
            logger.error(f"Create subprocesses failed, error msg: {e}")
            traceback.print_exc()
            print(f"❌ An error occurred. Please check the log file for details: {log_file}")
            sys.exit(-1)

    # 记录程序结束时间并计算总运行时长
    end_time = time.time()
    total_time = end_time - start_time
    logger.info(f"✅ Program run completed! Total runtime: {total_time:.2f}seconds ({total_time / 60:.2f}minutes)")


if __name__ == "__main__":
    main()
