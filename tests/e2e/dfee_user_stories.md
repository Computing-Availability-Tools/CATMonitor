# CATMonitor dfee 特性 User Story

> **文档定位**：dfee 能效监控特性（`features/dfee/`）的用户故事清单，作为测试设计的输入需求。
>
> **配套产物**：`tests/e2e/dfee_testcases.xmind`（测试点）、`tests/e2e/dfee_testcases.xlsx`（可执行用例）。
>
> **素材来源**：由 `features/dfee/dfee_SPEC.md`（v0.3.6 刷新版）反向改写为用户视角，经评审定稿。与原始需求冲突时以原始需求为准。

---

## DF-US-001 能效指标过滤与图表分组

作为能效分析工程师，我希望从全量指标中过滤出能效相关指标并按主题分组为图表，以便一屏纵览整机能效。

**业务规则**：
1. 过滤集从 216 项指标中筛出 78 项能效指标（v0.3.6 SPEC；代码 efficiencySpecs 80 条定义）；
2. 34 张图表按主题分组（filter_test 断言）（NPU 频率/功率/CPU 利用率分解/网络/内存等），每图含 id/title/y_unit/priority/series；
3. series.id 命名：NPU=`{npu_id}:{metric}`、磁盘=`{device}:{metric}:{direction}`、网络=`{interface}:{metric}`、Memory=`{metric}:{field}`、CPU 推导=`{derived_name}`；
4. 快照未就绪 503。

## DF-US-002 CPU 利用率推导

作为能效分析工程师，我希望将 CPU 累计 jiffies 推导为利用率分解，以便看实时占比而非累计值。

**业务规则**：
1. 8 个 jiffies 指标（user/nice/system/idle/iowait/irq/softirq/steal，core=total）→ 7 项利用率（含非空闲合计）；
2. 首次调用无前值 → 全部 0；
3. 计数器回退（curr<prev）→ 差值钳 0；
4. 按序列缓存前值。

## DF-US-003 网络字节差值

作为能效分析工程师，我希望网卡累计字节数转为速率差值，以便观察带宽波动。

**业务规则**：
1. rx/tx_bytes_total 替换为与上次的差值；
2. 首次调用 0；
3. 计数器重置钳 0；
4. 按序列 ID 缓存前值。

## DF-US-004 交互式实时图表

作为能效分析工程师，我希望图表支持交互定制，以便聚焦关注的数据维度。

**业务规则**：
1. Canvas 实时渲染 + 滚动缓冲；
2. 图表卡片拖拽重排 + 缩放；
3. NPU 双维度（NPU ID + CHIP ID）与磁盘/网络多选下拉筛选；
4. 模块折叠；
5. 空数据图表显示占位不报错。

## DF-US-005 内置 Prometheus exporter

作为 Grafana 用户，我希望 dfee 直接暴露 node_exporter/dsmi 兼容指标，以便复用现有仪表盘。

**业务规则**：
1. `-exporter enabled` 启动 `:9333/metrics`（默认关闭，`-exporter-port` 可调端口）；
2. snapshot 映射为 `node_*`/`dsmi_*`/`ipmi_*`/`static_*` 命名；
3. `supplementDiskStats` 直读 `/proc/diskstats` 补全快照缺失设备；
4. 启动期采集静态 HW/SW 身份（dmidecode/CANN/pip 等），无工具时优雅降级为空。

## DF-US-006 CSV 落盘

作为数据分析师，我希望能效数据周期性落盘 CSV，以便离线分析与导入。

**业务规则**：
1. 产出 `dfee_metrics_{时间戳}.csv`；
2. 四列：timestamp/metric_name/labels/value；
3. 与 exporter 同源（buildMetrics）；
4. 周期 interval 写、Close 关闭文件。
