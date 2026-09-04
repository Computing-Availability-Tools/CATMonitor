# CATMonitor 系统测试报告（无 NPU / 无 GPU）

> **Historical V1 test report. Not current v0.3.6 deployment guidance.**
> 本文仅保存 v0.3.5/V1 当时的测试证据，其中命令、容器、socket 与脚本名称可能已退役；
> 当前用户必须使用 `docker/README*.md` 和 `features/stress/STRESS_USER_GUIDE.md`。

> **项目**: CATMonitor (Computing Availability Tools Monitor) — CATHelper 底座
> **测试对象**: 本地 `main` 分支 @ `243082c`（合并 `origin/develop` → main，no-ff，无冲突）
> **合并内容（本次合并新增/变更）**:
> - **`features/stress` 可靠性压测模块（核心新增）**：通过 `catmonitor stress` 显式运行 STREAM/HPL/HPCG/Ascend NPU Burn；普通 health 与 daemon 不自动触发。CLI/Web 共享原子报告、最近 100 次历史与 Linux 跨进程锁；支持单次缩短超时、作业取消、进程组回收及 profile/资产/配置哈希追溯。第一版仅 Linux 单机，Windows 保证构建并返回 `unsupported`。
> - **stress Web 页面**：新增 `/stress/` 独立页面与 `/api/stress/{config,latest,history,runs}`，挂载到 snapshot 只读 Web；**仅 loopback 监听且 `stress.web_enabled=true` 时启用**，Web 默认读取平台主配置，可经 `CATMONITOR_CONFIG` 或 `-config` 覆盖。
> - **stress 管理员工具链**：`scripts/stress/` 提供 CPU benchmark 构建器（STREAM/HPL/HPCG，输出 build manifest）、Ascend NPU Burn 镜像构建器（固定上游源码 + Mulan PSL v2 + 逐文件 SHA256）、固定容器创建器（identity-map `/dev/davinciN`）、部署生成器与统一 `catmonitor-install` 安装器；`third_party/ascend_npu_burn/` 随仓提供固定 revision 源码。
> - **dfee CSV 落盘 + Grafana Dashboard**：`features/dfee/csv_writer.go` 标准 CSV 落盘（按启动时间命名）；`features/dfee/grafana-dashboard.json`（24 面板 6 行）。
> - **健康评估增强**：新增 `features/health/chassis.go`（机箱部件纳入评估）、`network.go`（网络部件纳入评估）、`WEIGHT_SPEC.md`（4 套权重方案）；`cpu_only` scheme 新增 network 权重；**server_type 判定口径统一**（CLI 与 snapshot 同 scope 下一致）。
> - **collectors 改进**：disk 按物理盘聚合空间使用率 + 过滤 LVM 逻辑卷 + 同一分区 bind mount 去重；network 过滤虚拟接口 + rx/tx 合并 + 接口状态文本；npu 用 card 替代 devID + chip_id label；CPU 8 个 jiffies 优先级 Low→Medium。
> - **新增 `internal/source/lspci`**：lspci 设备描述采集（网络物理网卡聚合分组等）。
> - **stragglerout KPI 扩展**：新增 `metrics.yaml`，A3 双芯片 device_id 自算（卡槽定址，掉卡稳定）。
> - **端口统一**：daemon exporter `:9100→:19320`、faultsub `:9101→:19321`、web `:9527→:19322`、dfee `:9528→:19323`（dfee exporter `:9333` 不变）。
> - **配置默认值变更**：`features: [web,dfee]→[web,dfee,health]`；新增 `stress:` 段（默认全 false）；`faultsub.rest_addr: :19321`。
> - **版本号**：`internal/version/version.go` 升至 `0.3.5`（本次合并对应 Release Notes "v0.3.5 — reliability stress"）。
> **测试日期**: 2026-08-25
> **测试人**: opencode
> **目标版本**: CATMonitor v0.3.5

