# CATMonitor 系统测试报告（无 NPU / 无 GPU）

> **项目**: CATMonitor (Computing Availability Tools Monitor) — CATHelper 底座
> **测试对象**: 本地 `main` 分支 @ `0fbc4f9`（合并 helper 升级）
> **合并提交**: *feat: 合并 helper 升级 — feature-scoped 收集/掉卡检测/faultsub/snapshot/stragglerout*
> **合并内容**:
> - `internal/source/dcmi` 新增 `ErrorCodeList`/`CardDrop`（`DeviceNotReadyErrCode -8012`）掉卡检测
> - `internal/collectors/npu` 新增 `error_codes`（完整 hex 列表）+ `card_drop` 指标
> - `internal/metrics` feature-scoped 收集（`SetFeatureScope`/`inScope`）+ `ComponentIntervals` 推导每组件 cadence
> - `internal/config` 新增 `features`/`faultsub`/`straggler_output`/`snapshot` 配置段
> - `cmd/catmonitor` 装配 feature metrics 覆盖、C_comp 推导、straggler storage 包装、faultsub detector/dispatcher/webhook
> - 新增 `features/{snapshot,faultsub,stragglerout}` 三包 + `features/dfee/main.go` 独立二进制
> - `features/web`/`features/dfee` 重构为只读消费者（读 daemon 生产的 global + per-component 快照）
> **测试日期**: 2026-08-05
> **测试人**: opencode
> **目标版本**: CATMonitor v0.3.3

---

## 测试环境

| 项 | 值 |
|----|----|
| OS | Ubuntu (WSL2, kernel 6.18.33.2-microsoft-standard-WSL2) |
| 架构 | linux/amd64 |
| Go | go1.23.4 |
| 硬件 | Intel Core i5-7200U (4 核)；**无 NPU / 无 GPU / 无 IPMI**（验证无硬件采集器优雅降级） |
| CANN DCMI | 头文件 `/usr/local/Ascend/driver/include/dcmi_interface_api.h` 不存在 → `-tags dcmi` 自动关闭（符合预期） |
| 测试配置 A (off) | `snapshot/straggler/faultsub = false`，`features:[dfee]`，`min_priority=low`，`data_dir=/tmp/cm-test/data` |
| 测试配置 B (on) | 在 A 基础上 `snapshot/straggler/faultsub.enabled=true`，dir 指向 `/tmp/cm-test/{snapshot,straggler}`，`faultsub.rest_addr=:9101` |

---

## 硬门禁结果：**通过 ✅**

---

## 1. 构建与静态检查

### 1.1 go vet / go build / go test
```
$ go vet ./cmd/...     → exit 0（无告警）
$ go build ./cmd/... ./features/dfee/... ./features/web/...   → exit 0
$ go test ./...        → 全部包 ok，无失败
   ok  features/dfee       ok  features/snapshot     ok  features/faultsub
   ok  features/web        ok  features/health      ok  features/stragglerout
   ok  internal/metrics    ok  internal/collectors/* ok  internal/source/*
```
> 注：`make` 未安装，构建/测试均用 `go` 直接执行，等价于 Makefile 的 `all`/`test` 目标。

### 1.2 三二进制独立构建（对应 `make all/web/dfee`）
```
$ go build -o bin/catmonitor      ./cmd/catmonitor   → 成功 (10.4 MB)
$ go build -o bin/catmonitor-web ./features/web      → 成功 (8.5 MB)
$ go build -o bin/catmonitor-dfee ./features/dfee    → 成功 (8.5 MB)
```
DCMI tag 自动探测：未检测到 CANN 头 → 不加 `-tags dcmi`（无 NPU 硬件，符合预期）。

### 1.3 已知限制
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

