# stragglerout KPI 输出模块技术规格 (stragglerout_SPEC)

> **文档定位**：stragglerout 模块的设计与规格文档。
> **对应代码**：`features/stragglerout/`（Go package `stragglerout`，与主项目同一 Go module）。
> **零新依赖**：仅用标准库。**默认关闭**：`straggler_output.enabled` 默认 false，不启用时无 KPI 文件、daemon 零回归。

## 1. 目标

为 straggler 慢节点检测器提供专用 KPI 时序文件，替代其自带 `kpi_collect.sh`。作为 daemon 的 `collector.Storage` 插件，复用采集管道，把每次采集到的 NPU KPI 指标按"每时刻×每卡"聚合追加写为日级 JSONL，供 straggler CLI 读取。

## 2. 架构

```
Scheduler → StragglerStorage.Write(npu metrics)
              ├── 委托 inner.Write (CachingStorage→JSONL，不变)
              └── KPIMapper.Extract → 缓冲 → 周期 flush → KPIWriter.Append
                    → {data_dir}/straggler_kpi_{date}.jsonl (保留 retention)
```

## 3. 文件格式（JSONL，每行一个 KPISample）

```json
{"ts":1784547926,"vals":{"0":{"temp":47,"power":1628,"aicore_freq":1800,"aicore_util":45,"hbm_util":50,"tx_bandwidth":1250,"rx_pfc_pkt":0,"roce_tx_err_pkt":0,"roce_out_of_order":0,"roce_new_pkt_rty":0}},"cpu_avg":{"cpu1":"4.26"}}
```

字段与 straggler `resource.CSVRow` 1:1 对应，straggler 的 JSON reader 直接重建 `TimeSeriesData`。

## 4. 指标映射

| straggler 字段 | CATMonitor metric.Name | 备注 |
|---|---|---|
| temp | temperature | npu |
| power | power_draw | npu |
| aicore_freq | aicore_freq | npu |
| aicore_util | utilization | npu |
| hbm_util | memory_usage | npu |
| tx_bandwidth | net_tx_bandwidth | npu (hccn_tool) |
| rx_pfc_pkt | mac_rx_pfc_pkt_num | npu (hccn_tool) |
| roce_tx_err_pkt | roce_tx_err_pkt_num | npu (hccn_tool) |
| roce_out_of_order | roce_out_of_order_num | npu (hccn_tool) |
| roce_new_pkt_rty | roce_new_pkt_rty / roce_retrans_pkt_num (别名) | npu (hccn_tool)，真机字段名可能不同，别名兼容 |
| cpu_avg | cpu/usage | 按 cpu 标签聚合，忽略 total |

> 计数器写**原始累计值**（不做 delta），straggler 聚合时累加，语义对齐。
> hccn_tool 的 `parseStatistics` 是通用 key:value 解析器，无需改代码即可捕获 `roce_new_pkt_rty`（仅 metrics.yaml 登记）。

## 5. 配置

```yaml
straggler_output:
  enabled: false
  data_dir: /var/lib/catmonitor/straggler
  retention: 360h        # 15 天
  flush_interval: 60s
```

## 6. 测试

- `storage_test.go`：Extract 各 NPU 指标 + 别名 + cpu + 忽略非 KPI 批；StragglerStorage 委托 + tap + flush 落盘；KPIWriter 追加 + 保留期清理。

运行：`go test ./features/stragglerout/`

*文档版本：v1.0*
