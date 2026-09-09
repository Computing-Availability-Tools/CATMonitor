# CATMonitor faultsub 特性 User Story

> **文档定位**：faultsub 特性（`features/faultsub/`）的用户故事清单，作为测试设计的输入需求。
>
> **配套产物**：`tests/e2e/faultsub_testcases.xmind`（测试点）、`tests/e2e/faultsub_testcases.xlsx`（可执行用例）。
>
> **素材来源**：由 `features/faultsub/faultsub_SPEC.md`（v0.3.6 刷新版）反向改写为用户视角，经评审定稿。SPEC 中 REST 端口写作 `:9101`，实际代码为 `:19321`（v0.3.5 端口统一），本文以 **19321** 为准。与原始需求冲突时以原始需求为准。

---

## FS-US-001 订阅 NPU 故障事件并接收 Webhook 推送

作为外部故障管理者（如 EEP 弹性容错系统），我希望订阅 CATMonitor 的 NPU 故障事件并通过 HTTP Webhook 接收 JSON 推送，以便故障发生/恢复时第一时间自动化容错处理而无需轮询。

**业务规则**：
1. 7 类故障判定：card_drop（值=1 或 error_codes 含 0x40f84e00，大小写不敏感）、npu_health（status ∈ Alarm/Critical）、npu_error_code（值>0，warning）、hbm_uce、ddr_uce（值>0，critical）、roce_link_down（=0 或 status=down 或 link 异常，warning）、driver_unhealthy（≠0，warning）；判定按卡（npu_id）分组，同卡各芯片（chip_id）指标合并为同一份卡级状态；
2. 变迁驱动：仅故障新出现或恢复时发事件，持续故障不重发；
3. 事件含全局唯一 event_id、type、component、npu_id、severity、detail、timestamp、recovered 标志；
4. Webhook 异步投递（go 协程）不阻塞采集管道；
5. 推送头含 Content-Type: application/json、X-CatMonitor-Event、X-CatMonitor-EventID，超时默认 5s。

## FS-US-002 通过 REST API 管理订阅

作为故障管理系统的开发者，我希望通过 REST API 注册/查询/注销订阅，以便程序化接入而无需重启 daemon。

**业务规则**：
1. POST/GET/DELETE `/faultsub/subscriptions`（含按 id 查看与注销），服务地址 `:19321`；
2. 订阅可声明过滤条件：types、components、npu_ids；
3. 投递方式 webhook | poll；
4. 订阅默认值：去抖 0ms、最低严重级 warning。

## FS-US-003 查询故障快照与事件回补

作为运维工程师，我希望查询各 NPU 当前活跃故障快照和近期历史事件，以便排查时了解故障全貌。

**业务规则**：
1. GET `/faultsub/snapshot` 返回各 NPU 最新活跃故障（恢复的清除）；
2. GET `/faultsub/events?since=&type=&npu_id=` 按时间/类型/NPU 过滤回补；
3. 事件环形缓冲（默认 1024，溢出丢最旧）；
4. `/-/healthy` 恒 200；`/-/ready` 有采集过则 200 否则 503。

## FS-US-004 订阅级去抖抑制

作为订阅者，我希望设置去抖窗口抑制同一 (npu, type) 的重复推送，以便避免告警风暴。

**业务规则**：
1. DebounceMs 窗口内同 (npu,type) 事件不重复推送；
2. 不同订阅独立去抖互不影响；
3. 去抖只作用于推送，事件仍进环形缓冲可回补。

## FS-US-005 故障规则开关配置

作为运维工程师，我希望按需启停故障判定规则，以便适配现场告警策略。

**业务规则**：
1. rules 未配置的规则默认启用（fail-open）；
2. 配置为 false 的规则不判定不推送；
3. 示例默认：driver_unhealthy=false 其余 true。

## FS-US-006 默认关闭零回归

作为 CATMonitor 维护者，我希望 faultsub 关闭时 daemon 行为与无此模块完全一致，以便渐进采用无回归风险。

**业务规则**：
1. enabled=false（默认）时 Storage 链路不变（Scheduler→CachingStorage→JSONL）；
2. 启用时链路（v0.3.6 修复 c110606）：Scheduler→snapshot.PerCompWriter（若 snapshot 启用）→FaultStorage（包装**当前链头 sink**，而非固定 cacheStore）→StragglerStorage（若启用）→CachingStorage→JSONL——两 tap 线性组合，与 stragglerout 同开时均收到写入，KPI 不再被旁路；
3. 内层写失败仅记日志不阻断故障检测，故障检测异常也不阻断落盘。