### 2.3 collect -o table（单次采集，feature-scoped）
使用 `features:[dfee]` scoped 模式（仅采集 dfee `metrics.yaml` 声明的指标）：
```
$ ./bin/catmonitor collect -o table -config <off.yaml>   (exit 0)
组件分布：  cpu 43   disk 16   memory 11   network 2    = 72 条
样例：
  cpu   usage           0.00   %     core=total
  cpu   load_average    1.56         interval=1m
  cpu   model_info      2.00   cores model_name=Intel(R) Core(TM) i5-7200U...
  disk  space_usage    30.04   %     device=C:\134,mount_point=/mnt/c,fstype=9p
  memory usage_detail 3840.61 MB    field=total
```
**feature-scoped 收集验证**：对比无 features 的非 scoped 模式（92 条：cpu 21/disk 44/memory 19/network 7/npu 1），scoped 后指标集收窄至 dfee 所需（disk 44→16、network 7→2），且 dfee 声明的低优先级 cpu 细节（per-core cache 等）通过 `LoadModuleOverride` 提升后得以保留（cpu 21→43）—— **`SetFeatureScope` + `LoadModuleOverride` + `ComponentIntervals` 三者协同生效**。

### 2.4 health（健康检查）
```
CATMonitor Health Report
======================================================================
  Overall Score:  [█████████████████████████████░]  97 / 100   [ Excellent ]
  Server Type:    accelerated
  Check Time:     2026-08-05 11:10:08
  ----------------------------------------------------------------------
  Component        Score / Max    Status       Deductions
  CPU                10 / 10       OK           -
  MEMORY             20 / 20       OK           -
  DISK                7 / 10       Warning      smart_failed (-3)
  NPU                60 / 60       OK           -
  ----------------------------------------------------------------------
  TOTAL              97 / 100      Excellent
  [OK]    All systems are healthy.
```
无 NPU 硬件下 `health` 子命令的 auto 检测判为 `accelerated`（NPU 采集器存在即分配 60 分权重，无故障判 OK）；DISK 因无 smartctl/SMART 数据扣 3 分（已知降级行为）。

---

## 3. daemon + Prometheus exporter（配置 A：全 opt-in 关闭）

```
$ ./bin/catmonitor daemon -config <off.yaml>
level=INFO msg="derived per-component cadence from features" features=[dfee] declared_components=6
level=INFO msg="exporter listening" addr=:9100
level=INFO msg="starting collector" name=chassis/cpu/memory/disk/gpu/npu/network ...
level=INFO msg="CATMonitor daemon started" version=0.3.3
```
daemon 无 error/warn/panic；`features:[dfee]` 派生 per-component cadence 正常；opt-in 功能（snapshot/straggler/faultsub）关闭时无相关日志，行为与升级前一致。

### exporter :9100/metrics（Prometheus 格式）
```
$ curl -s http://localhost:9100/metrics   (114 行)
# HELP catmonitor_disk_space_usage disk/space_usage
# TYPE catmonitor_disk_space_usage gauge
catmonitor_disk_space_usage{device="drivers",fstype="9p",mount_point="/usr/lib/wsl/drivers"} 30.04
catmonitor_disk_space_usage{device="/dev/sdd",fstype="ext4",mount_point="/"} 0.38
...
catmonitor_cpu_load_average{interval="1m"} 1.56
catmonitor_memory_usage_detail{field="total"} 3840.61328125
组件覆盖： catmonitor_cpu  catmonitor_disk  catmonitor_memory  catmonitor_network
$ curl -s -o /dev/null http://localhost:9100/   → HTTP 404（exporter 仅暴露 /metrics，符合设计）
```
标准 Prometheus 文本格式（`# HELP` / `# TYPE` + `metric{labels} value`）。

---

## 4. 全量 opt-in 模式（配置 B：snapshot + straggler + faultsub 开启）

### 4.1 启动日志
```
level=INFO msg="derived per-component cadence from features" features=[dfee] declared_components=6
level=INFO msg="straggler_output enabled" data_dir=/tmp/cm-test/straggler
level=INFO msg="faultsub enabled" rest_addr=:9101
level=INFO msg="snapshot production enabled" dir=/tmp/cm-test/snapshot refresh=1s
level=INFO msg="exporter listening" addr=:9100
level=INFO msg="CATMonitor daemon started" version=0.3.3
level=INFO msg="hardware specs distributed to snapshot writers" count=6
```
派生 cadence 生效后采集器实际 interval 变为 **cpu=1s / disk=2s / memory=2s / network=1s / npu=1s**（来自 `features/dfee/metrics.yaml`，覆盖了 catmonitor.yaml 的 3s/5s）—— `ComponentIntervals` 门禁生效。

