---
name: catmonitor-metrics
description: >
  查询服务器运行指标：CPU、内存、磁盘、GPU、NPU（昇腾）、网卡、机箱共 216 项，支持单次采集、
  常驻快照与 Prometheus 多种数据源。Use this skill whenever the user asks about system metrics,
  CPU/NPU/GPU temperature or utilization, memory usage, disk space, network throughput,
  查指标, 看温度, 利用率, 磁盘空间, 网络带宽, or wants current runtime values from CATMonitor.
compatibility: 需要 CATMonitor (Go 版) v0.3.6+ 已安装且 catmonitor 在 PATH 中。CLI 单次采集无需 daemon；snapshot/Prometheus 数据源需要 daemon 运行。
metadata:
  author: catmonitor
  version: 0.1.0
---

# 运行指标查询（catmonitor-metrics）

查询 CATMonitor 采集的服务器运行指标，支持多种数据源与输出格式。

## 触发条件

用户出现以下意图时使用本技能：

- "查一下 NPU 温度 / CPU 使用率 / 内存占用 / 磁盘空间"
- "现在 GPU 利用率多少？" / "网卡的收发带宽怎么样？"
- "这台机器有哪些采集器？能查什么指标？"
- 英文：show metrics / current temperature / utilization / disk usage

## 第一步：前置检查（必须先执行）

```bash
command -v catmonitor && catmonitor version
```

- 正常输出形如 `CATMonitor v0.3.6 (Go 1.23+)`，继续。
- 若是老版 `catmonitor_cli`（`-q/-m` 参数风格）或命令不存在：告知需要 Go 版 CATMonitor，不要用老版参数风格调用。

## 第二步：了解可查什么（可选）

```bash
catmonitor list          # 7 个采集器：chassis/cpu/disk/gpu/memory/network/npu
```

全部 216 项指标的名称、优先级、单位、标签见 [references/metric-catalog.md](./references/metric-catalog.md)。
常用查询速查：

| 用户想查 | 指标（component.name） |
|----------|------------------------|
| CPU 使用率 / 负载 | `cpu.usage`、`cpu.load_average` |
| CPU 温度（需 BMC） | `cpu.temperature`、`cpu.mem_temperature` |
| 内存使用 / 饱和度 | `memory.usage`、`memory.saturation` |
| 磁盘空间 / SMART | `disk.space_usage`、`disk.smart_status` |
| NPU 温度 / 利用率 / 掉卡 | `npu.temperature`、`npu.utilization`、`npu.card_drop` |
| NPU HBM / ECC | `npu.memory_usage`、`npu.hbm_double_ecc` |
| 网络吞吐 / 错误包 | `network.throughput`、`network.error_count` |
| 整机功耗（需 BMC） | `chassis.power` |

## 第三步：选择数据源

| 场景 | 数据源 | 获取方式 |
|------|--------|----------|
| 当前时刻一次性查询（默认） | CLI 单次采集 | `catmonitor collect -o json` |
| 人读表格 | CLI 单次采集 | `catmonitor collect -o table` |
| daemon 运行中，要含历史的最新值 | snapshot 文件 | 读 `/var/lib/catmonitor/snapshot/snapshot_<comp>.json` |
| daemon 运行中，整机聚合视图 | snapshot 全局文件 | 读 `/var/lib/catmonitor/snapshot/snapshot.json` |
| 已接入 Prometheus | daemon exporter | `curl -s http://localhost:19320/metrics` |
| 能效视图（node_exporter 风格） | dfee exporter | `curl -s http://localhost:9333/metrics` |

各数据源的具体格式与解析要点见 [references/output-formats.md](./references/output-formats.md)。

## 第四步：执行查询

```bash
# 一次性全量采集（JSON）
catmonitor collect -o json

# 表格预览（人读）
catmonitor collect -o table

# 从 JSON 输出中过滤目标指标（jq 示例，逐行流式过滤）
catmonitor collect -o json | jq -c 'select(.component=="npu" and .name=="temperature")'
```

> **解析坑位**：
> 1. `collect -o json` 输出的是 **JSONL（每行一个 JSON 对象）**，不是 JSON 数组！逐行解析，或用 `jq -s` 聚合成数组。
> 2. 每行格式：`{"component":"cpu","name":"usage","value":45.2,"unit":"%","labels":{"core":"total"},"timestamp":"..."}`。同一指标名常带多行（不同 labels，如每核/每卡/每挂载点）。
> 3. 输出行数受配置 `collection.min_priority`（low/medium/high）预过滤影响，行数变少不代表异常。
> 4. **未实现的 flag**：`-d/--data-dir`、`-v/--verbose`、`--component`、`--interval` 均未实现（历史文档误列），不要使用。
> 5. 无 GPU/NPU/BMC 时对应指标为空，不报错（优雅降级）。

## 第五步：组织答案

1. 用户问具体指标：过滤对应 `component` + `name`，按 labels（`npu_id`/`core`/`interface`/`mount_point` 等）逐实例列出值与单位。
2. 用户泛问"看看这台机器"：先 `catmonitor list` + 汇总各部件关键指标（每部件 1-3 个代表值），再询问要深入哪块。
3. 数值必须来自命令输出，不要编造；指标缺失时说明"未采集到"并注明可能原因（无该硬件/被优先级过滤/无 BMC）。

## 注意事项

- **配置文件路径**：命令默认读 `/etc/catmonitor/catmonitor.yaml`；目标机未执行过 `make install` 时需用 `-c <路径>` 指定配置（如 `catmonitor collect -c /path/catmonitor.yaml -o json`）。报错 `failed to load config` 即此原因。
- **参数顺序**：子命令必须紧跟在 `catmonitor` 之后。若把 `-c` 放在子命令前（如 `catmonitor -c cfg collect`），只会打印 usage 且**退出码为 0（静默失败）**，务必避免。
- **采集耗时**：`collect` 是一次全量采集（含磁盘、SMART 等），秒级到十几秒，不要高频循环调用。
- **数值单位**：注意 `unit` 字段（`%`、`°C`、`W`、`MB/s` 等）；`*_time` 类 jiffies 指标与 `*_util` 类百分比指标含义不同。
- **静态信息指标**：`model_info`、`npu_num`、`chip_type`、`driver_version` 等值放在 `labels` 或 `value` 中，用于回答"这机器什么配置"。
- **历史趋势**：`collect` 只有当前值；要看趋势用 snapshot 的 `history` 字段（最近 60 点，需 daemon + snapshot.enabled）。
- **Prometheus 消费**：daemon exporter 指标名规则为 `catmonitor_<component>_<name>`（`-`/`/`/`.` 转 `_`），`_time`/`_total` 结尾为 counter，其余 gauge。

## 参考

- [references/metric-catalog.md](./references/metric-catalog.md) — 216 项指标目录（按部件分章 + 标签约定）
- [references/output-formats.md](./references/output-formats.md) — JSONL / snapshot / Prometheus 各格式详解与解析示例
- 仓库文档：[docs/CATMonitor_indi_list.md](../../docs/CATMonitor_indi_list.md)（指标权威清单）、[docs/User_Manual.md](../../docs/User_Manual.md) §3
