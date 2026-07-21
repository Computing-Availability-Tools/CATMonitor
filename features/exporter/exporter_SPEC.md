# exporter Prometheus 导出模块技术规格说明书 (exporter_SPEC)

> **文档定位**：本文档是 exporter 模块的唯一设计与规格文档。
>
> **对应代码**：`features/exporter/` 目录（Go package `exporter`，与主项目同一 Go module）。
>
> **不新增采集**：exporter 作为 daemon 的 Storage 插件，复用 daemon 已有的采集管道，一次采集同时落盘 JSONL + 缓存到内存，HTTP 端点从缓存读取并转为 Prometheus 文本格式。

---

## 1. 概述

### 1.1 目标

为 CATMonitor 提供 Prometheus 兼容的指标导出能力。Prometheus server 定时从 `/metrics` 端点拉取全部采集指标，用于长期存储、告警、Grafana 可视化。

核心原则：
1. **不重复采集** — 复用 daemon 的 Scheduler 采集管道，CachingStorage 包装在 JSONLStorage 外面。
2. **单进程** — daemon 既是采集器又是 exporter，不需要额外进程。
3. **零侵入** — exporter 模块实现 `collector.Storage` 接口，daemon 只需 ~5 行改动。
4. **标准 Prometheus 格式** — 输出 `text/plain; version=0.0.4`，含 HELP/TYPE/labels。

### 1.2 架构

```
daemon (cmd/catmonitor/main.go)
  │
  ├── Scheduler.Start(ctx, configs)
  │     └── runCollector goroutine
  │           └── collectAndStore(c)
  │                 → c.Collect()
  │                 → metrics.Filter(allMetrics)
  │                 → CachingStorage.Write(metrics)
  │                       ├── 1. 按组件分组更新内存缓存（原子替换）
  │                       └── 2. 委托 JSONLStorage.Write(metrics)（历史落盘）
  │
  └── HTTP server (:9100)
        ├── GET /metrics      → CachingStorage.AllMetrics() → Prometheus 文本
        ├── GET /-/healthy    → 200 OK
        └── GET /-/ready      → 缓存非空则 200，否则 503
```

---

## 2. 目录结构

```
features/exporter/
├── exporter_SPEC.md         # 本设计文档
├── storage.go               # CachingStorage：实现 collector.Storage 接口
├── prometheus.go            # Prometheus 文本编码器 + ServeMetrics HTTP handler
├── storage_test.go          # 缓存更新、多组件、并发安全测试
└── prometheus_test.go       # 格式编码、counter/gauge 判定、标签映射测试
```

---

## 3. CachingStorage 设计（storage.go）

### 3.1 结构

```go
type CachingStorage struct {
    inner collector.Storage              // 委托给 JSONLStorage（历史落盘）
    mu    sync.RWMutex
    cache map[string][]collector.Metric  // component → 最新一批 metrics
}

func NewCachingStorage(inner collector.Storage) *CachingStorage {
    return &CachingStorage{
        inner: inner,
        cache: make(map[string][]collector.Metric),
    }
}
```

### 3.2 Write 实现

Scheduler 对每个 collector 独立调用 `Write(metrics)`。每次 Write 的 metrics 属于同一个 component（如全部是 cpu 或全部是 npu）。

```go
func (s *CachingStorage) Write(metrics []collector.Metric) error {
    // 1. 按组件分组，更新缓存中对应组件的 metrics
    byComponent := groupByComponent(metrics)
    s.mu.Lock()
    for comp, ms := range byComponent {
        s.cache[comp] = ms
    }
    s.mu.Unlock()
    // 2. 委托内层 Storage 落盘 JSONL（不影响缓存逻辑）
    return s.inner.Write(metrics)
}
```

**按组件分组的原因**：Scheduler 按 collector 独立调用 Write，每次只传一个 collector 的 metrics。如果直接覆盖整个缓存，后面的 collector 会覆盖前面的。按组件分组后，每个组件的最新值独立缓存，互不覆盖。

### 3.3 AllMetrics 实现

Prometheus 端点调用，返回缓存中所有组件的最新 metrics：

```go
func (s *CachingStorage) AllMetrics() []collector.Metric {
    s.mu.RLock()
    defer s.mu.RUnlock()
    var all []collector.Metric
    for _, ms := range s.cache {
        all = append(all, ms...)
    }
    return all
}
```

### 3.4 健康检查

```go
func (s *CachingStorage) Ready() bool {
    s.mu.RLock()
    defer s.mu.RUnlock()
    return len(s.cache) > 0
}
```

### 3.5 并发安全

- `Write` 用写锁 `Lock()` 更新缓存
- `AllMetrics` / `Ready` 用读锁 `RLock()` 读取缓存
- Prometheus 拉取和 Scheduler 采集可并发执行

---

## 4. Prometheus 文本格式编码（prometheus.go）