### 4.2 snapshot 文件生产（daemon 统一生产）
```
/tmp/cm-test/snapshot/
  snapshot.json          (2033 B, 全局: health/collectors/intervals/system_specs)
  snapshot_cpu.json      (10.2 KB)
  snapshot_disk.json     (7.9 KB)
  snapshot_memory.json   (2.6 KB)
  snapshot_network.json  (937 B)
```
per-comp + global snapshot 均生成；`chassis/gpu/npu` 因无硬件产出 0 指标，未产 per-comp 文件（符合「空组件跳过」设计），但 global `snapshot.json` 含其 health/specs。`refresh=1s` 原子写（temp + rename），全局文件每秒刷新。

### 4.3 exporter :9100/metrics（scoped，114 行，同配置 A）
opt-in 开启不影响 exporter 输出（snapshot 是独立写路径）；组件覆盖仍 cpu/disk/memory/network。

---

## 5. web 只读消费者（:9527）

```
$ ./bin/catmonitor-web -addr :9527 -snapshot-dir /tmp/cm-test/snapshot
level=INFO msg="web server starting (read-only consumer)" addr=:9527 snapshot_dir=/tmp/cm-test/snapshot
```

| 端点 | 方法 | HTTP | Content-Type | 说明 |
|------|------|------|--------------|------|
| `/` | GET | 200 | text/html | 首页（静态 SPA，1407 B） |
| `/api/snapshot` | GET | 200 | application/json | 组装 global+per-comp snapshot |
| `/dfee/` | GET | **404** | text/plain | dfee 不再挂载于 web（架构变更，见 §6） |

`/api/snapshot` 响应结构验证完整：
```json
{
  "session_id": "...", "timestamp": "2026-08-05T11:21:14...+08:00",
  "refresh_interval_ms": 1000, "history_points": 60,
  "health": {"score":100,"grade":"Excellent","server_type":"cpu_only",
             "components":{"cpu":{"score":30,"max":30},"disk":{"score":30,"max":30},"memory":{"score":40,"max":40}}},
  "metrics": [ {"component":"cpu","name":"user_time","value":67827,"unit":"jiffies","labels":{"core":"1"},...}, ... (80 条) ],
  "history": {...}, "specs": [...]
}
```
web 只读消费 daemon snapshot 链路打通；无自采集（已删 `DataCollector`）。
> 注：global snapshot writer 的 health auto 检测判为 `cpu_only`（无加速器指标流入），与 `health` 子命令的 `accelerated` 判定存在差异（见 §8 已知限制）。

---

## 6. dfee 独立二进制（:9528）

```
$ ./bin/catmonitor-dfee -addr :9528 -snapshot-dir /tmp/cm-test/snapshot
level=INFO msg="dfee server starting (read-only consumer)" addr=:9528 snapshot_dir=/tmp/cm-test/snapshot
```

| 端点 | HTTP | Content-Type | 说明 |
|------|------|--------------|------|
| `/` | 200 | text/html | SPA 首页（catch-all，940 B） |
| `/dfee/static/dfee.js` | 200 | text/javascript | 静态资源（24672 B） |
| `/api/dfee` | 200 | application/json | dfee 派生指标（25 charts） |

`/api/dfee` 返回 25 个图表；无 NPU 数据的图表 series 为空但结构完整（如 `{"id":"npu_aicore_freq","title":"AICore频率","series":null}`）—— 优雅降级。dfee 作为独立二进制只读消费 snapshot 正常。

---

## 7. faultsub 故障订阅（:9101，配置 B 开启）

```
level=INFO msg="faultsub enabled" rest_addr=:9101
```

| 操作 | 端点 | HTTP | 响应 |
|------|------|------|------|
| 查询事件 | `GET /faultsub/events` | 200 | `[]`（无硬件→无故障事件） |
| 查询订阅 | `GET /faultsub/subscriptions` | 200 | `[]` |
| 创建订阅 | `POST /faultsub/subscriptions` | **201** | `{"id":"sub-0001","delivery":"webhook","endpoint":"http://localhost:9999/hook",...}`（id 自动生成） |
| 创建订阅(错误字段) | `POST` 用 `url` 而非 `endpoint` | 400 | `{"error":"webhook delivery requires 'endpoint' URL"}`（字段校验生效） |
| 删除订阅 | `DELETE /faultsub/subscriptions/sub-0001` | 204 | （成功删除） |

