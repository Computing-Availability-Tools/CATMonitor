# CATMonitor snapshot 特性 User Story

> **文档定位**：snapshot 特性（`features/snapshot/`）的用户故事清单，作为测试设计的输入需求。
>
> **配套产物**：`tests/e2e/snapshot_testcases.xmind`（测试点）、`tests/e2e/snapshot_testcases.xlsx`（可执行用例）。
>
> **素材来源**：由 `DESIGN.md` §6（snapshot 无独立 SPEC）反向改写为用户视角（v0.3.6 校订），经评审定稿。与原始需求冲突时以原始需求为准。

---

## SN-US-001 统一 snapshot 生产

作为 web/dfee 等只读消费者，我希望 daemon 统一产出 snapshot 文件，以便一台机器只跑一次硬件采集。

**业务规则**：
1. daemon 是 snapshot 唯一写者；
2. 产出 per-component `snapshot_<comp>.json`（component/timestamp/metrics/history 60 点环形/specs）+ 全局 `snapshot.json`（session_id/timestamp/refresh_interval_ms/history_points=60/health/collectors/intervals/system_specs）；
3. web/dfee 绝不采集、不写本地文件，经 `-snapshot-dir` 只读消费。

## SN-US-002 原子写保证

作为消费者，我希望 snapshot 文件原子更新，以便任何时刻读到的都是完整文件而非半写状态。

**业务规则**：
1. 临时文件 + `os.Rename` 原子替换；
2. 读取方无锁；
3. 损坏/缺失文件读取返回 error，由消费方容错跳过。

## SN-US-003 per-component 文件与趋势环

作为 web 用户，我希望每个部件独立 snapshot 文件内含 60 点趋势序列，以便部件详情页展示趋势图。

**业务规则**：
1. 每个采集批次原子写该部件文件；
2. history 按序列名环形保留 60 点；
3. specs（gpu_info/npu_info 等启动身份）omitempty。

## SN-US-004 全局快照与刷新节奏

作为前端，我希望全局 snapshot 提供刷新节奏与会话标识，以便正确轮询并在 daemon 重启后识别新会话。

**业务规则**：
1. C_global = min(C_comp)，`refresh_interval_ms` 供前端轮询；
2. session_id 标识 daemon 会话；
3. health/collectors/intervals/system_specs 聚合全局视图。

## SN-US-005 硬件身份一次性采集

作为运维，我希望启动期采集一次硬件身份，以便快照携带设备规格且不重复跑硬件探测。

**业务规则**：
1. `CollectHWSpecs()` 启动期执行一次（非注册采集器）；
2. system 类 specs 进全局文件、部件类 specs 分发到对应 per-comp 文件。

## SN-US-006 只读消费契约

作为二次开发者，我希望 `ReadGlobal`/`ReadComp` 提供稳定 API，以便自定义消费者接入。

**业务规则**：
1. 文件缺失/非法 JSON 返回 error；
2. JSON 字段名稳定（前端可直接消费）。
