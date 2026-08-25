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
import subprocess
from collections import defaultdict
from dataclasses import dataclass, field
from typing import List, Dict, Any, Union, Optional

from common.log import logger


def _find_command_path(cmd: str) -> Optional[str]:
    """
    使用which命令查找工具路径
    :param cmd: 要查找的命令名
    :return: 命令的完整路径，如果找不到返回None
    """
    # which命令的常见绝对路径
    which_candidates = ['/usr/bin/which', '/bin/which', '/usr/local/bin/which']

    # 尝试每个可能的which路径
    for which_path in which_candidates:
        try:
            result = subprocess.run([which_path, cmd], capture_output=True, text=True, check=True)
            return result.stdout.strip()
        except (subprocess.CalledProcessError, FileNotFoundError):
            continue

    logger.warning(f"Could not find command '{cmd}' using any known which path")
    return None


def get_numa_configs():
    # 查找lscpu命令路径
    lscpu_path = _find_command_path('lscpu') or '/usr/bin/lscpu'  # 使用默认路径作为备选

    try:
        lscpu_ret = subprocess.run([lscpu_path], capture_output=True, text=True, check=True)
        raw_output = lscpu_ret.stdout
    except Exception as err:
        logger.warning(f"Failed to execute lscpu: {err}")
        raw_output = ""

    sys_info = {}
    for line in raw_output.splitlines():
        if ":" in line:
            key, val = line.split(":", 1)
            sys_info[key.strip()] = val.strip()

    # 提取基础硬件参数
    total_cpus = int(sys_info.get("CPU(s)", 0))
    threads_per_core = int(sys_info.get("Thread(s) per core", 0))
    cores_per_socket = int(sys_info.get("Core(s) per socket", 0))
    sockets_count = int(sys_info.get("Socket(s)", 0))

    cpus_per_socket = (total_cpus // sockets_count) if sockets_count > 0 else 0

    # 判断核心与线程的连续性
    core_is_continuous = True
    thread_is_continuous = False

    node0_info = sys_info.get("NUMA node0 CPU(s)")
    if node0_info:
        if "-" in node0_info:
            core_is_continuous = True
            boundaries = node0_info.split("-")
            # 通过首尾差值判断线程是否连续
            if (int(boundaries[-1]) - int(boundaries[0])) > cpus_per_socket:
                thread_is_continuous = False
            else:
                thread_is_continuous = True
        else:
            core_is_continuous = False
            thread_is_continuous = True

    configs = []
    try:
        node_count = int(sys_info.get("NUMA node(s)", 0))
        if node_count == 0:
            raise ValueError("NUMA nodes count is zero or missing.")

        for n_id in range(node_count):
            cpu_ranges = sys_info.get(f"NUMA node{n_id} CPU(s)", "")
            current_node_cpus = []

            # 解析例如 "0-47,96-143" 这样的字符串
            for part in cpu_ranges.split(','):
                part = part.strip()
                if not part:
                    continue
                if '-' in part:
                    start_idx, end_idx = map(int, part.split('-'))
                    current_node_cpus.extend(range(start_idx, end_idx + 1))
                else:
                    current_node_cpus.append(int(part))

            configs.append(current_node_cpus)

    except Exception as parse_err:
        logger.warning(f"Failed to parse NUMA configs automatically: {parse_err}. Using fallback.")
        configs.clear()

        # 兜底计算逻辑重构
        for s_id in range(sockets_count):
            socket_cpus = []
            if core_is_continuous:
                if thread_is_continuous:
                    # 场景 1：核心连续，线程连续 (如 0-95, 96-191)
                    base_offset = s_id * cpus_per_socket
                    socket_cpus = list(range(base_offset, base_offset + cpus_per_socket))
                else:
                    # 场景 2：核心连续，线程交错 (如 0-47 & 96-143)
                    for t_id in range(threads_per_core):
                        base_offset = (t_id * sockets_count * cores_per_socket) + (s_id * cores_per_socket)
                        socket_cpus.extend(range(base_offset, base_offset + cores_per_socket))
            else:
                # 场景 3：核心交错 (如 0,2,4... & 1,3,5...)
                socket_cpus = list(range(s_id, total_cpus, sockets_count))

            configs.append(socket_cpus)

    return configs


def get_npu_numa_topology():
    """
    返回 NUMA 节点到 NPU 列表的映射。
    格式: {NUMA_ID: [NPU_LOGICAL_ID_1, NPU_LOGICAL_ID_2, ...]}
    例如: {0: [0, 1, 2, 3], 1: [4, 5, 6, 7]}
    表示节点0管辖卡0-3，节点1管辖卡4-7
    """
    # 使用 defaultdict(list) 方便直接 append，不用判断 key 是否存在
    numa_to_npus = defaultdict(list)

    # 查找所需命令的路径
    lspci_path = _find_command_path('lspci') or '/usr/bin/lspci'
    grep_path = _find_command_path('grep') or '/usr/bin/grep'

    try:
        with subprocess.Popen([lspci_path, '-D', '-d', '19e5:'], stdout=subprocess.PIPE, text=True) as lspci_proc:
            with subprocess.Popen(
                [grep_path, 'Processing accelerators'], stdin=lspci_proc.stdout, stdout=subprocess.PIPE, text=True
            ) as grep1_proc:
                lspci_proc.stdout.close()
                with subprocess.Popen(
                    [grep_path, 'Device'], stdin=grep1_proc.stdout, stdout=subprocess.PIPE, text=True
                ) as grep2_proc:
                    grep1_proc.stdout.close()
                    output = grep2_proc.communicate(timeout=5)[0].strip()
        if not output:
            raise RuntimeError("No NPU devices found via lspci")

        # 按 PCI 地址排序，确保逻辑 ID (0,1,2...) 顺序固定
        pci_lines = sorted(output.splitlines())

        for logical_id, line in enumerate(pci_lines):
            pci_addr = line.split()[0]

            node = 0  # 默认为 0
            try:
                with open(f"/sys/bus/pci/devices/{pci_addr}/numa_node", "r", encoding="utf-8") as f:
                    val = int(f.read().strip())
                    # -1 通常表示未开启 NUMA 或属于 Node 0
                    if val != -1:
                        node = val
            except Exception as e:
                logger.warning(f"Could not detect numa node: {e}")
                node = 0  # 读不到文件兜底为 0

            numa_to_npus[node].append(logical_id)

    except Exception as e:
        logger.warning(f"Could not detect NPU topology automatically: {e}")
        # 假设标准的 8 卡机器，前 4 张在 Node 0，后 4 张在 Node 1
        numa_to_npus = defaultdict(list)  # 重置
        for i in range(8):
            node = i // 4
            numa_to_npus[node].append(i)

    return dict(numa_to_npus)


@dataclass
class TestCase:
    name: str
    data: Dict[str, Any] = field(default_factory=dict)

    @property
    def seq(self) -> int:
        return self.data["seq"]

    def __getitem__(self, key):
        """支持通过 case['dtype'] 这种方式直接访问内部数据"""
        return self.data.get(key)

    def __repr__(self):
        return f"TestCase(name='{self.name}', seq={self.seq}, keys={list(self.data.keys())})"


class ConfigManager:
    def __init__(self, json_data: Union[str, Dict], config_data: Union[str, Dict] = None):
        if isinstance(json_data, str):
            with open(json_data, 'r', encoding='utf-8') as f:
                self.raw_data = json.load(f)
        else:
            self.raw_data = json_data

        if config_data:
            if isinstance(config_data, str):
                with open(config_data, 'r', encoding='utf-8') as f:
                    self.raw_data.update(json.load(f))
            else:
                self.raw_data.update(config_data)

        # 建立索引
        self._seq_map: Dict[int, TestCase] = {}
        self._name_map: Dict[str, TestCase] = {}
        self._groups: Dict[str, List[TestCase]] = {}

        self._parse_config()

    def _parse_config(self):
        # 1. 解析 cases
        for case_entry in self.raw_data.get("cases", []):
            for op_name, op_data in case_entry.items():
                # 直接将 op_name 和整个字典传给 TestCase
                case_obj = TestCase(name=op_name, data=op_data)

                # 建立索引
                self._seq_map[case_obj.seq] = case_obj
                self._name_map[op_name] = case_obj

        # 2. 解析分组信息
        group_config = self.raw_data.get("group", {})
        for g_name, seq_list in group_config.items():
            self._groups[g_name] = [self._seq_map[seq] for seq in seq_list if seq in self._seq_map]

    def get_by_seq(self, seq: int) -> Optional[TestCase]:
        return self._seq_map.get(seq)

    def get_by_name(self, name: str) -> Optional[TestCase]:
        return self._name_map.get(name)

    def get_by_group(self, group_name: str) -> List[TestCase]:
        return self._groups.get(group_name, [])

    def __getitem__(self, key):
        """
        支持 cfg[1] -> TestCase
        支持 cfg['matmul'] -> TestCase
        支持 cfg['group_gpt'] -> List[TestCase]
        """
        if isinstance(key, int):
            return self.get_by_seq(key)

        if isinstance(key, str):
            # 先看是不是组名，如果是组名返回列表
            if key in self._groups:
                return self._groups[key]
            # 如果不是组名，尝试按用例名查找
            return self.get_by_name(key)

        return None


@dataclass
class EngineConfig:
    active_numa_ids: list  # 活跃的NUMA节点ID列表
    worker_resources: dict  # 工作进程资源（队列等）
    active_numa_map: dict  # NUMA节点到设备的映射
    numa_configs: dict  # NUMA节点的CPU亲和性配置