faultsub REST API（订阅 CRUD + 事件查询）端到端验证通过；`detector`/`dispatcher`/`webhook`/`subscription` 装配正常，无硬件下静默不产事件（符合设计）。

### straggler_output（:9101 无关，文件输出）
`features/stragglerout` 装配成功（日志 `straggler_output enabled`）；但本环境无 NPU KPI 指标，`/tmp/cm-test/straggler/` 未产 KPI 文件（`flush_interval=60s` + 无数据，符合设计——完整验证需 NPU 真机）。

---

## 8. 无硬件采集器优雅降级验证

| 采集器 | 硬件 | 行为 |
|--------|------|------|
| npu | 无（无 CANN DCMI 头，未加 `-tags dcmi`） | collector 正常启动不崩溃；输出 0~1 条降级指标；新增 `error_codes`/`card_drop` 因无硬件未产生（符合预期） |
| gpu | 无（无 nvidia-smi） | collector 正常启动不崩溃；无指标输出 |
| chassis | 无（无 ipmi/dmidecode 权限） | 未产 per-comp snapshot，但 global snapshot 含 chassis 字段 |
| cpu/memory/disk/network | 有 | 全量采集正常 |

daemon 全程日志 `grep -i "error\|warn\|fatal\|panic"` = 空。**优雅降级符合设计预期**。

---

## 9. 测试结论

| 门禁 | 结果 |
|------|------|
| `go vet` / `go build` / `go test` | ✅ 全绿 |
| 三二进制构建（daemon/web/dfee） | ✅ 全部成功 |
| CLI（version/list/collect/health） | ✅ 正常 |
| feature-scoped 收集（`SetFeatureScope`+`LoadModuleOverride`+`ComponentIntervals`） | ✅ 指标集收窄 + cadence 覆盖生效 |
| daemon + exporter（:9100/metrics，Prometheus 格式） | ✅ 正常（114 行，GET / → 404） |
| snapshot 统一生产（daemon 产 → web/dfee 读） | ✅ 端到端打通 |
| web 只读消费（:9527，`/api/snapshot` 等） | ✅ 正常 |
| dfee 独立二进制（:9528，`/` + `/api/dfee` 25 charts） | ✅ 正常 |
| faultsub 订阅 API（:9101，CRUD） | ✅ 正常 |
| 无硬件采集器优雅降级 | ✅ 无 error/warn/panic |

**整体：通过，可进入发布流程。**

### 已知限制与发现（非阻塞，建议后续处理）
1. **【BUG】`-c` 短 flag 为死代码**：`cmd/catmonitor/main.go` 的 `loadConfig` 注册了 `c` 短 flag 但其值被丢弃，只使用长 `config` flag。因此 `catmonitor -c <path>` 不生效（静默回退到 `platform.ConfigPath()`=`/etc/catmonitor/catmonitor.yaml`，文件不存在则用 `config.Default()`）。**临时绕过**：一律使用 `-config <path>` 长形式。建议修复：将短 flag 的值映射到 configPath，或改用标准库 `flag` 的同义注册。
2. **health `server_type` 判定不一致**：`catmonitor health` 子命令 auto 判为 `accelerated`（NPU 采集器存在即给 60 分），而 snapshot global writer 的 health 判为 `cpu_only`（无加速器指标流入）。两条 auto 检测路径口径不同，建议统一「无 NPU 指标时不应判 accelerated」。
3. **`-race` 需 cgo**，本环境未覆盖竞态检测。
4. **DISK `smart_failed (-3)` 扣分**：无 smartctl/无 SMART 数据，已知降级行为。
5. **无 NPU/GPU/IPMI 真机环境**：`error_codes`/`card_drop`/straggler KPI 等新增 NPU 相关能力仅验证「不崩溃 + 优雅降级」，完整功能需在昇腾服务器补测。
6. **snapshot.json 原子写瞬态**：`refresh=1s` 下全局快照每秒 temp+rename，外部 `cat`/`open` 直读有极小概率命中 ENOENT 窗口；web/dfee 的 Go 读路径已处理（读失败返回 503 重试），不影响消费端。
7. **`/api/refresh`、`DataCollector` 已删除**：本次重构的架构变更，符合设计。
