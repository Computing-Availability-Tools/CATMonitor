# CATMonitor Release Notes

> 本文档按时间倒序记录每次发布的版本信息。每次发布在顶部追加，不删除历史记录。

---

## v0.3.6（候选，未发布）

### Stress Architecture V2

- daemon 成为唯一 Stress Controller；CLI/Web 统一通过本机 Unix HTTP/JSON API；
- CPU 与 NPU workload 镜像统一实现 `catmonitor-stress-exec` 协议；
- CPU 不再使用 Unix Runner server/client，NPU 不新增 Unix Runner；
- 一个 Web 进程只监听 `:19322`，统一提供监控、Stress 查询、Run 与 Cancel；
- Web/DFeE 不再获得 Docker Socket；V2 暂时仅 daemon 获得该高权限 socket；
- CPU/NPU 结果在 workload 容器内归一化，HPCG 拒绝历史结果文件；
- canonical Compose 使用 `stress-cpu`、`stress-npu` profiles，NPU device override 由 generator 生成；
- 保留 A2 已有 workload 证据作为回归基线；发布前必须重新完成 V2 自动化与 A2 实机闭环；
- 当前候选不创建 Git tag、不发布 GHCR 镜像。

---

## v0.3.5

> 以下 Stress 内容是 v0.3.5 发布时的历史 V1 记录，不是 v0.3.6 当前部署指导。
> 当前架构与命令以顶部 v0.3.6 候选说明及 `features/stress/` 文档为准。

| 项目 | 说明 |
|------|------|
| 版本号 | v0.3.5 |
| 发布时间 | 2026-08-25 |
| 发布人 | sunnytao |
| 平台支持 | Linux (x86_64), Windows (x86_64) |
| 合并来源 | origin/develop → main（merge `243082c`，no-ff，无冲突） |

### 变更摘要

- **可靠性压测模块 `features/stress`（核心新增）**：通过 `catmonitor stress` 显式运行 STREAM / HPL / HPCG / Ascend NPU Burn；普通 health 和 daemon 不自动触发。CLI/Web 共享原子报告、最近 100 次历史和 Linux 跨进程锁；支持单次缩短超时、作业取消、进程组回收及 profile、资产和配置哈希追溯。第一版只支持 Linux 单机执行；Windows 保证构建并返回 `unsupported`，暂不支持 OSU 和多节点 MPI。
- **Stress V2 Web**：`/stress/` 与 `/api/stress/{config,latest,history,runs}` 统一挂载到 `:19322`；GET/Run/Cancel 均通过 `/run/catmonitor/control.sock` 调用 daemon Controller。Web 不挂 Docker Socket，也不提供脚本、路径或任意参数编辑；operator authentication/RBAC 作为后续安全债务。
- **stress 管理员工具链**：`scripts/stress/` 提供 CPU benchmark 构建器（STREAM/HPL/HPCG，从任意位置构建、显式选择 GCC/MPI/OpenBLAS、精确应用 HPCG OpenMP 兼容补丁、输出含工具链与资产哈希的 build manifest）、Ascend NPU Burn 镜像构建器（固定上游源码 + Mulan PSL v2 + 逐文件 SHA256、显式 source CANN 环境、HAL/torch/torch_npu/TBE 预检、离线强制重装 wheel、pciutils/lspci 依赖闭包）、固定容器创建器（动态 identity-map 全部 `/dev/davinciN`、`unless-stopped` 策略、交叉检查容器设备节点与 upstream `lspci` logical topology）、部署生成器与统一 `catmonitor-install` 安装器；`third_party/ascend_npu_burn/` 随仓提供固定 revision 源码。构建、节点适配和运行保持分离。
- **dfee CSV 落盘 + Grafana Dashboard**：`features/dfee/csv_writer.go` 标准 CSV 落盘（按启动时间命名、value 格式规则）；`features/dfee/grafana-dashboard.json`（24 面板 6 行）。
- **健康评估增强**：新增 `features/health/chassis.go`（机箱部件纳入评估：进/出风口温度）、`network.go`（网络部件纳入评估）、`WEIGHT_SPEC.md`（4 套权重方案）；`cpu_only` scheme 新增 network 权重；disk 健康评估改为「按物理盘聚合空间使用率」，无 SMART 数据时不判 `smart_failed`。
- **server_type 判定一致性修复**：CLI 与 snapshot global writer 在同 scope 下判定一致（消除 v0.3.3/v0.3.4「CLI 判 accelerated、web 判 cpu_only」不一致）；根因改为依赖真实 NPU 指标而非采集器注册存在性，无 NPU 硬件时一致判 `cpu_only`。
- **collectors 改进**：disk 按物理盘聚合空间使用率 + 过滤 LVM 逻辑卷（`dm-`/mapper）+ 同一分区 bind mount 去重；network 过滤虚拟接口（docker0 等）+ rx/tx 合并 + 接口状态显示 up/down 文本；npu 用 card 替代 devID + 全部 DCMI 指标加 `chip_id` label + `dcmi_get_device_resource_info` 采集进程信息（`process_info`/`process_total`）；CPU 8 个 jiffies 优先级 Low→Medium。
- **新增 `internal/source/lspci`**：lspci 设备描述采集（网络物理网卡聚合分组等，91 行）。
- **stragglerout KPI 扩展**：新增 `metrics.yaml`，A3 双芯片 device_id 自算（卡槽定址，掉卡稳定）+ `hbm_bandwidth_util`/`roce` 别名兼容。
- **端口统一**：daemon exporter `:9100→:19320`、faultsub `:9101→:19321`、web `:9527→:19322`、dfee `:9528→:19323`（dfee exporter `:9333` 不变）。
- **配置默认值变更**：`features: [web,dfee]→[web,dfee,health]`；新增 `stress:` 段（默认全 false）；`faultsub.rest_addr: :19321`。
- **版本号**：`internal/version/version.go` 升至 `0.3.5`；指标总数 210→216（CPU 40→39 删 `die_core_num`；Memory 19→20 新增 `swap_detail`；Disk 13→14 新增 `space_detail`；NPU 120→123 新增 `process_info`/`process_total`/`npu_util`；Network 5→7 新增 `rx_bytes_total`/`tx_bytes_total`；High 26 / Medium 125→143 / Low 59→47）。
- **测试**：`go vet ./...` 零告警；三二进制构建全成功（catmonitor 10.2MB / web 9.5MB / dfee 8.6MB）；`go test ./...` 41 包全绿（含新增 stress 三包 10.9s）；stress hermetic 脚本测试 11 项全 PASS；端到端 5 端点验证通过（`:19320`/`:19321`/`:19322`/`:19323`/`:9333`）；daemon 日志无 error/warn/panic；CLI 与 snapshot 健康度一致（`cpu_only/90`）。