### 4.1 指标命名

```
catmonitor_{component}_{name}
```

| 采集器指标 | Prometheus 指标名 |
|-----------|------------------|
| cpu/usage | `catmonitor_cpu_usage` |
| cpu/user_time | `catmonitor_cpu_user_time` |
| npu/temperature | `catmonitor_npu_temperature` |
| network/rx_bytes_total | `catmonitor_network_rx_bytes_total` |
| disk/throughput | `catmonitor_disk_throughput` |
| chassis/power | `catmonitor_chassis_power` |

> 指标名中的特殊字符（如 `/`、`-`）替换为 `_`。Prometheus 指标名只允许 `[a-zA-Z_:][a-zA-Z0-9_:]*`。

### 4.2 指标类型判定

| 类型 | 判定规则 | 示例 | PromQL |
|------|---------|------|--------|
| **counter** | 指标名以 `_time` 或 `_total` 结尾（累计计数器，值只增不减） | `user_time`, `rx_bytes_total`, `steal_time` | `rate()`, `increase()` |
| **gauge** | 其他所有指标（瞬时值，可增可减） | `temperature`, `usage`, `power_draw`, `iops` | `avg_over_time()`, `max_over_time()` |

判定函数：

```go
func isCounter(name string) bool {
    return strings.HasSuffix(name, "_time") ||
           strings.HasSuffix(name, "_total")
}
```

### 4.3 标签映射

直接从 `metric.Labels` 映射为 Prometheus labels，key 和 value 都做 `strconv.Quote` 转义：

```go
// metric.Labels = {"npu_id":"0","aicore":"1"}
// → {npu_id="0",aicore="1"}
```

### 4.4 编码器

```go
func Encode(metrics []collector.Metric) []byte
```

输出格式（按指标名分组，同名的多条数据共享一组 HELP/TYPE）：

```
# HELP catmonitor_cpu_usage CPU usage percentage
# TYPE catmonitor_cpu_usage gauge
catmonitor_cpu_usage{core="total"} 12.3
catmonitor_cpu_usage{core="0"} 15.0

# HELP catmonitor_cpu_user_time Cumulative user-mode CPU time in jiffies
# TYPE catmonitor_cpu_user_time counter
catmonitor_cpu_user_time{core="total"} 3357

# HELP catmonitor_npu_temperature NPU temperature in degrees Celsius
# TYPE catmonitor_npu_temperature gauge
catmonitor_npu_temperature{npu_id="0"} 55
catmonitor_npu_temperature{npu_id="1"} 60

# HELP catmonitor_network_rx_bytes_total Cumulative received bytes
# TYPE catmonitor_network_rx_bytes_total counter
catmonitor_network_rx_bytes_total{interface="eth0"} 21227412
```

**编码规则**：
- 按 `catmonitor_{component}_{name}` 分组，每组输出一次 `# HELP` + `# TYPE`
- HELP 文本取自 `metricDisplayNames` 映射或 fallback 到指标名
- 同组内按 labels 排序输出多条数据行
- 值用 `strconv.FormatFloat(v, 'f', -1, 64)` 格式化
- 空缓存返回空响应（200 + 空体）

### 4.5 HTTP Handler

```go
func ServeMetrics(addr string, store *CachingStorage, logger *slog.Logger) {
    mux := http.NewServeMux()
    mux.HandleFunc("/metrics", func(w, r) {
        metrics := store.AllMetrics()
        w.Header().Set("Content-Type", "text/plain; version=0.0.4; charset=utf-8")
        w.Write(Encode(metrics))
    })
    mux.HandleFunc("/-/healthy", func(w, r) {
        w.WriteHeader(200)
    })
    mux.HandleFunc("/-/ready", func(w, r) {
        if store.Ready() {
            w.WriteHeader(200)
        } else {
            w.WriteHeader(503)
        }
    })
    logger.Info("exporter listening", "addr", addr)
    http.ListenAndServe(addr, mux)
}
```

---

## 5. daemon 集成

### 5.1 改动（cmd/catmonitor/main.go，~5 行）

```go
import "github.com/Computing-Availability-Tools/CATMonitor/features/exporter"

func runDaemon() {
    // ...
    // 改前：
    // store, _ := storage.New(cfg.Storage.DataDir)
    // scheduler := collector.NewScheduler(collector.DefaultRegistry, store, logger)

    // 改后：
    jsonlStore, _ := storage.New(cfg.Storage.DataDir)
    cacheStore := exporter.NewCachingStorage(jsonlStore)
    scheduler := collector.NewScheduler(collector.DefaultRegistry, cacheStore, logger)
    defer jsonlStore.Close()

    // 启动 Prometheus 端点
    go exporter.ServeMetrics(":9100", cacheStore, logger)

    // ... 其余不变 ...
}
```

### 5.2 不影响的文件

