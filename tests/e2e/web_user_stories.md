# CATMonitor web 特性 User Story

> **文档定位**：web 特性（`features/web/`）的用户故事清单，作为测试设计的输入需求。
>
> **配套产物**：`tests/e2e/web_testcases.xmind`（测试点）、`tests/e2e/web_testcases.xlsx`（可执行用例）。
>
> **素材来源**：以 **v0.3.6 现行架构**为准（`features/web/Web_SPEC.md` v0.3.6 刷新版 + DESIGN.md §6 + 现行代码）。与原始需求冲突时以原始需求为准。

---

## WB-US-001 只读消费 snapshot

作为运维工程师，我希望 catmonitor-web 只读消费 daemon 产出的 snapshot，以便单进程采集、多视图消费而互不干扰。

**业务规则**：
1. `-snapshot-dir` 指向 daemon snapshot 目录，默认端口 `:19322`；
2. `/api/snapshot` 组装全局 + 各 per-comp 文件（metrics/history/specs）；
3. 快照未就绪返回 503 `{"error":"snapshot not ready"}`；
4. 绝不调用采集器、不写本地文件。

## WB-US-002 仪表盘页面

作为运维工程师，我希望浏览器看到概览页与部件详情页，以便掌握整机与部件级健康状态。

**业务规则**：
1. 概览页：健康度总分/等级 + 各部件关键指标；
2. 详情页：该部件全部指标 + 60 点趋势图；
3. SPA 原生 HTML/CSS/JS 无构建步骤，`//go:embed` 内嵌单文件部署；
4. 前端按 `refresh_interval_ms` 轮询。

## WB-US-003 REST API

作为前端/二次开发者，我希望 REST API 提供导航与配置信息，以便驱动页面渲染。

**业务规则**：
1. `/api/collectors` 取注册表元数据（按 name 排序；`system` 不出现——非注册采集器）；
2. `/api/config` 返回 version/started_at/refresh_interval_ms/history_points/stress_operator（只读）。

## WB-US-004 端口占用自动回退

作为部署者，我希望默认端口被占时自动尝试下一端口，以便多实例共存无需改配置。

**业务规则**：
1. 默认 `:19322`，EADDRINUSE 时端口 +1 重试直到成功；
2. 返回实际绑定地址并记日志。

## WB-US-005 stress 页面经 daemon control socket

作为安全审计者，我希望 stress 操作经 daemon 侧控制套接字进行，以便压测能力由 daemon 统一管控而非 web 直接执行。

**业务规则**：
1. web 经 `-control-socket`（默认 `/run/catmonitor/control.sock`）的 ControlClient（HTTP-over-UNIX）查询/控制压测：`/stress/` 页面 + `/api/stress/{config,latest,history,runs,cancel}`；
2. 套接字路径须为绝对路径，否则启动失败；
3. daemon 未启用 stress 时请求降级报错（web 不崩）；
4. 旧 `-config` flag 废弃，仅记告警并忽略；
5. 操作权限由 daemon 侧套接字文件权限界定。

## WB-US-006 优雅退出

作为 systemd 管理者，我希望 web 收到信号后有序关闭，以便无残留连接。

**业务规则**：
1. SIGINT/SIGTERM 触发 5s 超时 Shutdown；
2. stress manager 一并关闭。
