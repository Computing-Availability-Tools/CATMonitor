# CATMonitor 系统级 e2e 测试 User Story

> **文档定位**：CATMonitor 全系统端到端（e2e）测试的用户故事清单，作为 e2e 测试设计的唯一输入需求。
>
> **配套产物**：`tests/e2e/testcases.xmind`（测试点）、`tests/e2e/testcases.xlsx`（可执行用例）。
>
> **素材来源**：由 SPEC.md / DESIGN.md（v0.3.6）+ 各特性 `*_SPEC.md` 反向改写为**外部用户视角**（仅描述从进程外部可观察到的行为：HTTP 接口/文件系统/进程信号/浏览器），经评审定稿。
>
> **测试范围约定**：不含 stress 特性本身的压测功能（仅测 web 侧安全门控行为）；不含组件级/单元测试（由 Go 测试代码覆盖）。
>
> **依赖约定**：不依赖 Prometheus Server 安装——验证的是 `/metrics` 端点输出的 Prometheus 文本格式正确性，用 curl/HTTP 客户端即可断言。

---

## SYS-001 Daemon 启动并产出监控数据

作为运维工程师，我启动 CATMonitor daemon 后，它自动采集全部部件指标、评估健康度、将 snapshot 文件写入磁盘、并通过 HTTP 端点暴露指标，以便我无需任何手工操作就能获得完整的监控数据。

**外部可见行为**：
1. daemon 启动后 HTTP GET `:19320/metrics` 返回 `text/plain; version=0.0.4` 格式的 `catmonitor_*` 指标；
2. `/-/ready` 在首次采集完成前返回 503，之后返回 200；
3. `snapshot.json`（全局）+ `snapshot_<部件>.json`（每部件）出现在配置的 snapshot 目录中；
4. JSONL 历史文件按部件逐日写入 `data_dir`；
5. 指标名形如 `catmonitor_cpu_usage`，特殊字符（`/`、`-`）替换为 `_`；以 `_time`/`_total` 结尾的为 counter 类型；
6. 采集周期内 daemon 不崩溃、无 panic 日志。

## SYS-002 Web 仪表盘展示实时监控数据

作为运维工程师，我在浏览器打开 web 仪表盘，能看到服务器健康总分和等级、各部件关键指标卡片、60 点趋势图，以便我实时掌握服务器状态。

**外部可见行为**：
1. `/api/snapshot` 返回全局+部件合并视图（`session_id`/`health`/`metrics`/`history`/`specs`）；
2. 概览页显示健康总分、等级（Excellent/Good/Warning/Critical）、各部件关键指标；
3. 部件详情页显示该部件全部指标 + 60 点趋势图；
4. `/api/collectors` 返回按 name 排序的采集器列表（不含 `system`）；
5. `/api/config` 返回 `version`/`started_at`/`refresh_interval_ms`/`history_points`/`stress_operator`；
6. snapshot 未就绪时 `/api/snapshot` 返回 503；
7. web 进程不写 snapshot 目录（只读消费者，chmod 555 仍正常运行）；
8. 默认端口 :19322 被占时自动 +1 重试。

## SYS-003 dfee 能效仪表盘展示图表

作为能效分析工程师，我在浏览器打开 dfee 页面，看到约 34 张实时图表（NPU 频率/功率/CPU 利用率分解/网络/内存等），可拖拽缩放、多选筛选、折叠模块。

**外部可见行为**：
1. `/api/dfee` 返回 `timestamp`/`refresh_interval_ms`/`charts` 三顶层字段，`charts` 非空；
2. 图表支持卡片拖拽重排、右下角手柄缩放、NPU（双维度 ID+Chip）/磁盘/网络多选下拉筛选、模块折叠；
3. `-exporter enabled` 启动后 `:9333/metrics` 暴露 `node_*`/`dsmi_*`/`ipmi_*`/`static_*` 指标（默认关闭）；
4. `-csv enabled` 后产出 `dfee_metrics_*.csv`（四列：timestamp/metric_name/labels/value）；
5. 无 GPU 时 GPU 图表隐藏，无 NPU 时 NPU 图表显示占位。

## SYS-004 故障订阅与 webhook 推送

作为外部故障管理系统（如 EEP），我通过 REST API 注册 NPU 故障订阅，在故障发生/恢复时收到 HTTP POST 的 FaultEvent JSON 推送，以便第一时间自动化容错处理。

