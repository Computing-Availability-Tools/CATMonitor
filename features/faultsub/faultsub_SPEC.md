# faultsub 故障订阅模块技术规格说明书 (faultsub_SPEC)

> **文档定位**：本文档是 faultsub 模块的设计与规格文档。
>
> **对应代码**：`features/faultsub/`（Go package `faultsub`，与主项目同一 Go module）。
>
> **不新增采集**：faultsub 作为 daemon 的 Storage 插件，复用 daemon 已有的采集管道，对每次采集到的指标做故障判定并推送事件给已订阅者；不改变 JSONL/Prometheus 输出。
>
> **零新依赖**：仅用 Go 标准库 `net/http`，CATMonitor 保持"仅 yaml.v3"外部依赖。

---

## 1. 概述

### 1.1 目标

为 CATMonitor 提供故障信息的订阅/推送能力。外部故障管理者（如 EEP 弹性容错特性）可订阅 NPU 故障事件，CATMonitor 在采集周期内判定故障并以 **HTTP Webhook** 主动推送 `FaultEvent`（JSON），同时提供 REST API 用于订阅注册/查询/快照/事件回补。

核心原则：
1. **复用采集管道** — FaultStorage 包装内层 `collector.Storage`（exporter CachingStorage → JSONLStorage），一次 Write 同时落盘 + 故障判定。
2. **零新依赖** — 推送/REST 均用 `net/http`，不引入 ZMQ/msgpack 等库。
3. **跨机** — 订阅者注册回调 URL，CATMonitor 反向 POST，支持分机部署。
4. **事件驱动** — 故障按状态变迁推送（出现/恢复），持续故障不重复推送；订阅级去抖可进一步抑制。
5. **默认关闭** — `faultsub.enabled` 默认 false，不启用时 daemon 行为与现状完全一致。

### 1.2 架构

```
daemon (cmd/catmonitor/main.go)
  │
  ├── Scheduler.Start(ctx, configs)
  │     └── runCollector goroutine
  │           └── collectAndStore(c)
  │                 → c.Collect()
  │                 → metrics.Filter(allMetrics)
  │                 → FaultStorage.Write(metrics)            [若 faultsub 启用]
  │                       ├── 1. 委托 CachingStorage.Write → JSONLStorage（落盘，不变）
  │                       ├── 2. FaultDetector.Detect(metrics) → []FaultEvent
  │                       └── 3. Dispatcher.Dispatch(ev)
  │                             ├── record → 环形缓冲（poll/REST 回补）
  │                             └── 匹配订阅 → shouldFire(去抖) →
  │                                   ├── webhook: go deliverWebhook → net/http POST
  │                                   └── poll: 已 record，无需动作
  │
  └── REST :9101 (net/http)
        ├── POST   /faultsub/subscriptions        注册订阅（声明回调URL/类型/NPU/去抖）
        ├── GET    /faultsub/subscriptions         列出
        ├── GET    /faultsub/subscriptions/{id}    查看
        ├── DELETE /faultsub/subscriptions/{id}    注销
        ├── GET    /faultsub/snapshot             各 NPU 最新活跃故障快照
        ├── GET    /faultsub/events?since=&type=&npu_id=  近期事件回补
        ├── GET    /faultsub/types                支持的故障类型
        ├── GET    /-/healthy                      200 OK
        └── GET    /-/ready                        有采集过则 200，否则 503
```

---

## 2. 目录结构

```
features/faultsub/
├── faultsub_SPEC.md         # 本设计文档
├── event.go                 # FaultEvent / FaultType / Severity 数据模型
├── subscription.go          # Subscription / SubscriptionManager（订阅表+去抖）
├── detector.go              # FaultDetector 故障判定规则引擎（纯 Go）
├── storage.go               # FaultStorage：实现 collector.Storage 接口（管道 tap）
├── dispatcher.go            # Dispatcher：匹配+去抖+异步分发+环形缓冲
├── webhook.go               # Webhook 推送器（net/http 客户端）
├── server.go                # REST 订阅 API（/faultsub/*）
├── detector_test.go         # 各故障规则 + 恢复 + 变迁驱动测试
├── storage_test.go          # tap 委托 + 推送 + 快照 + 并发测试
├── dispatcher_test.go       # 分发 + 去抖 + poll + 环形缓冲 + 重试测试
└── server_test.go           # REST API 各端点测试
```

