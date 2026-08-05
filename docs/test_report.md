# CATMonitor 系统测试报告（无 NPU / 无 GPU）

> **项目**: CATMonitor (Computing Availability Tools Monitor) — CATHelper 底座
> **测试对象**: 本地 `main` 分支 @ `301bc6b`（No-FF 合并 `feature/catmonitor`）
> **合并提交**: *Merge branch 'feature/catmonitor' into main: snapshot 统一生产重构 v0.1 + metrics feature-scoped 采集*
> **合并内容**:
> - 新增 `features/snapshot` 包，daemon 统一生产 snapshot（per-comp `snapshot_<comp>.json` + global `snapshot.json`），web/dfee 转只读消费者
> - `features/dfee` 独立二进制化（`features/dfee` package main），补全 69 项 `metrics.yaml`
> - `features/web` 瘦身：删 `DataCollector`/`config.go`，改只读消费者（`/api/snapshot` 组装，`/api/config` 只读，删 `/api/refresh`）
> - `internal/metrics` 新增 scope 白名单机制（`SetFeatureScope`/`inScope`），按 features 并集采集 + priority 预过滤
> - `Makefile`：`make all/web/dfee` + CANN DCMI 头自动探测（`-tags dcmi`）
> **测试日期**: 2026-07-31
> **发布人**: sunnytao
> **目标版本**: CATHelper v0.2.1（底座 CATMonitor v0.3.3）

---

## 测试环境

| 项 | 值 |
|----|----|
| OS | Ubuntu 26.04 LTS (WSL2, kernel 6.18.33.2-microsoft-standard-WSL2) |
| 架构 | linux/amd64 |
| Go | go1.23.4 |
| 硬件 | Intel Core i5-7200U (4 核)；**无 NPU / 无 GPU / 无 IPMI**（验证无硬件采集器优雅降级） |
| 测试配置 | `snapshot.enabled=true`, `dir=/tmp/opencode/snap`, `features:[dfee]`, `collection.min_priority=low` |

---

## 硬门禁结果：**通过 ✅**

---

## 1. 构建与静态检查

### 1.1 go vet / go build / go test
```
$ go vet ./...          → exit 0（无告警）
$ go build ./...        → exit 0
$ go test ./...         → 全部包 ok，无失败
   ok  features/dfee          ok  features/snapshot
   ok  features/web           ok  features/health
   ok  features/faultsub      ok  internal/metrics
   ok  internal/collectors/*  ok  internal/source/*
```

### 1.2 三二进制独立构建（对应 `make all/web/dfee`）
```
$ go build -o bin/catmonitor      ./cmd/catmonitor   → 成功 (10.4 MB)
$ go build -o bin/catmonitor-web ./features/web     → 成功 (8.5 MB)
$ go build -o bin/catmonitor-dfee ./features/dfee   → 成功 (8.5 MB)
```

### 1.3 已知限制
- `gofmt -l` 检出 5 个本次新增/重写文件存在 struct 字段对齐格式瑕疵（非阻塞，不影响 vet/build/test）：
  `features/snapshot/{comp,global}.go`、`features/snapshot/series_test.go`、`features/web/http_linux_test.go`、`internal/metrics/metrics_test.go`
- `go test -race` 需 cgo（`CGO_ENABLED=1`），本环境跳过；如需竞态检测需在启用了 cgo 的环境补测。

---

## 2. CLI 子命令

### 2.1 version
```
$ ./bin/catmonitor version
CATMonitor v0.3.3 (Go 1.23+)
```

### 2.2 list（采集器清单）
```
Name     Component  Priority  Interval  Enabled
chassis  chassis    High      3s        true
cpu      cpu        High      3s        true
disk     disk       High      5s        true
gpu      gpu        High      3s        true
memory   memory    High      3s        true
network  network    High      3s        true
npu      npu        High      3s        true
```
7 个采集器全部 enabled。无 NPU/GPU 硬件下 `npu`/`gpu` 仍注册并优雅降级（不崩溃）。