### 已知限制

1. **二进制制品入仓**：合并带入两个二进制 `dfee`(8.6MB) + `web`(8.6MB)（仓库根目录），违反常规 Git 规范，显著增大仓库体积。建议后续从 main 移除并加入 `.gitignore`，改走 release 制品。
2. **`-c` 短 flag 仍为死代码**（继承 v0.3.3）：`cmd/catmonitor` 的 `c` 短 flag 值被丢弃，须用 `-config` 长形式（stress CLI 已支持 `-c`）。
3. **DCMI CGo 未真机验证**：`dcmi_cgo.go` 在 `-tags dcmi` 后，本机无 CANN SDK 无法编译，需在真 NPU 服务器验证。
4. **GPU/NPU/Chassis 无真机**：系统测试仅验证优雅降级路径（空数据 / 计数 0 / 不崩溃），stress 真实 benchmark 执行（STREAM/HPL/HPCG/NPU Burn）与 dfee exporter 的 `dsmi_*`/`ipmi_*` 输出需在配备对应硬件的机器复测。
5. **dfee static_info 命令依赖**：`static_hardware_info`/`static_software_info` 依赖 `ipmitool`/`dmidecode`/`npu-smi`/`nvidia-smi`/`pip`/`nvcc` 等命令，容器环境需 `--privileged` + 安装对应工具，缺失时对应 label 为空（降级而非报错）。
6. **stress Web API 需 loopback + web_enabled**：非 loopback 监听或 `stress.web_enabled=false` 时 run 端点不挂载/返回 403（安全设计）。
7. **`-race` 需 cgo + gcc**：无 `gcc` 环境竞态检测未覆盖。

---

## v0.3.4

| 项目 | 说明 |
|------|------|
| 版本号 | v0.3.4 |
| 发布时间 | 2026-08-10 |
| 发布人 | sunnytao |
| 平台支持 | Linux (x86_64), Windows (x86_64) |
| 合并来源 | feature/wyx/add-metrics (b0d0bf1) → main（合并 feature/wyx/add-metrics 全部后续提交） |

### 变更摘要

