---
name: catmonitor-health
description: >
  基于 CATMonitor 对服务器做健康体检：执行一次健康检查，获取 0-100 健康分、等级（Excellent/Good/Warning/Critical）、
  逐部件扣分明细，并生成人可读的巡检报告。Use this skill whenever the user asks about node health,
  health check, health score, server status, 服务器体检, 健康巡检, 健康状况, 节点状态,
  or wants to know whether a server is healthy and what is wrong with it.
compatibility: 需要 CATMonitor (Go 版) v0.3.6+ 已安装且 catmonitor 在 PATH 中。一次性检查无需 daemon 运行。
metadata:
  author: catmonitor
  version: 0.1.0
---

# 服务器健康体检（catmonitor-health）

对服务器执行 CATMonitor 健康检查，解读健康分与扣分明细，输出结构化巡检报告。

## 触发条件

用户出现以下意图时使用本技能：

- "帮我体检一下这台服务器" / "做个健康检查" / "巡检报告"
- "这台机器健康吗？" / "服务器状态怎么样？"
- "健康分是多少？" / "为什么这台机器健康分低？"
- 英文：health check / health score / is this server healthy

## 第一步：前置检查（必须先执行）

```bash
command -v catmonitor && catmonitor version
```

- 正常输出形如 `CATMonitor v0.3.6 (Go 1.23+)`，继续后续步骤。
- 若 `catmonitor` 不存在：提示用户安装 CATMonitor（`make build && make install`），**不要**尝试用其他工具替代。
- 若找到的是 `catmonitor_cli`（老版 C 实现）或 `catmonitor version` 报未知命令：说明装的是老版本，本技能不适用，告知用户需要 Go 版 v0.3.6+。

## 第二步：选择数据源

| 场景 | 数据源 | 命令 |
|------|--------|------|
| 一次性体检（默认，无需 daemon） | CLI 单次评估 | `catmonitor health -o json` |
| daemon 运行中（snapshot 已开启） | 全局快照文件 | 读 `/var/lib/catmonitor/snapshot/snapshot.json` 的 `health` 字段 |
| Web 已启动 | HTTP API | `curl -s http://localhost:19322/api/snapshot` 取 `health` |

优先用 CLI：它自行执行一轮采集并评估，不依赖任何常驻进程。需要历史趋势时才用 snapshot（含最近历史点）。

## 第三步：执行健康检查

```bash
catmonitor health -o json
```

> **注意默认格式**：`health` 默认输出 table（给终端看的进度条图），**必须显式加 `-o json`** 才能拿到结构化数据（这一点与 `collect` 相反，后者默认 json）。v0.3.3 起 `health` 不落盘，需要留存时自行重定向。

### 输出 Schema（`-o json`）

```json
{
  "score": 94,
  "grade": "Good",
  "server_type": "accelerated",
  "components": {
    "cpu":    { "score": 12, "max": 15, "deductions": [ { "rule": "temp>75C", "penalty": 2 } ] },
    "memory": { "score": 15, "max": 15, "deductions": [] },
    "disk":   { "score": 15, "max": 15, "deductions": [] },
    "npu":    { "score": 32, "max": 35, "deductions": [ { "rule": "temp>80C", "penalty": 3 } ] }
  },
  "timestamp": "2026-09-08T14:30:00Z"
}
```

字段详解与所有扣分规则语义见 [references/health-output-schema.md](./references/health-output-schema.md)。

### 等级映射

| 得分 | 等级 | 含义 |
|------|------|------|
| 90-100 | Excellent | 服务器运行良好 |
| 75-89 | Good | 轻微问题，建议关注 |
| 60-74 | Warning | 存在风险，需检查 |
| 0-59 | Critical | 严重问题，需立即处理 |

## 第四步：解读结果

1. **总分与等级**：先看 `score`/`grade`，判断整体状况。
2. **找扣分部件**：遍历 `components`，凡 `deductions` 非空的部件逐条列出 `rule (−penalty)`。
3. **解读规则语义**：`rule` 是规则名（如 `temp>75C`、`uce_error`、`smart_failed`），各规则的触发阈值与严重程度见 [references/scoring-rules.md](./references/scoring-rules.md)。
4. **部件状态映射**：得分/满额比例 ≥90% Excellent、≥75% Good、≥60% Warning、<60% Critical。与整机等级同用一套标签（CLI `componentStatus`、Web `statusOf`、总评 `gradeForScore` 三口径一致）。注意：`health -o json` 的 components 中**没有** status 字段，部件状态需按本映射自行计算；max 为 0 的部件（如无 GPU 时的 gpu）状态为 N/A。
5. **权重方案**：各部件 `max` 由服务器类型决定（有无 GPU/NPU、NPU 卡数自动检测），方案表见 [references/scoring-rules.md](./references/scoring-rules.md) 的权重方案一节。

## 第五步：生成巡检报告

按以下模板输出 Markdown 报告（数据来自 JSON，不要编造）：

```markdown
# 服务器健康巡检报告

- 检查时间：{timestamp}
- 健康总分：{score} / 100（{grade}）
- 服务器类型：{server_type}（cpu_only 无加速卡 / accelerated 有 GPU/NPU）

## 各部件状况

| 部件 | 得分/满额 | 状态 | 扣分明细 |
|------|-----------|------|----------|
| cpu  | 12/15     | Good | temp>75C (−2) |
| ...  |           |      |      |

## 问题诊断

（对每条扣分规则给出解释与建议动作，规则语义见 references/scoring-rules.md）

## 结论与建议

（综合判断：是否需要立即处理 / 建议观察 / 运行正常）
```

## 注意事项

- **配置文件路径**：命令默认读 `/etc/catmonitor/catmonitor.yaml`；目标机未执行过 `make install` 时需用 `-c <路径>` 指定配置（如 `catmonitor health -c /path/catmonitor.yaml -o json`）。报错 `failed to load config` 即此原因。
- **参数顺序**：子命令必须紧跟在 `catmonitor` 之后。若把 `-c` 放在子命令前（如 `catmonitor -c cfg health`），只会打印 usage 且**退出码为 0（静默失败）**，务必避免。
- **优雅降级**：无 GPU/NPU/BMC 的机器上对应部件不产出指标，评估自动跳过，不报错不扣分——扣分少不代表硬件无问题，可能只是采不到。
- **network/chassis 参与总分**：CLI 表格输出只展示 cpu/memory/disk/gpu/npu 五个部件，但 network/chassis（各占 10 分）同样计入总分；JSON 输出则包含全部参与部件。
- **多卡取最差**：GPU/NPU 多卡场景按最差卡触发扣分。
- **不要反复执行**：`health` 每次都跑一轮采集，秒级完成但会短暂访问硬件；一次体检执行一次即可，不要循环调用。
- **配置影响**：`collection.min_priority` 会过滤参与评估的指标；采集目录以 `features/health/metrics.yaml` 覆盖为准。

## 参考

- [references/health-output-schema.md](./references/health-output-schema.md) — HealthScore JSON 全字段说明
- [references/scoring-rules.md](./references/scoring-rules.md) — 权重方案、各部件扣分规则与阈值语义
- 仓库文档：[features/health/HEALTH_SPEC.md](../../features/health/HEALTH_SPEC.md)（权威规格）、[docs/User_Manual.md](../../docs/User_Manual.md) §3.4