**外部可见行为**：
1. `POST /faultsub/subscriptions`（:19321）注册订阅（声明 types/components/npu_ids/delivery/endpoint）返回 201 + 分配的 ID；
2. NPU 故障发生 → 订阅方收到 `Content-Type: application/json` + `X-CatMonitor-Event` + `X-CatMonitor-EventID` 头的 POST 请求；
3. 事件 JSON 含 `event_id`/`type`/`component`/`npu_id`/`severity`/`detail`/`timestamp`/`recovered` 字段；
4. 故障恢复 → 发送 `recovered:true` 事件，持续故障不重复推送（变迁驱动）；
5. `GET /faultsub/events?since=&type=&npu_id=` 按时间/类型/NPU 过滤回补；
6. `GET /faultsub/snapshot` 返回各 NPU 最新活跃故障（恢复的清除）；
7. webhook 端点不可达/超时 → 重试 N 次后仅记日志，事件仍可经 REST 回补；
8. 订阅级去抖（DebounceMs）窗口内同 (npu,type) 不重复推送；
9. 注册 endpoint 无 URL 校验——任意内网/回环地址均接受并实际投递（SSRF 暴露面）；
10. REST 无认证——任意本地进程可 DELETE 他人订阅且立即生效。

## SYS-005 Daemon 重启后自愈

作为运维工程师，daemon 重启后 web 和 dfee 自动切换到新会话数据而无需重启自身，以便我在维护 daemon 时消费者不受影响。

**外部可见行为**：
1. 重启后 `snapshot.json` 的 `session_id` 变化（新会话标识）；
2. 停机窗口内 web 仍 200 并展示旧数据（snapshot 文件持久化）；
3. 重启完成后 web 返回新 `session_id`（自愈，无需重启 web）；
4. `/-/ready` 从 503（未采集）变为 200（首次采集完成）；
5. faultsub 订阅重启后全部丢失（纯内存态），重新注册后投递恢复。

## SYS-006 stress 操作安全门控

作为安全审计者，压测操作经 daemon 侧 control socket 权限控制，非授权进程无法触发，以便压测能力不被远程滥用。

**外部可见行为**：
1. control socket 以 0660 权限创建，非 root/非同组进程无法连接；
2. web 的 `/api/stress/*` 端点有三道防线：缺 `X-CATMonitor-Action` 头 403、非 JSON Content-Type 415、跨 Origin 403；
3. 合法请求（三道防线全过）到达控制器（202 接受或结构化 5xx 执行器不可用）；
4. socket 不可达时 stress 端点优雅降级（`available:false`），web 其他功能不受影响。

## SYS-007 优雅降级

作为运维工程师，硬件缺失/外部工具不可用/磁盘满时 daemon 不崩溃且已有功能继续，以便部分监控的机器上仍能获得有效数据。

**外部可见行为**：
1. 无 nvidia-smi/npu-smi/ipmitool 时对应部件指标为空（不报错），其余部件正常；
2. BMC 响应慢（`ipmitool sensor list` >10s）时 daemon 不阻塞，首批 ipmi 依赖指标延迟到达但不丢失；
3. JSONL 数据目录写满时 daemon 不崩、`/metrics` 缓存持续可用、snapshot 文件（独立目录）继续更新；
4. 磁盘空间恢复后 JSONL 写入自愈（无需重启 daemon）。

## SYS-008 KPI 文件输出

作为 straggler 慢节点检测器，我从 data_dir 读到日级 JSONL 格式的 KPI 数据（温度/功耗/频率/利用率/带宽/RoCE 统计），以便执行慢节点分析。

**外部可见行为**：
1. `{data_dir}/straggler_kpi_YYYY-MM-DD.jsonl` 文件按日产出；
2. 每行 JSON 含 `ts`（unix 秒）/`vals`（deviceID→metric→value 嵌套映射）/`cpu_avg`（cpuName→利用率）；
3. `vals` 键为全局设备号（A3 双芯片 8 卡=键 0..15；掉卡时编号稳定保留空洞）；
4. 计数器写原始累计值（不做 delta）；
5. 过期日级文件按 retention 自动删除（磁盘空间有界）。

## SYS-009 feature-scope 指标采集

作为特性开发者，我通过 `features` 配置声明所需指标，daemon 只采集白名单内的指标并按 feature 声明的节奏采集，以便多特性共存时互不干扰且开销最小。

**外部可见行为**：
1. `features: [web, dfee]` 时 `/metrics` 仅暴露 web+dfee 两个 feature 的 `metrics.yaml` 并集内的指标；
2. 白名单外的高优先级指标不出现在任何输出中；
3. `min_priority: high` 时 Low/Medium 指标被滤除（仅 High+Static 保留）；
4. features 空列表时退回目录全集（按优先级门采集）；
5. 每部件采集节奏 = features 声明 interval 的最小值（覆盖 yaml 配置的 interval）。

## SYS-010 跨视图数据一致性

作为运维工程师，daemon 产出的数据在 web、dfee、HTTP `/metrics` 端点三个消费视图中保持一致，以便我在任何入口看到的都是同一份实时数据。

**外部可见行为**：
1. daemon `snapshot.json` 的 `session_id` == web `/api/snapshot` 的 `session_id`；
2. `refresh_interval_ms` 在 daemon snapshot / web / dfee 三端一致；
3. HTTP GET `:19320/metrics` 中的 `catmonitor_cpu_usage` 与 web `/api/snapshot` 中的 cpu usage 指标来自同一次采集（值一致）。