- **dfee Prometheus exporter（核心特性）**：`features/dfee` 新增独立 Prometheus exporter（`-exporter=enabled` 启动 `:9333/metrics`），将 snapshot 映射为 `node_*`（CPU/内存/网络/磁盘，对齐 node_exporter 命名）/ `dsmi_*`（NPU，对齐 dsmi 命名）/ `ipmi_*`（机箱）/ `static_hardware_info`+`static_software_info`（启动时一次性采集的硬件/软件身份）格式；`supplementDiskStats` 直读 `/proc/diskstats` 补 snapshot 未覆盖设备；零外部 prometheus 库依赖（自实现文本 exposition + label 转义）
- **静态软硬件信息采集**：`features/dfee/static_info.go` 启动时经 `ipmitool`/`lscpu`/`dmidecode`/`lsblk`/`npu-smi`/`nvidia-smi`/`nvcc`/`pip` 等命令采集硬件型号与软件版本（OS/NPU 驱动/CANN/Python/PyTorch/vLLM 等），无对应工具或权限时优雅降级为空；支持 `-docker-container` 经 `docker exec` 在容器内采集软件版本
- **`LoadFeatureOverrides` higher-priority-wins 合并**：`internal/metrics` 新增 `LoadFeatureOverrides(paths)`，一次性加载全部 feature `metrics.yaml`，同名指标取高优先级、字段后写覆盖，替代 `cmd/catmonitor` 中逐个 `LoadModuleOverride` 循环
- **Disk 新增 4 项累计 raw counters**：`internal/collectors/disk/disk_linux.go` 新增 `collectRawCounters`，从 `/proc/diskstats` 输出 `read_sectors_total`/`written_sectors_total`/`read_time_total`/`write_time_total`（Medium，累计计数器，不差分）；`configs/metrics.yaml` 补登 `read_latency`/`write_latency` + 上述 4 项
- **GPU 新增 `memory_detail`**：`configs/metrics.yaml` 新增 `memory_detail`（显存明细，Medium，按 field=total/used 输出 MB）
- **容器化方案**：新增 `docker/` 目录——`Dockerfile.npu`（Debian/glibc 两步构建：golang 容器挂载 driver 编译 + debian 运行时打包，链接 `libdcmi.so`）+ `Dockerfile.generic`（alpine 多阶段，纯 Go）+ `build.sh`（自动检测 driver）+ `docker-compose.yml`（daemon+web+dfee 三服务编排）+ `docker/catmonitor.yaml` + `README.md`（406 行完整文档）
- **bug 修复**：① faultsub `FaultStorage.Ready()` 改用 `written` 标志（健康 NPU 无故障时 snapshot 为空但已采集，`/-/ready` 不再误报 503）；② NPU `power_draw` 单位修正（DCMI 返回 0.1W，`/10.0` 转 W，测试用例同步 65→6.5W）；③ IPMI `cacheDir` 由相对路径 `features/web/data` 改绝对路径 `/var/lib/catmonitor`，消除工作目录依赖
- **配置默认值变更**：`configs/catmonitor.yaml` 默认 `collection.min_priority: low→medium`、`features: [dfee]→[web, dfee]`、`snapshot.enabled: false→true`；`.gitignore` `loc_configs/→local_configs/` + 新增 `**/ipmi_sensor_map.json`
- **文档**：README/SPEC/DESIGN/User_Manual/indi_list 5 文档全面同步合并后代码（+237/-59 行）：指标总数 205→210、dfee exporter :9333、容器化、disk raw counters、LoadFeatureOverrides higher-wins、bug 修复说明、配置默认值、版本演进表加 v0.3.3 后续条目
- **版本号**：`cmd/catmonitor` version 升至 `0.3.4`；指标总数 205→210（Disk +4 / GPU +1）
- **测试**：`go vet ./...` 零告警；三二进制构建全成功（catmonitor 10.4MB / web 8.5MB / dfee 8.6MB）；`go test ./...` 27 包全 ok（含新增 dfee exporter/faultsub healthy ready/npu power_draw 单位测试）；端到端 5 端点验证通过（`:9100/metrics` 239 行 41 TYPE、`:9101/faultsub` CRUD 200/201/400、`:9527` web 200、`:9528` dfee 200、`:9333/metrics` 135 行 `node_*`/`static_*` 输出正常）；daemon 日志无 error/warn/panic

### 已知限制

1. **DCMI CGo 未真机验证**：`dcmi_cgo.go` 在 `-tags dcmi` 后，本机无 CANN SDK 无法编译，需在真 NPU 服务器验证
2. **GPU/NPU/Chassis 无真机**：系统测试仅验证优雅降级路径（空数据 / 计数 0 / 不崩溃），真实指标采集与 dfee exporter 的 `dsmi_*`/`ipmi_*` 输出需在配备对应硬件的机器复测
3. **dfee static_info 命令依赖**：`static_hardware_info`/`static_software_info` 依赖 `ipmitool`/`dmidecode`/`npu-smi`/`nvidia-smi`/`pip`/`nvcc` 等命令，容器环境需 `--privileged` + 安装对应工具，缺失时对应 label 为空（降级而非报错）
4. **NPU 镜像须 Debian/glibc**：`libdcmi.so` 为 glibc 链接，alpine（musl libc）不兼容；`docker-compose.yml` 默认按 NPU 环境配置，非 NPU 环境需改 `Dockerfile.generic` 并删除 driver/nnae 挂载
5. **`-c` 短 flag 死代码**（继承 v0.3.3）：`cmd/catmonitor` 的 `c` 短 flag 值被丢弃，须用 `-config` 长形式
6. **server_type 判定口径不一致**（继承 v0.3.3）：`catmonitor health` CLI 判 `accelerated`、web/dfee 端判 `cpu_only`，建议后续统一