---

## 3. 核心设计

### 3.1 FaultStorage（storage.go）

实现 `collector.Storage`，包装内层（CachingStorage）：

```go
func (s *FaultStorage) Write(metrics []collector.Metric) error {
    s.inner.Write(metrics)                    // 落盘 + exporter 缓存（不变）
    events := s.detector.Detect(metrics)      // 故障判定
    for _, ev := range events {
        s.updateSnapshot(ev)                 // 更新各 NPU 最新活跃故障
        s.dispatcher.Dispatch(ev)            // 推送/缓冲
    }
    return nil
}
```

- 内层写失败仅记日志，不阻断（故障检测不能拖垮落盘）。
- `Snapshot()` 返回各 NPU 最新活跃故障副本（供 REST `/faultsub/snapshot`）。
- `Ready()` 缓存非空即 true（供 `/-/ready`）。

### 3.2 FaultDetector（detector.go）

消费 `[]collector.Metric`，仅处理 `component=="npu"`，按 `npu_id` 分组评估规则。

**变迁驱动**：维护上一周期各 (npu,type) 的活跃集，仅当故障**新出现**或**恢复**时发事件；持续故障不重复发。事件语义清晰（"某状态改变"）。

| 规则 | 判定条件 | Severity |
|------|---------|----------|
| `card_drop` | `card_drop`==1；或 error_code labels 含 `0x40f84e00`（大小写不敏感） | critical |
| `npu_health` | `health_status` label status ∈ {Alarm,Critical} | warning→critical |
| `npu_error_code` | `error_code` Value>0 | warning |
| `hbm_uce` | `hbm_double_ecc` Value>0 | critical |
| `ddr_uce` | `ddr_double_ecc` Value>0 | critical |
| `roce_link_down` | `roce_link_status`==0/status=="down"；或 `roce_link_health` link 异常 | warning |
| `driver_unhealthy` | `driver_health`!=0 | warning |

`RuleConfig` 开关：未配置的规则默认启用（fail-open）。

### 3.3 Subscription / SubscriptionManager（subscription.go）

```go
type Subscription struct {
    ID, Types, Components, NPUIDs  // 过滤
    Delivery DeliveryMethod        // webhook | poll
    Endpoint string                // webhook 回调 URL
    DebounceMs int                 // 同(npu,type)去抖窗口
    MinSeverity string             // warning | critical
    CreatedAt time.Time
}
```

- `Subscription` 是纯值类型（无锁），可安全复制用于 REST 序列化。
- 去抖状态（`map[string]time.Time`）由 `SubscriptionManager` 持有，按订阅 ID 隔离。
- `ShouldFire(sub, ev)` = 匹配 + 去抖判定；`Matched(ev)` 返回匹配的活跃订阅指针。

### 3.4 Dispatcher（dispatcher.go）

```go
func (d *Dispatcher) Dispatch(ev FaultEvent) {
    d.record(ev)                              // 写环形缓冲（poll/回补）
    for _, s := range d.subs.Matched(ev) {
        if !d.subs.ShouldFire(s, ev) { continue }   // 去抖
        switch s.Delivery {
        case DeliveryWebhook: go d.deliverWebhook(s.Endpoint, ev)  // 异步不阻塞采集
        case DeliveryPoll:    // 已 record
        }
    }
}
```

