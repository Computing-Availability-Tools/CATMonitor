# 数据源输出格式详解

CATMonitor 有五类数据面输出。按场景选择，注意各自的解析坑位。

## 1. CLI 单次采集（无需 daemon）

```bash
catmonitor collect -o json   # JSONL，默认
catmonitor collect -o table  # 人读表格
```

### JSONL 格式（重点坑位）

`-o json` 输出 **JSON Lines**：每行一个独立的 JSON 对象，**不是 JSON 数组**。

```json
{"component":"cpu","name":"usage","value":45.2,"unit":"%","labels":{"core":"total"},"timestamp":"2026-07-25T10:30:00Z"}
{"component":"cpu","name":"load_average","value":1.62,"unit":"","labels":{"interval":"1m"},"timestamp":"2026-07-25T10:30:00Z"}
{"component":"npu","name":"temperature","value":42.0,"unit":"°C","labels":{"npu_id":"0"},"timestamp":"2026-07-25T10:30:00Z"}
```

| 字段 | 说明 |
|------|------|
| `component` | 部件：cpu/memory/disk/gpu/npu/network/chassis |
| `name` | 指标名（同部件内唯一） |
| `value` | 数值；静态信息类指标可能为 1 或 0，实际信息在 labels |
| `unit` | 单位，可为空字符串 |
| `labels` | 维度标签；**无标签时整个字段省略**（不是 null），多实例指标靠它区分 |
| `timestamp` | RFC3339 |

**解析示例（jq）**：

```bash
# 过滤 NPU 温度
catmonitor collect -o json | jq -c 'select(.component=="npu" and .name=="temperature")'

# 聚合成数组
catmonitor collect -o json | jq -s 'length'

# 按部件计数
catmonitor collect -o json | jq -s 'group_by(.component) | map({component: .[0].component, count: length})'
```

> 环境没有 jq（或 jq 损坏）时，用 `grep -E '"component":"cpu"'` 逐行过滤后直接解析单行 JSON，或改用 `-o table` 人读。

### table 格式

列：`Component / Metric / Value / Unit / Labels`。适合人读预览，不适合程序解析。

## 2. Snapshot 文件（daemon 运行中，需 `snapshot.enabled: true`）

目录默认 `/var/lib/catmonitor/snapshot/`（配置 `snapshot.dir`），原子写（tmp+rename），读到的一定是完整文件。

### 全局 `snapshot.json`

```json
{
  "session_id": "1757431277",
  "timestamp": "2026-09-08T14:30:00Z",
  "refresh_interval_ms": 3000,
  "intervals_ms": { "cpu": 3000, "memory": 3000 },
  "health": { "score": 100, "grade": "Excellent", "...": "同 catmonitor health -o json" },
  "collectors": [ { "name": "cpu", "component": "cpu", "priority": "High", "interval": "3s", "enabled": true } ],
  "system_specs": [ { "component": "system", "name": "device_model", "value": 1, "labels": { } } ]
}
```

适合：整机聚合视图（健康 + 采集器 + 设备规格一次拿全）。

### 部件 `snapshot_<comp>.json`

```json
{
  "component": "npu",
  "timestamp": "2026-09-08T14:30:00Z",
  "metrics": [ { "name": "temperature", "value": 42.0, "unit": "°C", "labels": { "npu_id": "0" } } ],
  "history": { "npu_temperature": [ /* 最近 60 个点 */ ] },
  "specs": { }
}
```

适合：**要看历史趋势**时（`history` 键为 `<component>_<series>` 的最近 60 点序列）与读取部件静态规格（`specs`：gpu_info/npu_info/disk_info/os_info 等）。

> `history` 的键是预定义的 TrackedSeries（如 `cpu_usage`、`npu_temperature`、`disk_space_usage`），不是任意指标都有历史。

## 3. daemon Prometheus exporter（`:19320`）

```bash
curl -s http://localhost:19320/metrics
curl -s http://localhost:19320/-/ready    # 200 就绪 / 503 缓存未就绪
```

- 指标名规则：`catmonitor_<component>_<name>`，name 中 `-`/`/`/`.` 转 `_`。
- 类型：name 以 `_time` 或 `_total` 结尾 → counter，其余 gauge；标签按字典序。
- 例：`catmonitor_cpu_usage{core="total"}`、`catmonitor_npu_temperature{npu_id="0"}`。
- 一次采集同时落 JSONL 与缓存导出，零额外进程。

## 4. dfee exporter（`:9333`，需 `catmonitor-dfee -exporter=enabled`）

node_exporter / dsmi 对齐命名，适合已有 Prometheus 生态的接入：

- `node_*`：CPU/内存/网络/磁盘（node_exporter 风格）
- `dsmi_*`：NPU 指标
- `ipmi_*`：机箱
- `static_hardware_info` / `static_software_info`：value=1，信息在 labels（含 vLLM/sglang/mindie 等 AI 软件栈版本）

另有 dfee SPA 的 `GET :19323/api/dfee`（78 项能效指标 + 34 张图表数据）。

## 5. JSONL 历史存储（daemon 持续采集落盘）

路径：`/var/lib/catmonitor/data/<component>_<YYYY-MM-DD>.jsonl`（配置 `storage.data_dir`），每天一文件，行格式同 CLI collect 输出，保留 `max_file_age`（默认 168h）。

适合：回看过去数小时/数天的原始序列（注意文件按天轮转，跨天要读多个文件）。

## 数据源选择速查

| 需求 | 推荐 |
|------|------|
| 现在的值，一次性的 | CLI `collect -o json` |
| 含历史趋势的最新值 | `snapshot_<comp>.json` 的 `history` |
| 整机健康 + 指标 + 规格聚合 | `snapshot.json` 或 Web `/api/snapshot` |
| 接入 Prometheus/Grafana | `:19320/metrics`（catmonitor_*）或 `:9333/metrics`（node_* 风格） |
| 过去 N 小时原始序列 | `storage.data_dir` 的按天 JSONL |
