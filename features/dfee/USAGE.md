# CATMonitor dfee 使用文档

> 本文档说明如何单独使用 dfee（能效监控）功能：启动 daemon 采集、启用 dfee exporter、配置 Prometheus + Grafana 可视化。

---

## 1. 编译

```bash
# NPU 环境（含 DCMI 指标）
CGO_ENABLED=1 go build -tags dcmi -o catmonitor ./cmd/catmonitor
CGO_ENABLED=0 go build -o dfee ./features/dfee

# 非 NPU 环境（纯 CPU/GPU）
CGO_ENABLED=0 go build -o catmonitor ./cmd/catmonitor
CGO_ENABLED=0 go build -o dfee ./features/dfee
```

## 2. 配置 daemon

修改 `configs/catmonitor.yaml`，将 `features` 改为只启用 dfee：

```yaml
collectors:
  chassis:
    enabled: true
    interval: 3s
  cpu:
    enabled: true
    interval: 3s
  memory:
    enabled: true
    interval: 3s
  disk:
    enabled: true
    interval: 5s
  gpu:
    enabled: true
    interval: 3s
  npu:
    enabled: true
    interval: 3s
  network:
    enabled: true
    interval: 3s

storage:
  data_dir: /var/lib/catmonitor/data
  max_file_age: 168h
  rotation: daily

collection:
  min_priority: medium

features: [dfee]          # ← 只启用 dfee

snapshot:
  enabled: true
  dir: /var/lib/catmonitor/snapshot
```

> `features: [dfee]` 表示只采集 dfee metrics.yaml 中声明的指标（scope 白名单），其余指标跳过。这样 dfee exporter 输出的指标和采集范围一致。

## 3. 启动 daemon

```bash
# 创建 snapshot 目录
mkdir -p /var/lib/catmonitor/snapshot

# 启动 daemon（需要 root 访问 IPMI/SMART/NPU 设备）
sudo ./catmonitor daemon --config configs/catmonitor.yaml

# 验证 snapshot 已生成（等待 6-9 秒）
ls /var/lib/catmonitor/snapshot/
# 预期：snapshot.json + snapshot_cpu.json + snapshot_memory.json + ...
```

## 4. 启动 dfee exporter

```bash
./dfee \
  -addr=:19323 \
  -snapshot-dir=/var/lib/catmonitor/snapshot \
  -exporter=enabled \
  -exporter-port=9333 \
  -csv=disabled

# 验证 exporter
curl http://localhost:9333/metrics | head -20

# 可选：开启 CSV 持久化
./dfee \
  -addr=:19323 \
  -snapshot-dir=/var/lib/catmonitor/snapshot \
  -exporter=enabled \
  -exporter-port=9333 \
  -csv=enabled \
  -csv-dir=/var/lib/catmonitor/csv \
  -csv-interval=10s

# 可选：定时运行（10 分钟后自动退出）
./dfee \
  -addr=:19323 \
  -snapshot-dir=/var/lib/catmonitor/snapshot \
  -exporter=enabled \
  -csv=enabled \
  -max-runtime=10m
```

**dfee 参数说明：**

| 参数 | 默认值 | 说明 |
|------|--------|------|
| `-addr` | `:19323` | dfee SPA + API 监听地址 |
| `-snapshot-dir` | `/var/lib/catmonitor/snapshot` | daemon snapshot 目录 |
| `-exporter` | `disabled` | `enabled` 开启 Prometheus exporter |
| `-exporter-port` | `9333` | exporter 监听端口 |
| `-device` | `""` | NPU 设备过滤（如 `0,1`），空=全部 |
| `-docker-container` | `""` | Docker 容器名（采集软件版本信息） |
| `-csv` | `disabled` | `enabled` 开启 CSV 输出 |
| `-csv-dir` | `/var/lib/catmonitor/csv` | CSV 输出目录 |
| `-csv-interval` | `10s` | CSV 写入间隔 |
| `-max-runtime` | `0` | 最大运行时长（如 `10m`、`1h`），0=永久 |