---

## v0.3.3

| 项目 | 说明 |
|------|------|
| 版本号 | v0.3.3 |
| 发布时间 | 2026-07-28 |
| 发布人 | sunnytao |
| 平台支持 | Linux (x86_64), Windows (x86_64) |
| 合并来源 | feature/wyx/add-metrics (70865d7) → main (merge e1c14c4，no-ff，无冲突) + 热修 b2181e6 |

### 变更摘要

- **采集粒度控制（核心特性）**：新增 `collection.min_priority` 配置（low/medium/high）按优先级阈值预过滤采集；`internal/metrics` 暴露 `SetCollectionThreshold`/`AnyWanted`/`IsWanted`（优先级值大小写不敏感），`internal/collector` 经 `SetWantedChecker` DI 注入；CPU/Memory/Disk/NPU 等采集器在执行昂贵采集阶段前调用 `collector.AnyWanted` 判断指标组是否通过阈值，无则整组跳过，降低无谓开销；daemon 与 `runCollect` 启动时均装配
- **daemon 移除周期健康检查**：`runDaemon` 不再启动 health 评估 goroutine，健康度评估改由 `catmonitor health` 子命令按需执行
- **web 退出清 snapshot**：`features/web/main.go` 收到 SIGINT/SIGTERM 后清理 snapshot 再退出
- **配置**：`configs/catmonitor.yaml` 新增 `collection.min_priority: low`（默认全采）；`.gitignore` 增加 `loc_configs/`（本地测试用 `metrics_low.yaml` 不入库）
- **dfee**：CPU 图表标题 `CPU 利用率分解` → `CPU 利用率`
- **热修**：`internal/collectors/npu/npu_other.go` `collectDevice(devID int, ...)` → `collectDevice(dev npuDevice, ...)`，与 `npu_linux.go` 签名对齐，修复非 linux 平台签名不匹配致 Windows 交叉编译失败（v0.3.2 起 `67ef5f1` 引入 `npuDevice` 时遗留，非本次合并引入）
- **文档**：README/SPEC/DESIGN/User_Manual/indi_list 同步采集粒度控制说明 + 版本号升至 v0.3.3；DESIGN 数据流与架构注释更新（daemon 不再周期评估健康度、§1.7 增预过滤要点）
- **版本号**：`cmd/catmonitor` version 升至 `0.3.3`；指标总数不变（204）
- **测试**：263 用例全过（与 v0.3.2 持平），覆盖率 29.5%~94.3%，`go vet` 零警告，Linux/Windows 双平台编译通过（Windows 交叉编译恢复）；无 NPU/GPU 系统测试通过（`:9100/metrics` 52 TYPE / 173 指标行 + `/-/healthy`·`/-/ready` 200、`:9527` root/dfee/snapshot/collectors 全 200、GPU/NPU/Chassis 优雅降级不崩溃）

### 已知限制

1. **DCMI CGo 未真机验证**：`dcmi_cgo.go` 在 `dcmi` 构建标签后，本机无 CANN SDK 无法编译，需在真 NPU 服务器 `go build -tags dcmi` 验证
2. **GPU/NPU/Chassis 无真机**：系统测试仅验证优雅降级路径（空数据 / 计数 0 / 不崩溃），真实指标采集需在配备对应硬件的机器复测
3. **采集粒度控制仅验证默认 low**：`medium`（跳过 Low）/ `high`（仅 High）的预过滤行为未在系统测试中实跑，且未补对应单元测试（`internal/metrics` 覆盖率由 85.9% 降至 66.3%），建议后续补测
4. **server_type 判定口径不一致**：`catmonitor health` CLI 因 NPU 采集器产出 `npu_num` 判定 `accelerated`，web 端 hwinfo 探测无真实 NPU 判定 `cpu_only`，非功能缺陷，建议后续统一
5. **daemon 短时运行未落盘 JSONL**：`CachingStorage` 内存缓存供 `/metrics` 读取，短时未触发 JSONL 落盘，建议真机长时运行观察
6. **未推送到远端**：合并提交 `e1c14c4` + 热修 `b2181e6` 暂在本地完成

