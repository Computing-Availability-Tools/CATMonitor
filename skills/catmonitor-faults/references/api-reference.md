# faultsub REST API 契约

> 权威来源：[features/faultsub/server.go](../../../features/faultsub/server.go) 与 [faultsub_SPEC.md](../../../features/faultsub/faultsub_SPEC.md)。
> 监听地址：配置 `faultsub.rest_addr`（示例配置 `:19321`）。所有响应均为 JSON。

## 服务探活

| 方法 | 路径 | 成功 | 失败 |
|------|------|------|------|
| GET | `/-/healthy` | 200（服务存活） | 连接失败 = faultsub 未启用 |
| GET | `/-/ready` | 200（存储就绪） | 503 刚启动未就绪 |

## 能力发现

```
GET /faultsub/types
→ 200 { "types": ["card_drop","npu_health","npu_error_code","hbm_uce","ddr_uce","roce_link_down","driver_unhealthy","straggler_detected"] }
```

## 活跃故障快照

```
GET /faultsub/snapshot
→ 200 { "<npu_id>": { ...FaultEvent }, "<npu_id>": { ... } }
```

返回各 NPU **当前活跃**（未恢复）的最新故障，按 `npu_id` 键控的 map。无故障时为 `{}`。
注意：外部注入的事件不进入快照（不是 CATMonitor 自身检测状态）。

## 历史事件回补

```
GET /faultsub/events?since=<RFC3339>&type=<FaultType>&npu_id=<id>
→ 200 [ { ...FaultEvent }, ... ]
```

| Query 参数 | 说明 |
|------------|------|
| `since` | RFC3339 时间戳，只返回之后的事件；**格式非法返回 400** |
| `type` | 按故障类型过滤（可选） |
| `npu_id` | 按设备过滤（可选） |

- 返回时间序数组（环形缓冲，默认容量 1024，配置 `event_buffer`），无事件时 `[]`。
- 生成 `since` 参数示例：`date -u -d '24 hours ago' +%Y-%m-%dT%H:%M:%SZ`。

## 外部事件注入（高级，慎用）

```
POST /faultsub/events
Body: 单个 FaultEvent JSON（event_id/timestamp/component 缺失时服务端补全，component 默认 "npu"）
→ 202 { "event_id": "...", "status": "dispatched" }
```

把外部检测器（如 straggler）的发现注入同一分发管道。**写操作**：仅当用户明确要求对接外部检测时使用。

## 订阅管理

### 创建订阅

```
POST /faultsub/subscriptions
Content-Type: application/json
Body:
{
  "types": ["card_drop", "hbm_uce"],   // 可选，空 = 全部类型
  "components": ["npu"],               // 可选
  "npu_ids": ["0", "3"],               // 可选，空 = 全部设备
  "delivery": "poll",                  // webhook（默认）| poll
  "endpoint": "http://...",            // webhook 模式必填；poll 模式忽略
  "debounce_ms": 0,                    // 同 (npu,type) 抑制窗口
  "min_severity": "warning"            // warning | critical；空 = 接受全部
}
→ 201 { ...Subscription }   // 含服务端分配的 "id"（如 "sub-0001"）与 "created_at"
```

- **webhook 模式**：CATMonitor 对每个匹配事件 POST FaultEvent JSON 到 endpoint，头 `Content-Type: application/json`、`X-CatMonitor-Event: <type>`、`X-CatMonitor-EventID: <id>`；超时默认 5s，失败重试 1 次（线性退避），异步投递。
- **poll 模式**：无需 endpoint，事件进入缓冲，订阅方定期 `GET /faultsub/events` 拉取。**agent 场景首选**（agent 通常无法暴露回调端口）。
- webhook 缺 endpoint → 400 `webhook delivery requires 'endpoint' URL`。

### 查询 / 删除

```
GET    /faultsub/subscriptions        → 200 [ { ...Subscription } ]
GET    /faultsub/subscriptions/{id}   → 200 { ...Subscription } | 404
DELETE /faultsub/subscriptions/{id}   → 204 | 404
```

## FaultEvent 结构

| 字段 | 类型 | 说明 |
|------|------|------|
| `event_id` | string | 全局唯一随机 hex，用于去重 |
| `type` | string | 8 种故障类型之一 |
| `component` | string | 固定 `npu` |
| `npu_id` | string | 受影响设备号 |
| `severity` | string | `warning` / `critical` |
| `detail` | object | 原始证据（error_codes、health、ecc_count 等，string→string map） |
| `timestamp` | string | RFC3339 检测时间 |
| `recovered` | bool | true = 故障恢复事件 |

## curl 速查

```bash
BASE=http://localhost:19321   # 以目标机 faultsub.rest_addr 为准

curl -s $BASE/-/ready                                  # 探活
curl -s $BASE/faultsub/types                           # 故障类型全集
curl -s $BASE/faultsub/snapshot                        # 当前活跃故障
curl -s "$BASE/faultsub/events?since=$(date -u -d '1 hour ago' +%Y-%m-%dT%H:%M:%SZ)"  # 近 1h 事件
curl -s -X POST $BASE/faultsub/subscriptions -H 'Content-Type: application/json' \
     -d '{"types":["card_drop"],"delivery":"poll"}'    # 注册 poll 订阅
curl -s -X DELETE $BASE/faultsub/subscriptions/sub-0001  # 删除订阅
```