- Webhook 异步投递（`go`），慢/不可达订阅者不拖慢采集管道。
- 失败重试 `webhook_retry` 次（线性退避 `defaultBackoff`），仍失败仅记日志（事件仍在环形缓冲可回补）。
- `Events(since, type, npuID)` 按时间/类型/NPU 过滤，返回时间顺序切片。

### 3.5 Webhook（webhook.go）

`net/http` 客户端，POST `FaultEvent` JSON 到 `Endpoint`，头：
- `Content-Type: application/json`
- `X-CatMonitor-Event: <type>`
- `X-CatMonitor-EventID: <id>`

超时 `webhook_timeout`（默认 5s）。

### 3.6 REST API（server.go）

Go 1.22+ `ServeMux` 模式路由（`GET /faultsub/subscriptions/{id}`）。见 §1.2。

---

## 4. 配置

```yaml
faultsub:
  enabled: false            # 默认关闭
  rest_addr: ":9101"
  webhook_timeout: 5s
  webhook_retry: 1
  event_buffer: 1024
  defaults:
    debounce_ms: 0
    min_severity: "warning"
  rules:
    card_drop: true
    npu_health: true
    npu_error_code: true
    hbm_uce: true
    ddr_uce: true
    roce_link_down: true
    driver_unhealthy: false
```

---

## 5. daemon 集成

`cmd/catmonitor/main.go runDaemon()`（受 `cfg.FaultSub.Enabled` 门控）：

```go
var sink collector.Storage = cacheStore
if cfg.FaultSub.Enabled {
    det  := faultsub.NewDetector(rules)
    wh   := faultsub.NewWebhook(cfg.FaultSub.WebhookTimeout, logger)
    disp := faultsub.NewDispatcher(wh, faultsub.NewSubscriptionManager(), ...)
    fstore := faultsub.NewFaultStorage(cacheStore, det, disp, logger)
    go faultsub.ServeAPI(ctx, cfg.FaultSub.RestAddr, disp, fstore, logger)
    sink = fstore
}
scheduler := collector.NewScheduler(collector.DefaultRegistry, sink, logger)
```

Storage 链路：`Scheduler → FaultStorage(若启用) → CachingStorage → JSONLStorage`。未启用时与现状完全一致。

---

## 6. FaultEvent 消息格式

```json
{
  "event_id": "a1b2c3d4...",
  "type": "card_drop",
  "component": "npu",
  "npu_id": "3",
  "severity": "critical",
  "detail": { "error_codes": "0x40f84e00", "card_drop": "1" },
  "timestamp": "2026-07-28T10:30:00Z",
  "recovered": false
}
```

---

## 7. 测试

运行 `go test ./features/faultsub/`：

| 文件 | 覆盖 |
|------|------|
| detector_test.go | 各规则判定；变迁驱动（出现/恢复/持续不重复）；规则开关；非 npu 忽略；并发 |
| storage_test.go | 委托内层；故障推送；健康不推送；快照更新/清除；Ready；并发 |
| dispatcher_test.go | webhook/poll 交付；去抖抑制；事件过滤；环形缓冲溢出；重试后丢弃 |
| server_test.go | REST 各端点；快照；事件过滤；健康探针；ServeAPI 启停 |

---

## 8. 关键设计决策

| 决策 | 选择 | 理由 |
|------|------|------|
| 接入点 | `collector.Storage` 插件 | 与 exporter 同模式，零侵入采集管道 |
| 推送协议 | HTTP Webhook（net/http） | 零新依赖，跨机天然支持 |
| 消息编码 | JSON | 跨语言、易调试 |
| 事件语义 | 变迁驱动 | 持续故障不重复推送，流安静 |
| 去抖位置 | 订阅级（SubscriptionManager） | 不同订阅者可独立去抖；Subscription 保持值类型 |
| 异步投递 | `go deliverWebhook` | 慢订阅者不阻塞采集 |
| 默认开关 | false | 渐进采用，零回归 |

---

*文档版本：v1.0 · 对应代码：features/faultsub/（11 个文件）*