---

## 测试环境

| 项 | 值 |
|----|----|
| OS | Ubuntu 26.04 LTS (WSL2, kernel 6.18.33.2-microsoft-standard-WSL2) |
| 架构 | linux/amd64 |
| Go | go1.23.4 |
| 硬件 | Intel Core i5-7200U (4 核)；**无 NPU / 无 GPU / 无 IPMI**（验证无硬件采集器优雅降级） |
| CANN DCMI | 头文件 `/usr/local/Ascend/driver/include/dcmi_interface_api.h` 不存在 → `-tags dcmi` 自动关闭（符合预期） |
| 工具链 | `make` 未随系统安装，已下载 `.deb` 解压到 `/tmp/opencode/make-prefix` 并加入 `PATH`（GNU Make 4.4.1）；`gcc` 未安装（`CGO_ENABLED=0`，`-race` 跳过） |
| 外部命令 | `smartctl`/`nvidia-smi`/`npu-smi`/`ipmitool`/`dmidecode` 均未安装（验证 static_info / SMART 降级路径） |
| 测试配置 B (全 opt-in) | `features:[web,dfee,health]`，`min_priority=medium`，snapshot/straggler/faultsub = true，端口 19320-19323，dir 指向 `/tmp/cm-test/{data,snapshot,straggler}` |

---

## 硬门禁结果：**通过 ✅**

---

## 1. 构建与静态检查

### 1.1 go vet / go test
```
$ go vet ./...                          → exit 0（零告警）
$ go test ./...                         → 全部包 ok，无失败
   32 个包 ok + 9 个 [no test files] = 41 个包
   ok  features/{dfee,exporter,faultsub,health,snapshot,stragglerout,web}
   ok  features/stress/{stress,cli,runnerapi}          ← 新增（stress 包 10.944s）
   ok  internal/metrics   ok  internal/config         ← config 新增 stress_test.go
   ok  internal/collectors/{chassis,cpu,disk,gpu,memory,network,npu}
   ok  internal/source/{dcmi,dmesg,dmidecode,hccn_tool,ipmi,lscpu,mce,
        npu_smi,nvidia_smi,proc,smartctl,statfs,sys}
   [no test] cmd/catmonitor, features/stress/cmd/{cpu-runner,cpu-runner-client},
        internal/{collector,platform,source,source/lspci,storage,version}
```

### 1.2 三二进制独立构建（对应 `make all/web/dfee`）
```
$ go build -o bin/catmonitor      ./cmd/catmonitor   → 成功 (10.2 MB)
$ go build -o bin/catmonitor-web ./features/web      → 成功 (9.5 MB)
$ go build -o bin/catmonitor-dfee ./features/dfee    → 成功 (8.6 MB)
```
DCMI tag 自动探测：未检测到 CANN 头 → 不加 `-tags dcmi`（无 NPU 硬件，符合预期）。

### 1.3 已知限制
- `go test -race` 需 cgo（`CGO_ENABLED=1` + `gcc`），本环境无 `gcc`，竞态检测跳过。需在装了 gcc 的环境补测。

---

## 2. CLI 子命令

### 2.1 version
```
$ ./bin/catmonitor version
CATMonitor v0.3.5 (Go 1.23+)
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
npu      npu       High      3s        true
```
7 个采集器全部 enabled。无 NPU/GPU 硬件下 `npu`/`gpu` 仍注册并优雅降级（不崩溃）。