## 5. 安装 Prometheus + Grafana

### 5.1 安装 Prometheus

```bash
# 拉取镜像
docker pull prom/prometheus

# 创建配置目录和文件
mkdir -p $PWD/prometheus/data
touch $PWD/prometheus/prometheus.yml

# 设置权限（Prometheus 容器内 UID 为 65534）
chown -R 65534:65534 $PWD/prometheus/data
chown 65534:65534 $PWD/prometheus/prometheus.yml

# 启动 Prometheus
docker run -d \
  --name prometheus \
  -v $PWD/prometheus/data:/prometheus \
  -v $PWD/prometheus/prometheus.yml:/etc/prometheus/prometheus.yml \
  -p 9090:9090 \
  prom/prometheus

# 编辑配置文件
vim $PWD/prometheus/prometheus.yml
```

配置文件内容（`targets` 改为你的 dfee exporter 地址和端口）：

```yaml
scrape_configs:
  - job_name: "CATMonitor"
    scrape_interval: 2s
    scrape_timeout: 2s
    static_configs:
      - targets: ["<dfee_exporter_ip>:9333"]
        labels:
          instance: <dfee_exporter_ip>
```

> 将 `<dfee_exporter_ip>` 替换为运行 dfee exporter 的机器 IP。

> 修改配置后重启容器：`docker restart prometheus`
>
> 验证：浏览器访问 `http://localhost:9090/targets`，状态应为 **UP**。

### 5.2 安装 Grafana

```bash
# 拉取镜像
docker pull grafana/grafana

# 创建数据目录（Grafana 容器内 UID 为 472）
mkdir $PWD/grafana-storage
chown -R 472:472 $PWD/grafana-storage

# 启动 Grafana
docker run -d \
  --name=grafana \
  --restart=always \
  -p 3000:3000 \
  -v $PWD/grafana-storage:/var/lib/grafana \
  grafana/grafana
```

> 验证：浏览器访问 `http://localhost:3000`，默认账号 `admin / admin`。

### 5.3 配置 Grafana 数据源

1. 进入 **Configuration → Data Sources**
2. 点击 **Add data source**
3. 选择 **Prometheus**
4. URL 填入 `http://localhost:9090`
5. 点击 **Save & Test**，显示绿色 "Data source is working"

## 6. 导入 Grafana Dashboard

1. 进入 **Dashboards → Import**
2. 点击 **Upload JSON file**
3. 选择 `features/dfee/grafana-dashboard.json`
4. 在导入页面：
   - **DS_PROMETHEUS**：选择你的 Prometheus 数据源
   - 点击 **Import**
5. 导入后，顶部变量栏可选择：
   - **Instance**：目标实例（单选）
   - **Job**：目标 Job（单选）
   - **NPU ID**：NPU 卡号（多选，支持 All）
   - **Chip ID**：NPU 芯片号（多选，支持 All）

**Dashboard 面板概览（24 个面板，6 行）：**

| 行 | 面板 | 说明 |
|---|------|------|
| CPU | CPU 利用率（堆叠） | IDLE/非IDLE/用户空间/系统/IO等待/中断/虚拟机 7 条 |
| CPU | 系统平均负载 | load1/load5/load15 |
| CPU | 在线核心数 | stat |
| Memory | 内存利用率 | (MemTotal-MemFree-Buffers-Cached-SReclaimable)/MemTotal |
| Memory | 交换分区利用率 | (SwapTotal-SwapFree)/SwapTotal |
| Network | 接收速率 | 按 interface |
| Network | 发送速率 | 按 interface |
| Disk | 读吞吐 | 按 device（bytes/s） |
| Disk | 写吞吐 | 按 device（bytes/s） |
| Disk | 读耗时 | 按 device |
| Disk | 写耗时 | 按 device |
| NPU | 9 个面板 | AICore/HBM/Vector 利用率、NPU 整体利用率、功耗、电压、HBM/AICore 频率、HBM 带宽 |
| Chassis | 进风口温度 | ipmitool |
| Chassis | 出风口温度 | ipmitool |
| Chassis | 整机功耗 | ipmitool |
| Chassis | 风扇转速 | 按 fan_id |

