---
name: catmonitor-faults
description: >
  查询与订阅 CATMonitor 检测的 NPU 故障事件：掉卡、健康状态告警、错误码、HBM/DDR ECC、
  RoCE 链路中断、驱动异常、慢节点检测。支持查当前活跃故障、回查历史事件、注册 poll 订阅。
  Use this skill whenever the user asks about NPU faults, card drop, ECC errors, RoCE link down,
  fault events, 掉卡, 故障事件, NPU 告警, or wants to subscribe to fault push notifications.
compatibility: >
  需要 CATMonitor (Go 版) v0.3.6+ 且 daemon 开启 faultsub.enabled: true（默认关闭）。
  REST API 地址由配置 faultsub.rest_addr 决定（示例配置为 :19321，以目标机配置为准）。
metadata:
  author: catmonitor
  version: 0.1.0
---

# NPU 故障事件查询与订阅（catmonitor-faults）

查询 CATMonitor faultsub 模块检测/记录的 NPU 故障事件，或注册订阅持续接收故障推送。

## 触发条件

用户出现以下意图时使用本技能：

- "这台机器有没有掉过卡？" / "最近有什么 NPU 故障？"
- "NPU 有没有 ECC 错误？" / "RoCE 链路断过吗？"
- "现在有哪些活跃故障？" / "帮我订阅掉卡告警"
- 英文：any card drop / NPU fault events / active faults / subscribe to fault notifications

## 第一步：前置检查（必须先执行）

faultsub 是 opt-in 特性，**默认关闭**。按顺序探测：

```bash
# 1. 二进制存在性
command -v catmonitor && catmonitor version

# 2. 探测 REST API（地址以目标机 catmonitor.yaml 的 faultsub.rest_addr 为准，示例 :19321）
curl -s -o /dev/null -w '%{http_code}' http://localhost:19321/-/healthy   # 200 = 服务在
curl -s -o /dev/null -w '%{http_code}' http://localhost:19321/-/ready     # 200 = 就绪可查询
```

- `/-/healthy` 返回 200 但 `/-/ready` 返回 503：服务在但内部存储未就绪（刚启动），稍后重试。
- 连接被拒/超时：faultsub 未启用。告知用户需要在 `/etc/catmonitor/catmonitor.yaml` 设置 `faultsub.enabled: true` 并重启 daemon；**不要**假装查询成功。
- 常见地址：默认配置示例为 `:19321`；若不确定，可查配置文件或让用户告知。

## 第二步：选择操作

| 用户需求 | 操作 | API |
|----------|------|-----|
| 有哪些故障类型 | 能力发现 | `GET /faultsub/types` |
| 现在有哪些活跃故障 | 查快照 | `GET /faultsub/snapshot` |
| 过去有没有出过某类故障 | 查历史事件 | `GET /faultsub/events?since=&type=&npu_id=` |
| 持续接收故障通知 | 注册订阅 | `POST /faultsub/subscriptions`（agent 场景优先 `delivery: poll`） |
| 管理已有订阅 | 查/删订阅 | `GET/DELETE /faultsub/subscriptions[/{id}]` |

完整 API 契约（请求/响应示例、状态码、字段表）见 [references/api-reference.md](./references/api-reference.md)。

## 第三步：执行查询

```bash
# 活跃故障快照（按 npu_id 的 map）
curl -s http://localhost:19321/faultsub/snapshot

# 最近 24h 的 card_drop 事件
curl -s "http://localhost:19321/faultsub/events?type=card_drop&since=$(date -u -d '24 hours ago' +%Y-%m-%dT%H:%M:%SZ)"
```

事件对象关键字段：

```json
{
  "event_id": "a1b2c3...",
  "type": "card_drop",
  "component": "npu",
  "npu_id": "3",
  "severity": "critical",
  "detail": { "error_codes": "..." },
  "timestamp": "2026-09-08T14:30:00Z",
  "recovered": false
}
```

- `type` 的 8 种取值与含义见 [references/fault-types.md](./references/fault-types.md)。
- **变迁驱动**：故障"新出现"或"恢复"才产生事件；持续存在的故障不会重复推送。`recovered: true` 表示故障恢复事件。
- `detail` 是原始证据（错误码、ECC 计数等），报告时应原样引用。

## 第四步：订阅（仅当用户明确要求持续通知时）

agent 通常无法暴露回调端点，**优先用 poll 模式**（无需 endpoint，后续靠 `GET /faultsub/events` 拉取）：

```bash
curl -s -X POST http://localhost:19321/faultsub/subscriptions \
  -H 'Content-Type: application/json' \
  -d '{"types": ["card_drop", "hbm_uce"], "delivery": "poll", "min_severity": "warning"}'
```

- 响应 201，返回带服务端分配 `id`（形如 `sub-0001`）的订阅对象。
- webhook 模式（需要 `endpoint` URL，适合用户环境有常驻 HTTP 服务时）：CATMonitor 会 POST 事件 JSON 到该 URL，请求头 `X-CatMonitor-Event`/`X-CatMonitor-EventID` 便于路由。
- 注册订阅是**写操作**：注册前向用户确认过滤条件，注册后报告订阅 id，并告知如何删除（`DELETE /faultsub/subscriptions/{id}`）。

## 注意事项

- **配置路径与参数顺序**：确认配置时读 `/etc/catmonitor/catmonitor.yaml`（未 `make install` 的机器在别处，让用户提供路径）。执行 CLI 时子命令必须紧跟 `catmonitor` 之后；`-c` 放在子命令前只会打印 usage 且**退出码为 0（静默失败）**。
- **只查询，不诊断修复**：本技能只读故障事件；处置建议（隔离/换件）可作为报告内容，但不要执行任何修复动作。
- **事件 ≠ 实时状态**：事件是变迁记录；"当前是否故障"看 snapshot，"发生过什么"看 events。
- **检测规则可配置**：`faultsub.rules.*` 可关闭个别规则（如 `driver_unhealthy` 示例配置默认关闭），查询结果为空不代表规则未配置过。
- **外部事件**：`straggler_detected` 类事件来自外部检测器注入（POST /faultsub/events），并非 CATMonitor 自身采集判定。
- **缓冲区有限**：events 是环形缓冲（默认 1024 条，配置 `event_buffer`），很久之前的事件可能已被覆盖。

## 参考

- [references/fault-types.md](./references/fault-types.md) — 8 种故障类型的含义、severity 与对应指标
- [references/api-reference.md](./references/api-reference.md) — REST API 完整契约
- 仓库文档：[features/faultsub/faultsub_SPEC.md](../../features/faultsub/faultsub_SPEC.md)（权威规格）