### 2.3 stress（新增 CLI 子命令）
```
$ ./bin/catmonitor stress --help
Usage:
  catmonitor stress [--bench hpl,hpcg,stream,npu_burn] [-c config.yaml] [-o json|table]
  catmonitor stress doctor [-c config.yaml] [-o json|table]
  -b/--bench  逗号分隔 benchmark 名
  -c/--config 配置文件路径
  -o/--output json(默认) 或 table
```
```
$ ./bin/catmonitor stress doctor -config <test.yaml>   (exit 0)
{
  "status": "fail",
  "feature_enabled": false,          ← stress 默认关闭（符合设计）
  "web_enabled": false,
  "script_path": "/opt/catmonitor/stress/benchmark_check.sh",
  "benchmarks": [
    {"name":"hpcg","enabled":false,"status":"unsupported","message":"stress testing is disabled"},
    {"name":"hpl", ...}, {"name":"stream", ...}, {"name":"npu_burn", ...}
  ]
}
```
stress CLI/help/doctor 装配正常；`doctor` 在 feature 关闭时返回 `feature_enabled:false` 而非崩溃。真实 benchmark 执行需 `benchmark_check.sh` + 真机，由 hermetic 脚本测试与 e2e 覆盖（见 §9）。

### 2.4 health（健康检查，配置 B `[web,dfee,health]` scope）
```
$ ./bin/catmonitor health -config <test.yaml>
  Overall Score:  [███████████████████████████░░░]  90 / 100   [ Excellent ]
  Server Type:    cpu_only
    CPU                25 / 25       OK           -
    MEMORY             25 / 25       OK           -
    DISK               30 / 30       OK           -
    NETWORK            10 / 10       OK           -      ← develop 新增：network 纳入 cpu_only scheme
    TOTAL              90 / 100      Excellent
```
**关键改进验证**：
- **server_type 判定一致性已消除**：`catmonitor health` CLI 判 `cpu_only`，snapshot global writer 同 scope 下也判 `cpu_only/90`（见 §4.2）。v0.3.3/v0.3.4 报告的「CLI 判 accelerated、web 判 cpu_only」不一致问题在 v0.3.5**已消除**——根因是 develop 将 server_type 判定改为依赖真实 NPU 指标而非采集器注册存在性，无 NPU 硬件时一致判 `cpu_only`。
- **DISK 不再因无 smartctl 扣分**：develop 将 disk 健康评估改为「按物理盘聚合空间使用率」，无 SMART 数据时不判 `smart_failed`（旧版 `[web,dfee]` scope 下 DISK 7/10 smart_failed -3，现 30/30 OK）。

---

## 3. daemon + Prometheus exporter（配置 B：全 opt-in 开启）

### 3.1 启动日志
```
level=INFO msg="derived per-component cadence from features" features=[web dfee health] declared_components=7
level=INFO msg="straggler_output enabled" data_dir=/tmp/cm-test/straggler
level=INFO msg="faultsub enabled" rest_addr=:19321
level=INFO msg="snapshot production enabled" dir=/tmp/cm-test/snapshot refresh=1s
level=INFO msg="starting collector" name=cpu interval=1s        ← ComponentIntervals 派生生效
level=INFO msg="starting collector" name=memory interval=2s
level=INFO msg="starting collector" name=network interval=1s
level=INFO msg="starting collector" name=npu interval=1s
level=INFO msg="starting collector" name=chassis interval=3s
level=INFO msg="starting collector" name=gpu interval=3s
level=INFO msg="starting collector" name=disk interval=2s
level=INFO msg="faultsub REST listening" addr=:19321
level=INFO msg="CATMonitor daemon started" version=0.3.5
level=INFO msg="exporter listening" addr=:19320
level=INFO msg="hardware specs distributed to snapshot writers" count=6
```
daemon 全程 `grep -iE "error|warn|fatal|panic"` = **空**。`features=[web,dfee,health]` 派生 per-component cadence 生效；`ComponentIntervals` 门禁生效。

### 3.2 exporter :19320/metrics（Prometheus 格式）
```
$ curl -s http://localhost:19320/metrics   (229 行, 40 TYPE, 149 指标行)
# HELP catmonitor_cpu_load_average cpu/load_average
# TYPE catmonitor_cpu_load_average gauge
catmonitor_cpu_load_average{interval="1m"} 2.63
...
$ curl -s -o /dev/null http://localhost:19320/   → HTTP 404（exporter 仅暴露 /metrics，符合设计）
```
标准 Prometheus 文本格式。指标行数 174→149（develop 的采集器过滤生效：disk LVM/重复分区去重、network 虚拟接口过滤、rx/tx 合并），属预期收窄而非回归。