---

## v0.3.2

| 项目 | 说明 |
|------|------|
| 版本号 | v0.3.2 |
| 发布时间 | 2026-07-25 |
| 发布人 | Sunnytao |
| 平台支持 | Linux (x86_64), Windows (x86_64) |
| 合并来源 | feature/wyx/add-metrics (c21a081) → main (merge c824349，no-ff，1 处冲突已解决) |

### 变更摘要

- **Prometheus 导出模块**：新增 `features/exporter`——`CachingStorage` 包装在 `JSONLStorage` 外（实现 `collector.Storage` 接口），一次采集同时落盘 JSONL + 更新内存缓存（按组件分组原子替换），HTTP `/metrics` 端点（`:9100`）从缓存读取转 Prometheus 文本格式（`catmonitor_{component}_{name}` 前缀，`_total`/`_time` 后缀判 counter，含 `# HELP`/`# TYPE`/labels）；daemon 集成仅需 ~5 行；附 `/-/healthy`、`/-/ready` 健康端点
- **NPU 指标扩展 74→119**：新增 45 项 `hccn_tool` 网络统计指标（Medium，网口/PCIe 带宽、RoCE 速度/链路等扩展统计）；指标总数 159→204（High 24 / Medium 121 / Low 59）
- **NPU 采集器 DCMI CGo 修复**：`dcmi_init()` 初始化、`dcmi_get_card_num_list` 返回全部设备 ID、`dcmi_get_device_errorcode_v2` 5 参数签名适配、`dcmi_get_device_info` 指针参数、dvpp struct 名修正；NPU card/device 二级枚举（CardList + DeviceNumInCard 遍历全部设备）；默认布局调整（4 行 3+2+2+2、gridCols 6 列、功耗电压首行、图例简化）
- **IPMI 来源层重构**：`ipmitool sdr`→`sensor` 命令 + 解析器兼容 3/4 段格式 + 定向 `ipmi sensor get` 采集 + 两级缓存（传感器名称 24h / 采集结果 10s）+ 磁盘持久化 + 降级回退 + 超时 5s→30s→60s；进出风口温度精确匹配、风扇转速取平均、整机功耗只匹配 `Power`（排除 PSU 输出）
- **dfee 能效监控增强**：图表卡片拖拽重排 + 右下角手柄缩放（`align-self: start` 边框不跟随增长）+ 虚线对齐辅助（3px 吸附）；NPU/磁盘/网络多选下拉筛选（重构为通用筛选框架，固定宽度 + 截断省略号）；模块折叠（机箱 3 图同行）；NPU 改为单指标图布局
- **main.go 行为修复**：`--help` 解析后 `os.Exit(0)` 退出，不再继续执行采集
- **web 修复**：补充 chassis 采集器 import，修复机箱类指标无数据
- **配置**：新增 `configs/metrics.yaml` 默认指标采集目录
- **文档**：README 精简（使用说明迁移至新增 `docs/User_Manual.md`）；SPEC 改为功能规格（不含技术细节，链接各 feature SPEC）；DESIGN 新增 exporter 章节、更新 NPU/IPMI/dfee；indi_list 版本升至 v0.3.2/204 指标
- **版本号**：`cmd/catmonitor` version 升至 `0.3.2`
- **测试**：263 用例全过（较 v0.3.1 的 241 +22，来自 `features/exporter` + `internal/source/hccn_tool` 扩展用例），覆盖率 29.5%~97.0%，`go vet` 零警告，Linux/Windows 双平台编译通过；无 NPU/GPU 系统测试通过（`:9100/metrics` 导出 33 指标名 / 31 gauge + 2 counter、`:9527` web/dfee 5 端点全 200、GPU/NPU/Chassis 优雅降级不崩溃）

### 已知限制

1. **DCMI CGo 未真机验证**：`dcmi_cgo.go` 在 `dcmi` 构建标签后，本机无 CANN SDK 无法编译，需在真 NPU 服务器 `go build -tags dcmi` 验证 CGo 绑定
2. **GPU/NPU/Chassis 无真机**：系统测试仅验证优雅降级路径（空数据 / 计数 0 / 不崩溃），真实指标采集需在配备对应硬件的机器复测
3. **server_type 判定口径不一致**：`catmonitor health` CLI 因 NPU 采集器产出 `npu_num` 指标判定 `accelerated`，web 端 hwinfo 探测无真实 NPU 硬件判定 `cpu_only`，非功能缺陷，建议后续统一
4. **daemon 短时运行未落盘 JSONL**：`CachingStorage` 在内存缓存指标供 `/metrics` 读取，短时未触发 JSONL 落盘，建议真机长时运行观察落盘周期
5. **dfee_SPEC.md 内部描述待修订**：其头部仍写"25 图/74 指标/优先级筛选"，与合并后实际行为（61 图表定义、拖拽缩放、多选筛选、取消优先级筛选）有出入，待后续修订
6. **未推送到远端**：合并提交 `c824349` 仅在本地完成