- `features/web/` — 不变（web 有独立的 DataCollector，不走 daemon 的 Storage）
- `features/dfee/` — 不变（dfee 读 snapshot.json）
- `features/health/` — 不变
- `internal/` — 不变

---

## 6. 配置

exporter 端口可通过环境变量或 daemon config 扩展：

```yaml
# configs/catmonitor.yaml（可选扩展）
exporter:
  addr: ":9100"  # 默认 :9100
```

当前阶段先用硬编码 `:9100`，后续可加配置项。

---

## 7. 测试策略

### 7.1 storage_test.go

| 测试 | 覆盖内容 |
|------|---------|
| TestWriteAndRead | 写入 cpu + npu metrics → AllMetrics 返回全部 |
| TestWriteReplacesComponent | 同一组件两次 Write → 缓存只保留最新 |
| TestWriteMultiComponent | 不同组件的 Write 不互相覆盖 |
| TestConcurrentAccess | 并发 Write + AllMetrics 不 panic |
| TestReady | 空缓存→false，写入后→true |
| TestDelegatesToInner | Write 同时调用内层 Storage（验证委托） |

### 7.2 prometheus_test.go

| 测试 | 覆盖内容 |
|------|---------|
| TestEncodeBasic | 基本 gauge 指标格式 |
| TestEncodeCounter | `_time`/`_total` 后缀标记为 counter |
| TestEncodeLabels | 多标签行（npu_id, device, direction 等） |
| TestEncodeEmpty | 空 metrics → 空输出 |
| TestEncodeSpecialChars | 指标名/标签值含特殊字符的转义 |
| TestMetricName | `catmonitor_{component}_{name}` 命名规则 |
| TestIsCounter | `_time`/`_total` 判定 |

运行：`go test ./features/exporter/`

---

## 8. Prometheus scrape 配置

```yaml
scrape_configs:
  - job_name: 'catmonitor'
    scrape_interval: 15s
    static_configs:
      - targets: ['localhost:9100']
    metrics_path: '/metrics'
```

---

## 9. 常用 PromQL 示例

```promql
# CPU 利用率（5 分钟平均）
avg_over_time(catmonitor_cpu_usage{core="total"}[5m])

# CPU 用户态时间增长率（每秒 jiffies）
rate(catmonitor_cpu_user_time{core="total"}[1m])

# 网络接收速率（每秒字节）
rate(catmonitor_network_rx_bytes_total{interface="eth0"}[1m])

# NPU 温度最大值
max_over_time(catmonitor_npu_temperature[10m])

# 磁盘 IOPS
catmonitor_disk_iops{direction="read"}

# 内存使用量
catmonitor_memory_usage_detail{field="total"}
```

---

## 10. 关键设计决策

| 决策 | 选择 | 理由 |
|------|------|------|
| 数据来源 | 复用 daemon 采集（CachingStorage 插件） | 不重复采集，单进程部署 |
| 形态 | 库（非独立二进制） | daemon 导入后获得 Prometheus 能力，不额外开进程 |
| Storage 接口 | 包装内层 JSONLStorage | 落盘 + 缓存一次完成，符合现有接口设计 |
| 缓存粒度 | 按 component 分组 | Scheduler per-collector 调用 Write，按组件独立缓存避免互相覆盖 |
| 指标前缀 | `catmonitor_` | 与项目名一致，避免与系统其他 exporter 冲突 |
| counter 判定 | 命名约定（`_time`/`_total` 后缀） | 零侵入，不改 metrics.yaml 结构 |
| 端口 | 9100 | Prometheus exporter 常用端口 |
| 指标覆盖范围 | 全部 High/Medium 指标 | 经过 metrics.Filter 后的指标集，与 JSONL 落盘一致 |
| 健康探针 | `/-/healthy` + `/-/ready` | 标准 Prometheus exporter 惯例 |

---

## 11. 已知限制与后续预留

1. **采集间隔 ≠ 拉取间隔**：daemon 按 per-collector 间隔采集（CPU 3s、Disk 5s），Prometheus 按 scrape_interval 拉取（如 15s）。缓存中是各组件最近一次采集值，不是同一时刻的全局快照。对监控无影响（指标是瞬时值）。
2. **counter 单调性**：CPU 时间 jiffies 和网络字节在重启后会重置（counter reset），Prometheus 的 `rate()` 能自动处理 counter reset。
3. **无 TLS / 认证**：当前不提供 TLS 或 basic auth。如需安全，可通过 reverse proxy（nginx）提供。
4. **无独立配置文件**：端口暂用硬编码。后续可扩展 daemon config 加 `exporter.addr` 字段。
5. **dfee 不受影响**：dfee 读 snapshot.json，不经过 CachingStorage，两条管道完全独立。

---

*文档版本：v1.0 · 对应代码：features/exporter/（4 个文件）*