---

## 4. snapshot 统一生产（配置 B）

### 4.1 snapshot 文件
```
/tmp/cm-test/snapshot/
  snapshot.json          (2.1 KB, 全局: health/collectors/intervals/system_specs)
  snapshot_cpu.json      (19.6 KB)
  snapshot_disk.json     (15.6 KB)
  snapshot_memory.json   (7.7 KB)
  snapshot_network.json (5.2 KB)
```
per-comp + global snapshot 均生成；`chassis/gpu/npu` 因无硬件产出 0 指标，未产 per-comp 文件（符合「空组件跳过」设计），但 global `snapshot.json` 含其 collector info + intervals。`refresh=1s` 原子写（temp + rename）。

### 4.2 global snapshot.json 结构验证
```json
{
  "session_id": "...",
  "refresh_interval_ms": 1000,
  "health": {"score":90,"grade":"Excellent","server_type":"cpu_only",
             "components":{"cpu":{...},"disk":{...},"memory":{...},"network":{"score":10,"max":10}}},
  ...
}
```
global health 与 `catmonitor health` CLI（同 `[web,dfee,health]` scope）判定一致：`cpu_only / 90`。

### 4.3 straggler_output
`features/stragglerout` 装配成功（日志 `straggler_output enabled`）；本环境无 NPU KPI 指标，`/tmp/cm-test/straggler/` 未产 KPI 文件（`flush_interval=60s` + 无数据，符合设计——完整验证需 NPU 真机）。

---

## 5. web 只读消费者（:19322）

```
$ ./bin/catmonitor-web -addr :19322 -snapshot-dir /tmp/cm-test/snapshot
level=INFO msg="web server starting (snapshot read-only consumer)" addr=:19322 snapshot_dir=/tmp/cm-test/snapshot
```

| 端点 | 方法 | HTTP | Content-Type | 说明 |
|------|------|------|--------------|------|
| `/` | GET | 200 | text/html; charset=utf-8 | 首页 SPA（1.4 KB） |
| `/api/snapshot` | GET | 200 | application/json | 组装 global+per-comp snapshot（28 KB） |
| `/stress/` | GET | 200 | text/html; charset=utf-8 | **新增** stress 压测 SPA（8.7 KB，title "CATMonitor 可靠性压测"） |

`/api/snapshot` 响应结构完整：
```
keys: session_id, timestamp, refresh_interval_ms, history_points, health, metrics, history, specs
metrics: 149 条
health:  90 / Excellent / cpu_only
specs:   15 条
refresh_interval_ms: 1000
```
web 只读消费 daemon snapshot 链路打通；无自采集。

> **stress Web 前置条件**：web 启动时读取平台 CATMonitor 主配置；若 `/etc/catmonitor/catmonitor.yaml` 不存在则打印 WARN `default CATMonitor config is absent; stress feature remains disabled`（非错误，stress 默认关闭，符合设计）。要启用 stress Web API 须 `-config` 指向含 `stress.web_enabled:true` 的配置且监听 loopback（见 §6.2）。

### 5.1 stress Web API（loopback + web_enabled=true）

用 `-config <stress-enabled.yaml>` + `-addr 127.0.0.1:19325` 重启 web 验证（stress 要求 loopback 监听）：

| 端点 | 方法 | HTTP | 响应 | 说明 |
|------|------|------|------|------|
| `/api/stress/config` | GET | 200 | benchmark 清单（全 disabled） | 路由在 web_enabled=true 时挂载 |
| `/api/stress/history` | GET | 200 | `[]`（空，无历史 run） | 最近 100 次历史 |
| `/api/stress/latest` | GET | 404 | `{"error":"no stress report"}` | 无报告（未跑过） |
| `/api/stress/runs` | GET | 405 | `{"error":"method not allowed"}` | 需 POST 启动 run |