## 7. 完整启动流程（总结）

```bash
# 1. 配置 daemon（features 改为 [dfee]）
vim configs/catmonitor.yaml

# 2. 启动 daemon
sudo ./catmonitor daemon --config configs/catmonitor.yaml &

# 3. 等待首次采集
sleep 8 && ls /var/lib/catmonitor/snapshot/

# 4. 启动 dfee exporter
./dfee -addr=:19323 -snapshot-dir=/var/lib/catmonitor/snapshot -exporter=enabled -exporter-port=9333 &

# 5. 启动 Prometheus
./prometheus --config.file=prometheus.yml &

# 6. 启动 Grafana
systemctl start grafana-server

# 7. 浏览器导入 grafana-dashboard.json
# http://localhost:3000 → Dashboards → Import → Upload JSON
```

## 8. 指标说明

### CPU 指标

| Prometheus 指标 | 说明 |
|----------------|------|
| `node_cpu_seconds_total{mode}` | CPU 累计时间（user/nice/system/idle/iowait/irq/softirq/steal） |
| `node_load1/5/15` | 系统负载 |
| `node_cpu_cores_online` | 在线核心数 |

### Memory 指标

| Prometheus 指标 | 说明 |
|----------------|------|
| `node_memory_MemTotal_bytes` | 总内存 |
| `node_memory_MemFree_bytes` | 空闲内存 |
| `node_memory_Buffers_bytes` | Buffers |
| `node_memory_Cached_bytes` | Cached |
| `node_memory_SReclaimable_bytes` | SReclaimable |
| `node_memory_SwapTotal_bytes` | Swap 总量 |
| `node_memory_SwapFree_bytes` | Swap 空闲 |

### Network 指标

| Prometheus 指标 | 说明 |
|----------------|------|
| `node_network_receive_bytes_total{interface}` | 接收字节数 |
| `node_network_transmit_bytes_total{interface}` | 发送字节数 |

### Disk 指标

| Prometheus 指标 | 说明 |
|----------------|------|
| `node_disk_read_sectors_total{device}` | 读扇区总数 |
| `node_disk_written_sectors_total{device}` | 写扇区总数 |
| `node_disk_read_time_seconds_total{device}` | 读耗时（秒） |
| `node_disk_write_time_seconds_total{device}` | 写耗时（秒） |

### NPU 指标

| Prometheus 指标 | 说明 |
|----------------|------|
| `dsmi_aicore_utilization_percent{npu_id,chip_id}` | AICore 利用率 |
| `dsmi_hbm_utilization_percent{npu_id,chip_id}` | HBM 利用率 |
| `dsmi_npu_utilization_percent{npu_id,chip_id}` | NPU 整体利用率 |
| `dsmi_power_w{npu_id,chip_id}` | NPU 功耗 |
| `dsmi_voltage_mv{npu_id,chip_id}` | NPU 电压 |
| `dsmi_hbm_frequency_hz{npu_id,chip_id}` | HBM 频率 |
| `dsmi_aicore_current_frequency_hz{npu_id,chip_id}` | AICore 频率 |
| `dsmi_vector_utilization_percent{npu_id,chip_id}` | Vector Core 利用率 |
| `dsmi_hbm_bandwidth_utilization_percent{npu_id,chip_id}` | HBM 带宽利用率 |

### Chassis 指标

| Prometheus 指标 | 说明 |
|----------------|------|
| `ipmi_power_w` | 整机功耗 |
| `ipmi_inlet_temp_celsius` | 进风口温度 |
| `ipmi_outlet_temp_celsius` | 出风口温度 |
| `ipmi_fan_speed_rpm{fan_id}` | 风扇转速 |