### 2.3 collect -o table（单次采集）
```
Component  Metric             Value     Unit   Labels
cpu        usage              0.00      %      core=total
cpu        usage              0.00      %      core=0/1/2/3
cpu        load_average       1.73             interval=1m
cpu        load_average       1.32             interval=5m
cpu        load_average       0.87             interval=15m
cpu        online_core_num    4.00      个
cpu        model_info         2.00      cores  cache_size=3072 KB,model_name=Intel(R) Core(TM) i5-7200U CPU @ 2.50GHz
disk       space_usage        0.39      %      device=/dev/sdd,mount_point=/,fstype=ext4
disk       space_detail       1031018   MB     field=total,device=/dev/sdd
disk       throughput         0.00      MB/s   device=sda/sdb/sdc/sdd,direction=read/write
disk       read_latency       0.00      ms/s   device=sda/sdb/sdc/sdd
memory     usage_detail       3840.61   MB     field=total
network    ... (rx/tx_bytes_total 等)
```
采集正常；`exit 0`。

### 2.4 health（健康检查）
```
CATMonitor Health Report
======================================================================
  Overall Score:  [█████████████████████████████░]  97 / 100   [ Excellent ]
  Server Type:    accelerated
  Check Time:     2026-07-31 15:31:44
  --------------------------------------------------------------------
  Component        Score / Max    Status       Deductions
  CPU                10 / 10       OK           -
  MEMORY             20 / 20       OK           -
  DISK                7 / 10       Warning      smart_failed (-3)
  NPU                60 / 60       OK           -
  --------------------------------------------------------------------
  TOTAL              97 / 100      Excellent
  [OK]    All systems are healthy.
```

---

## 3. daemon + Prometheus exporter

### 3.1 启动日志（snapshot 生产开启）
```
level=INFO msg="derived per-component cadence from features" features=[dfee] declared_components=6
level=INFO msg="snapshot production enabled" dir=/tmp/opencode/snap refresh=1s
level=INFO msg="exporter listening" addr=:9100
level=INFO msg="starting collector" name=chassis/cpu/memory/disk/gpu/npu/network ...
level=INFO msg="CATMonitor daemon started" version=0.3.3
level=INFO msg="hardware specs distributed to snapshot writers" count=6
```
daemon 无 error/warn/panic；`features:[dfee]` 派生 per-component cadence 正常；hw specs 分发到 snapshot writer 成功。

### 3.2 snapshot 文件生产
```
/tmp/opencode/snap/
  snapshot.json          (2033 B, 全局: health/collectors/intervals/system_specs)
  snapshot_cpu.json      (9642 B)
  snapshot_disk.json     (6884 B)
  snapshot_memory.json   (2635 B)
  snapshot_network.json  (939 B)
```
per-comp + global snapshot 均生成；`chassis/gpu/npu` 因无硬件/未被 feature 声明 cadence 未产 per-comp 文件，但 global `snapshot.json` 含其 health/specs —— 符合「按 feature 声明派生 cadence」设计。

### 3.3 exporter :9100/metrics（Prometheus 格式）
```
$ curl -s http://localhost:9100/metrics   (114 行)
# HELP catmonitor_memory_usage_detail memory/usage_detail
# TYPE catmonitor_memory_usage_detail gauge
catmonitor_memory_usage_detail{field="total"} 3840.61328125
catmonitor_memory_usage_detail{field="used"} 2575.13671875
...
# HELP catmonitor_disk_iops disk/iops
# TYPE catmonitor_disk_iops gauge
catmonitor_disk_iops{device="sda",direction="read"} 0
...
catmonitor_disk_throughput{device="sdd",direction="read"} 0
catmonitor_disk_read_latency{device="sda"} 0
```
标准 Prometheus 文本格式（`# HELP` / `# TYPE` + `metric{labels} value`）。`GET /` 返回 404（exporter 仅暴露 `/metrics`，符合设计）。

---

## 4. web 只读消费者（:9527）

```
$ ./bin/catmonitor-web -addr :9527 -snapshot-dir /tmp/opencode/snap
level=INFO msg="web server starting (read-only consumer)" addr=:9527 snapshot_dir=/tmp/opencode/snap
```