stress Web 路由 `/api/stress/{config,latest,history,runs,runs/}` 在 loopback + web_enabled 时正确挂载；非 loopback 或 web_enabled=false 时 run 端点返回 403/不挂载（安全设计，符合 `handler.go:243` 校验）。

---

## 6. dfee 独立二进制（:19323 + :9333 exporter）

```
$ ./bin/catmonitor-dfee -addr :19323 -snapshot-dir /tmp/cm-test/snapshot -exporter enabled -exporter-port 9333
level=INFO msg="dfee server starting (read-only consumer)" addr=:19323 snapshot_dir=/tmp/cm-test/snapshot
level=INFO msg="collecting static info for exporter..."
level=INFO msg="exporter starting" port=9333
```

### 6.1 dfee SPA + API（:19323）

| 端点 | HTTP | Content-Type | 说明 |
|------|------|--------------|------|
| `/` | 200 | text/html; charset=utf-8 | SPA 首页（catch-all） |
| `/api/dfee` | 200 | application/json | dfee 派生指标（25 charts） |

`/api/dfee` 返回 25 个图表；无 NPU 数据的图表 `series: null`——优雅降级，结构完整。

### 6.2 dfee 内置 Prometheus exporter（:9333/metrics）

```
$ curl -s http://localhost:9333/metrics   (135 行)
```
| 指标族 | 行数 | 说明 |
|--------|------|------|
| `node_*` | 133 | CPU/内存/网络/磁盘，对齐 node_exporter 命名（`node_cpu_seconds_total`/`node_cpu_cores_online`/`node_network_receive_bytes_total`/`node_disk_read_sectors_total`/`node_disk_read_time_seconds_total` 等） |
| `static_*` | 2 | `static_hardware_info` + `static_software_info`（启动一次性采集） |
| `dsmi_*` | 0 | 无 NPU 硬件 → 空输出（优雅降级） |
| `ipmi_*` | 0 | 无 IPMI 硬件 → 空输出（优雅降级） |

静态信息优雅降级验证（无工具时对应字段为空，不报错）：
```
static_hardware_info{cpu_info="1*Intel(R) Core(TM) i5-7200U CPU @ 2.50GHz",
  disk_info="sda 356.9M, sdb 159.4M, sdc 1G, sdd 1T",
  gpu_type="",memory_info="",npu_chip_name="",product_name="",psu_info=""} 1
static_software_info{os_version="Ubuntu 26.04 LTS",python_version="3.14.4",
  cann_version="",cuda_version="",npu_driver_version="",gpu_driver_version="",
  mindie_version="",mindspeed_version="",torch_npu_version="",verl_npu_version="",
  vllm_ascend_version="",vllm_version="",...} 1
```
`cpu_info`/`disk_info`/`os_version`/`python_version` 已采集；`npu_*`/`gpu_*`/`memory_info`/`product_name`/`psu_info` 因无对应硬件/命令为空（降级而非报错，符合 `static_info.go` 设计）。

---

## 7. faultsub 故障订阅（:19321，配置 B 开启）

| 操作 | 端点 | HTTP | 响应 |
|------|------|------|------|
| 查询事件 | `GET /faultsub/events` | 200 | `[]`（无硬件→无故障事件） |
| 查询订阅 | `GET /faultsub/subscriptions` | 200 | `[]` |
| 创建订阅 | `POST /faultsub/subscriptions` | **201** | `{"id":"sub-0001","delivery":"webhook","endpoint":"http://localhost:9999/hook","created_at":"..."}`（id 自动生成） |
| 创建订阅(错误字段) | `POST` 用 `url` 而非 `endpoint` | 400 | `{"error":"webhook delivery requires 'endpoint' URL"}`（字段校验生效） |
| 删除订阅 | `DELETE /faultsub/subscriptions/sub-0001` | 204 | （成功删除） |