---

## v0.3.1

| 项目 | 说明 |
|------|------|
| 版本号 | v0.3.1 |
| 发布时间 | 2026-07-17 |
| 发布人 | sunnytao |
| 平台支持 | Linux (x86_64), Windows (x86_64) |
| 合并来源 | feature/wyx/add-metrics (9868b80) → main (fast-forward) |

### 变更摘要

- **Chassis 机箱环境采集器**：新增第 7 个采集器 `internal/collectors/chassis`（5 指标：整机功耗 / 进出风口温度 / 风扇转速 / 风扇功率，来自 ipmitool SDR，与 CPU/Memory 共享 30s SDR 缓存，Linux 专有）
- **Disk 读/写耗时**：Disk 采集器新增 `read_latency`/`write_latency` 指标（/proc/diskstats field 7/11，ms/s）；`internal/source/proc` DiskStat 加 ReadTime/WriteTime 字段。Disk 指标 7→9
- **dfee 能效监控模块**：新增 `features/dfee` 能效监控模块（25 张实时图表 + CPU 8 jiffies→7 利用率推导 + 网络差值），从 159 项指标中过滤 74 项能效指标，独立 SPA 路由 `/dfee/`；`features/web/server.go` 加 dfee.Register 路由注册，`features/web/static/app.js` 加导航入口
- **dfee metrics 覆盖**：`features/dfee/metrics.yaml` 将 8 个 CPU Low 时间指标 + 14 个 NPU Low 指标覆盖为 Medium，使它们通过 metrics.Filter 进入 snapshot.json 供 dfee 推导/展示
- **DCMI 库路径修正**：`internal/source/dcmi/dcmi_cgo.go` 明确 `#cgo CFLAGS`/`LDFLAGS` 指向 `/usr/local/Ascend/driver/`
- **配置扩展**：`internal/config/config.go` + `configs/catmonitor.yaml` 加 chassis 采集器配置项
- **文档**：README/SPEC/DESIGN/indi_list 同步新增 Chassis/dfee/Disk latency，版本号升至 v0.3.1
- **版本号**：`cmd/catmonitor` version 升至 `0.3.1`；指标总数 152→159（+5 Chassis +2 Disk latency），部件 6→7
- **测试**：241 用例全过（较 v0.3.0 的 215 +26），`go vet` 零警告，Linux/Windows 双平台编译通过

### 已知限制

- DCMI CGo 未真机验证（需 NPU 服务器 `go build -tags dcmi`）；DCMI 原始单位待实测
- Chassis/Disk latency 未加入 configs/metrics.yaml 默认目录（靠 default-allow 规则采集）
- GPU/NPU/Chassis 无真机验证（测试由 mock 驱动）
- 继承 v0.3.0 已知限制：interval 未接 scheduler ticker、Windows 来源层迁移延后、`-c` 短选项 bug

---

## v0.3.0

| 项目 | 说明 |
|------|------|
| 版本号 | v0.3.0 |
| 发布时间 | 2026-07-17 |
| 发布人 | sunnytao |
| 平台支持 | Linux (x86_64), Windows (x86_64) |
| 合并来源 | feature/jhon (1bae347) → main |

### 变更摘要

- **健康度模块抽取**：`internal/health` → `features/health`，重构为按部件评估器（cpu/memory/disk/gpu/npu），`Evaluate` 用局部 scheme 不改写 receiver；规则对齐 indi_list High/Medium，新增 CPU MCE、内存 saturation/fragmentation、硬盘 smart_status、GPU utilization、NPU utilization/ECC/error_code，温度取子温度最差值
- **指标采集目录系统**：新增 `internal/metrics`（MetricSpec/Catalog/Filter）+ `configs/metrics.yaml` 默认目录（6 部件，High/Medium+静态身份默认采、Low 诊断默认不采）；模块自有 `metrics.yaml` 按 name 覆盖合并；scheduler 经 `SetFilter` DI 注入。注：interval 本期仅记录、不接 ticker
- **特性层**：新增 `features/` 承载上层模块；`web/` → `features/web/`（新增 `os_info` 采集，specsGroup 无 primary 数值型指标回退显示 value+unit，概览卡隐藏无数据部件）
- **目录与脚本**：`scripts/gen_metrics_catalog.py` 生成脚本、`scripts/install.sh` 部署 metrics.yaml
- **文档**：README/SPEC/DESIGN 同步结构树与引用，SPEC 精简（详细设计迁入 DESIGN），Web_SPEC/HEALTH_SPEC 路径更新
- **版本号**：`cmd/catmonitor` version 升至 `0.3.0`；指标总数不变（152）
- **测试**：215 用例全过（较 v0.2.2 的 176 +39），`go vet` 零警告，Linux/Windows 双平台编译通过