| 端点 | 方法 | HTTP | Content-Type | 说明 |
|------|------|------|--------------|------|
| `/` | GET | 200 | text/html | 首页（静态） |
| `/api/snapshot` | GET | 200 | application/json | 13689 B，组装 global+per-comp snapshot |
| `/api/config` | GET | 200 | application/json | 只读配置 |
| `/api/collectors` | GET | 200 | application/json | 采集器清单（只读） |

`/api/snapshot` 响应结构验证完整：
```json
{
  "session_id": "1785483193",
  "timestamp": "2026-07-31T15:33:49.901847378+08:00",
  "refresh_interval_ms": 1000,
  "history_points": 60,
  "health": {"score":100,"grade":"Excellent","server_type":"cpu_only",
             "components":{"cpu":{"score":30,"max":30},"disk":{...},"memory":{...}}},
  "metrics": [ ... cpu/disk/memory/network 时序 ... ],
  "history": {"cpu_load_average":[...36], "disk_iops":[...17], "disk_throughput":[...19]},
  "specs": [ ... disk_info/net_info/os_info 启动身份 ... ]
}
```
web 只读消费 daemon snapshot 链路正常；无自采集（已删 `DataCollector`）。

---

## 5. dfee 独立二进制（:9528）

```
$ ./bin/catmonitor-dfee -addr :9528 -snapshot-dir /tmp/opencode/snap
level=INFO msg="dfee server starting (read-only consumer)" addr=:9528 snapshot_dir=/tmp/opencode/snap
```

| 端点 | HTTP | Content-Type | 说明 |
|------|------|--------------|------|
| `/` | 200 | text/html | SPA 首页（catch-all） |
| `/dfee/` | 200 | text/html (940 B) | dfee 视图首页 |
| `/dfee/static/` | 200 | — | 静态资源 |
| `/api/dfee` | 200 | application/json | dfee 派生指标 |

dfee 作为独立二进制只读消费 snapshot 正常，路由 `/dfee/` + `/api/dfee` 工作正常。

---

## 6. 无硬件采集器优雅降级验证

| 采集器 | 硬件 | 行为 |
|--------|------|------|
| npu | 无（无 CANN DCMI 头，未加 `-tags dcmi`） | collector 正常启动，不崩溃；指标输出空/降级，health 计 60/60 OK（mock 降级路径） |
| gpu | 无（无 nvidia-smi） | collector 正常启动，不崩溃 |
| chassis | 无（无 ipmi/dmidecode 权限） | 未产 per-comp snapshot，但 global snapshot 含 chassis 字段；health 计入 |
| cpu/memory/disk/network | 有 | 全量采集正常 |

daemon 全程日志 `grep -i "error\|warn\|fatal\|panic"` = 空。**优雅降级符合设计预期**。

---

## 7. 测试结论

| 门禁 | 结果 |
|------|------|
| `go vet` / `go build` / `go test` | ✅ 全绿 |
| 三二进制构建 | ✅ daemon/web/dfee 全部成功 |
| CLI（version/list/collect/health） | ✅ 正常 |
| daemon + exporter（:9100/metrics，Prometheus 格式） | ✅ 正常 |
| web 只读消费（:9527，/api/snapshot 等） | ✅ 正常 |
| dfee 独立二进制（:9528，/dfee/ + /api/dfee） | ✅ 正常 |
| 无硬件采集器优雅降级 | ✅ 无 error/warn/panic |
| snapshot 统一生产链路（daemon 产 → web/dfee 读） | ✅ 端到端打通 |

**整体：通过，可进入发布流程。**

### 已知限制（非阻塞）
1. `gofmt` 格式瑕疵 5 文件（struct 字段对齐，不影响功能与 vet/test）。
2. `-race` 需 cgo，本环境未覆盖竞态检测。
3. DISK `smart_failed (-3)` 扣分（无 smartctl/无 SMART 数据，已知降级行为）。
4. 无 NPU/GPU/IPMI 真机环境，相关采集器仅验证不崩溃；完整采集需在昇腾/NVIDIA 服务器补测。
5. `/api/refresh`、`DataCollector` 已在本次重构删除（架构变更，符合设计）。