faultsub REST API（订阅 CRUD + 事件查询）端到端验证通过；`detector`/`dispatcher`/`webhook`/`subscription` 装配正常，无硬件下静默不产事件（符合设计）。

---

## 8. 无硬件采集器优雅降级验证

| 采集器 | 硬件 | 行为 |
|--------|------|------|
| npu | 无（无 CANN DCMI 头，未加 `-tags dcmi`） | collector 正常启动不崩溃；无 per-comp snapshot 文件，global snapshot 含 npu collector info |
| gpu | 无（无 nvidia-smi） | collector 正常启动不崩溃；无指标输出；`memory_detail` 已注册但无硬件不采集 |
| chassis | 无（无 ipmi/dmidecode 权限） | 未产 per-comp snapshot，但 global snapshot 含 chassis collector info |
| cpu/memory/disk/network | 有 | 全量采集正常；disk 按物理盘聚合 + LVM 过滤；network 虚拟接口过滤 + rx/tx 合并 |

daemon 全程日志 `grep -i "error\|warn\|fatal\|panic"` = **空**。**优雅降级符合设计预期**。

---

## 9. stress 模块 hermetic 自动化测试（新增）

stress 有三层自动化测试（package-local Go 单测 / hermetic build/deployment fixtures / Linux binary-level CLI/Web e2e）。真实 benchmark 性能与 NPU 负载执行仍为显式硬件验收门禁。

### 9.1 Go 单元/组件测试（已含于 §1.1 `go test ./...`）
```
ok  features/stress              10.944s   ← manager/profile/parse/handler/runnerapi
ok  features/stress/cli           0.043s
ok  features/stress/runnerapi     0.083s
ok  internal/config              0.015s    ← 新增 stress_test.go
```

### 9.2 hermetic 脚本测试（11 项，对应 `make test-stress-build`）
环境补装 `make`（GNU Make 4.4.1）后全部通过：

| 脚本 | 结果 | 验证内容 |
|------|------|----------|
| `audit_stress_release_test.sh` | ✅ PASS | manifest schema 兼容性审计 |
| `ascend_env_test.sh` | ✅ PASS | CANN 环境 source 校验 |
| `runtime_preflight_test.sh` | ✅ PASS | NPU Burn 固定容器运行时预检 |
| `create_npu_burn_container_test.sh` | ✅ PASS | 固定容器创建 + identity-map |
| `build_npu_burn_image_test.sh` | ✅ PASS | NPU Burn 镜像构建 + manifest |
| `build_cpu_benchmarks_test.sh` | ✅ PASS | STREAM/HPL/HPCG 构建 + HPL 并行安装竞态 fixture |
| `build_cpu_runner_image_test.sh` | ✅ PASS | CPU runner 镜像构建上下文 + manifest |
| `generate_stress_deployment_test.sh` | ✅ PASS | 部署生成器完整 fixture |
| `install_stress_runtime_test.sh` | ✅ PASS | stress 插件安装布局 + 替换边界 |
| `container_deployment_test.sh` | ✅ PASS | 容器部署定义 + stress opt-in 边界 |
| `catmonitor_install_test.sh` | ✅ PASS | 统一安装器 profile/safety/no-workload 契约 |

`catmonitor-install` 打包逻辑额外用等价 `install` 命令手动验证：`/usr/local/sbin/catmonitor-install` 可执行 + 5 个 compose 定义文件落位正常。

> **`make` 依赖说明**：`catmonitor_install_test.sh` 与 `build_cpu_benchmarks_test.sh` 内部调用 `make`（前者 `make install-installer`，后者 `make -C ... arch=RaceFixture` 复现 HPL 并行安装竞态）。本环境无系统 `make`，已用 `apt-get download make` + `dpkg-deb -x` 解压到本地 prefix 并加入 `PATH`，两项随即通过——确认失败纯属环境缺 `make`，非代码回归。