### 已知限制

- DCMI CGo 未真机验证（需 NPU 服务器 `go build -tags dcmi`）；DCMI 原始单位待实测
- interval 已记录但未接 scheduler ticker（采集仍 per-collector）
- 继承 v0.2.0/v0.2.1/v0.2.2 已知限制：NPU device 并行未在真多卡环境验证、Windows 来源层迁移延后、`-c` 短选项 bug

---

## v0.2.2

| 项目 | 说明 |
|------|------|
| 版本号 | v0.2.2 |
| 发布时间 | 2026-07-15 |
| 发布人 | sunnytao |
| 平台支持 | Linux (x86_64), Windows (x86_64) |
| 合并来源 | v0.2.1 分支 (79dc527) → main |

### 变更摘要

- **NPU 指标扩展**：5 → 74 指标，device 并行采集（每块 NPU 一个 goroutine，单卡失败不影响其他卡），全部指标 Linux 专属
- **来源层扩展**：新增 `dcmi`(CGo)/`npu_smi`/`hccn_tool`/`nvidia_smi` 4 个来源包，来源层 10 → 14 包，全部 6 个采集器接入来源层
- **DCMI CGo**：NPU 主体指标通过 `libdcmi.so`（`//go:build cgo && linux && dcmi`，`-tags dcmi` 启用），默认构建排除并优雅降级
- **GPU 迁移**：gpu collector 从内联 exec 改为调用 `nvidia_smi` 来源包（最后一个接入来源层的 collector）
- **总指标**：83 → 152
- **测试**：176 用例全过，`go vet` 零警告，Linux/Windows 双平台编译通过

### 已知限制

- DCMI CGo 未真机验证（需 NPU 服务器 `go build -tags dcmi`）；DCMI 原始单位待实测
- NPU device 并行未在真多卡环境验证
- 继承 v0.2.0/v0.2.1 已知限制：per-metric 周期未实现、Windows 来源层迁移延后、`-c` 短选项 bug

---

## v0.2.1

| 项目 | 说明 |
|------|------|
| 版本号 | v0.2.1 |
| 发布时间 | 2026-07-14 |
| 发布人 | sunnytao, ggboom12138 |
| 平台支持 | Linux (x86_64), Windows (x86_64) |
| 合并来源 | feature/jw (5461263) → main |

### 变更摘要

- **Web 仪表盘（新模块）**：新增独立二进制 `catmonitor-web`（`web/` 目录），可视化单台服务器健康度与各部件采集指标。SPA 概览页（健康度面板 + 设备规格面板 + 部件芯片 + 概览卡网格 + 趋势 sparkline）+ 部件详情页（趋势面板 + 全部指标表）。与采集守护进程/CLI 完全解耦，不修改主项目任何文件
- **解耦架构**：以 `web/data/snapshot.json` 为读写解耦边界，采集 goroutine 为唯一写者（原子写），HTTP 层只读快照；`health`/`metrics` 字段直接复用主项目结构体，不重新定义
- **静态设备规格采集**：`hwinfo.go` 启动期一次性采集跨部件身份（device_model/gpu_info/npu_info/disk_info/net_info，外部命令缺失优雅降级）+ `collector.go` staticStash 缓存 CPU/内存首周期静态指标，合并写入每个快照的 `specs` 字段
- **端口占用自动回退**：`listenWithFallback` 启动时 `net.Listen` 探测，`EADDRINUSE` 时端口 +1 递增（默认 9527 → 9528…）直至空闲，跨平台有效
- **REST API**：`/api/snapshot`、`/api/collectors`、`GET|POST /api/config`（间隔热生效 + runtime.json 持久化）、`POST /api/refresh`
- **可扩展性**：新增部件采集器只需在 `web/main.go` 加一行 blank import，导航/概览卡/详情页自动出现；新增趋势 sparkline 在 `trackedSeries` 加一行 spec
- **零新增依赖**：前端原生 HTML/CSS/JS `//go:embed` 内嵌进二进制，go.mod 仍仅 `gopkg.in/yaml.v3`；`Web_SPEC.md` 为 Web 模块唯一设计与规格文档
- **版本号**：`cmd/catmonitor` version 升至 `0.2.1`
- **测试**：168 用例全过（collectors 62 / sources 70 / health 20 / web 16），`go vet` 零警告，Linux/Windows 双平台编译通过，CLI（~4.3MB）与 Web（~9.1MB）二进制构建成功