---

## 10. 测试结论

| 门禁 | 结果 |
|------|------|
| `go vet` / `go test`（32 包 ok） | ✅ 全绿（含新增 stress 三包 10.9s） |
| 三二进制构建（daemon/web/dfee） | ✅ 全部成功（10.2/9.5/8.6 MB） |
| CLI（version/list/health/**stress**） | ✅ 正常（stress 为新增子命令） |
| feature-scoped 收集（`SetFeatureScope`+`LoadFeatureOverrides`+`ComponentIntervals`） | ✅ 指标集收窄 + cadence 覆盖生效 |
| daemon + exporter（:19320/metrics，229 行/40 TYPE） | ✅ 正常（GET / → 404） |
| snapshot 统一生产（daemon 产 → web/dfee 读） | ✅ 端到端打通（5 文件 + global） |
| web 只读消费（:19322，`/api/snapshot` 149 metrics/15 specs） | ✅ 正常 |
| **web `/stress/` SPA + `/api/stress/*`（loopback）** | ✅ **新增，路由挂载 + config/history 200、latest 404、runs 405 符合预期** |
| dfee 独立二进制（:19323，25 charts） | ✅ 正常 |
| dfee exporter（:9333，135 行 node_/static_） | ✅ 正常 |
| static_info 优雅降级（无 ipmitool/dmidecode） | ✅ 字段为空不报错 |
| faultsub 订阅 API（:19321，CRUD 200/201/400/204） | ✅ 正常 |
| **server_type 判定一致性（CLI vs snapshot，同 scope）** | ✅ **一致（cpu_only/90），v0.3.5 已知限制消除** |
| 无硬件采集器优雅降级 | ✅ 无 error/warn/panic |
| **stress hermetic 脚本测试（11 项）** | ✅ **新增，全 PASS（补装 make 后）** |

**整体：通过，可进入发布流程。**

### 已知限制与发现（非阻塞，建议后续处理）
1. **`-c` 短 flag 仍为死代码**（继承 v0.3.3）：`cmd/catmonitor/main.go` 注册了 `c` 短 flag 但其值被丢弃，只使用长 `config` flag。**临时绕过**：一律使用 `-config <path>` 长形式。
2. **`-race` 需 cgo + gcc**，本环境无 `gcc`，竞态检测未覆盖。
3. **二进制制品入仓**：合并带入两个二进制 `dfee`(8.6MB) + `web`(8.6MB)（仓库根目录），违反常规 Git 规范，显著增大仓库体积。建议后续从 main 移除并加入 `.gitignore`，改走 release 制品。
4. **无 NPU/GPU/IPMI 真机环境**：`error_codes`/`card_drop`/straggler KPI/dfee exporter 的 `dsmi_*`/`ipmi_*` 输出仅验证「不崩溃 + 优雅降级」，stress 真实 benchmark 执行（STREAM/HPL/HPCG/NPU Burn）需在昇腾/CPU 真机补测。
5. **DCMI CGo 未真机验证**：`dcmi_cgo.go` 在 `-tags dcmi` 后，本机无 CANN SDK 无法编译。
6. **dfee static_info 命令依赖**：`static_hardware_info`/`static_software_info` 依赖 `ipmitool`/`dmidecode`/`npu-smi`/`nvidia-smi`/`pip`/`nvcc` 等，缺失时对应 label 为空（降级而非报错，符合设计）。
7. **stress Web API 需 loopback + web_enabled**：非 loopback 监听或 `stress.web_enabled=false` 时 run 端点不挂载/返回 403（安全设计，符合 `handler.go:243`）。
8. **未推送到远端**：合并提交 `243082c` 暂在本地完成。