### 已知限制（后续跟进）

- Web 为单机本地视图，不含认证与多机聚合（预留多 snapshot 源聚合）
- Web 历史仅存内存环形缓冲，重启清空，未落盘（预留 JSONL 持久化）
- Web 前端轮询而非推送（预留 WebSocket/SSE）
- 继承 v0.2.0 已知限制：gpu/npu 未迁移来源层、per-metric 周期未实现、Windows 来源层迁移延后、`-c` 短选项 bug

---

## v0.2.0

| 项目 | 说明 |
|------|------|
| 版本号 | v0.2.0 |
| 发布时间 | 2026-07-14 |
| 发布人 | sunnytao, ggboom12138 |
| 平台支持 | Linux (x86_64), Windows (x86_64) |
| 合并来源 | feature/wyx/add-metrics (b114848) → main (merge 21c7083) |

### 变更摘要

- **来源层（source layer）**：新增 `internal/source/` 9 个来源包（proc/sys/ipmi/lscpu/mce/dmesg/dmidecode/statfs/smartctl），抽象数据获取与解析；采集器不再直接 `os.ReadFile`/`exec`，来源返回 parsed struct + 单例 + `SetRoot`/可注入 fetcher + 缓存
- **CPU 指标扩展 7 → 40**：拓扑/核状态/频率/缓存/BuddyInfo/MCE 错误/IPMI 温度功率
- **Memory 指标扩展 6 → 19**：usage_detail/swap/PSI 饱和度/碎片化/页计数/DIMM 模块/功率
- **disk/network 迁移**：迁移到来源层（指标集不变，行为不变）
- **平台抽象层**：`internal/platform` 抽象配置路径与数据目录跨平台化
- **健康度自动检测**：`Evaluate()` 根据是否存在 GPU/NPU 指标自动选择权重方案
- **缺陷修复 4 项**：/sys 符号链接过滤、swap 无 swap 机器产出、ipmitool negative cache、statfs build tag
- **测试**：141 用例全过（collectors 62 / sources 59 / health 20），覆盖率 69.0%~92.3%，`go vet` 零警告，Linux/Windows 双平台编译通过
- **零新增依赖**：go.mod 仍仅 `gopkg.in/yaml.v3`

### 已知限制（后续跟进）

- gpu/npu 未迁移到来源层（待建 nvsmi/npsmi 来源）
- health 未给 CPU MCE / Memory saturation 加扣分规则
- per-metric 采集周期未实现（仍为 per-collector interval）
- Windows 来源层迁移延后（`*_windows.go` 保留原实现，扩展指标当前 Linux 专有）
- `-c` 短选项 bug 未修（建议使用 `--config`）

---

## v0.1.1

| 项目 | 说明 |
|------|------|
| 版本号 | v0.1.1 |
| 发布时间 | 2026-07-12 |
| 发布人 | sunnytao |
| 平台支持 | Linux (x86_64), Windows (x86_64) |

### 变更摘要

- **跨平台支持**：新增 Windows 平台适配，通过 Go 构建标签（build tags）隔离平台代码
- **6 个采集器**：CPU、Memory、Disk、GPU、NPU、Network 全部支持双平台
- **37 个采集指标**：Linux 全部 37 个，Windows 可用 32 个（5 个无可靠数据源优雅降级）
- **健康度评估**：新增 GPU/NPU 自动检测逻辑，根据实际采集指标自动切换权重方案
- **零新增依赖**：Windows 通过 Go 标准库 syscall 调用 kernel32.dll，go.mod 仍仅 yaml.v3
- **平台抽象层**：新增 `internal/platform` 包，统一管理跨平台默认路径

---

## v0.1.0

| 项目 | 说明 |
|------|------|
| 版本号 | v0.1.0 |
| 发布时间 | 2026-07-10 |
| 发布人 | sunnytao |
| 平台支持 | Linux (x86_64) |

### 变更摘要

- **核心架构**：Collector 接口 + Registry 注册表 + Scheduler 调度引擎
- **6 个采集器**：CPU、Memory、Disk、GPU、NPU、Network
- **37 个采集指标**：覆盖全部 6 个部件（High 14, Medium 14, Low 9）
- **健康度评估**：CPU-only 和 Accelerated 双权重方案，阈值扣分规则
- **CLI 命令**：daemon、collect、health、status、list、version
- **数据存储**：JSONL 格式，按天轮转
- **外部依赖**：仅 gopkg.in/yaml.v3
