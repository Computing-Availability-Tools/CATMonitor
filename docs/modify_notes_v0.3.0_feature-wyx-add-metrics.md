# CATMonitor 修改说明 (v0.3.0)

> **版本**: v0.3.0  
> **日期**: 2026-07-14  
> **范围**: NPU 指标扩展(5→74) + device 并行采集 + DCMI CGo 来源 + GPU 接入来源层  
> **回归**: 全量 149 测试通过，Linux/Windows 双平台编译通过，`go vet` 零警告

---

## 一、变更总览

| 维度 | 变更 |
|------|------|
| 新增来源包 | 4 个：dcmi、npu_smi、hccn_tool、nvidia_smi |
| NPU 指标 | 5 → 74（+69，含既有 5 改 DCMI） |
| NPU 特性 | **device 并行采集**（collector 层 goroutine 化） |
| NPU CGo | DCMI 通过 `//go:build cgo && linux && dcmi` 隔离，`-tags dcmi` 启用 |
| GPU 迁移 | 从内联 exec 改为调 nvidia_smi 来源包（最后一个接入来源层的 collector） |
| 来源层总数 | 10 → 14 个包 |
| 总指标 | 83 → 152 |
| 外部依赖 | 无新增 |

---

## 二、新增来源包

### `internal/source/dcmi/`（CGo，4 文件）

| 文件 | 说明 |
|------|------|
| `dcmi.go` | Source 接口(22 方法) + 7 Go struct(ChipInfo/HbmInfo/EccInfo/LlcPerf/AicpuInfo/ResourceInfo/DvppRatio) + defaultSource delegation + Available() |
| `dcmi_cgo.go` | `//go:build cgo && linux && dcmi`，`#cgo LDFLAGS: -ldcmi`，实现 FetchProvider（22 个 dcmi_* CGo 调用），init() 注册 |
| `dcmi_mock.go` | MockProvider（map 索引逐字段 mock，实现 FetchProvider 全接口） |
| `dcmi_test.go` | 3 测试（NotAvailableWithoutCGo / MockProvider 9 方法 / MockMissing） |

**设计**：
- FetchProvider 接口 seam：CGo 实现 + mock 实现，`Default().Available()` = provider != nil
- 默认构建（无 `-tags dcmi`）：CGo 文件排除，Available()=false，所有方法返回 errNotAvailable → 优雅降级
- NPU 服务器：`go build -tags dcmi ./...` 启用 CGo 绑定
- 无缓存（DCMI 是进程内 CGo 调用，无 fork/exec，比命令快）

### `internal/source/npu_smi/`（exec，2 文件）

| 文件 | 说明 |
|------|------|
| `npu_smi.go` | Source 接口：Topo() / HccsBandwidth(devID) / Available()；fetcher 注入；Topo 常驻缓存(sync.Once)；5s exec 超时 |
| `npu_smi_test.go` | 3 测试（Topo 解析 + 常驻缓存 + HccsBandwidth 解析） |

### `internal/source/hccn_tool/`（exec，2 文件）

| 文件 | 说明 |
|------|------|
| `hccn_tool.go` | Source 接口：Bandwidth(devID) → {NetTX,NetRX,PcieTX,PcieRX}、Speed(devID)、Link(devID)；fetcher 注入；per-devID:opt 30s 缓存 + 失败缓存；5s 超时 |
| `hccn_tool_test.go` | 3 测试（Bandwidth 4 路解析 + Speed + Link） |

**Bug 修复**：缓存 key 原为 `devID`（不同 opt 互相覆盖），改为 `devID:opt` 复合 key。

### `internal/source/nvidia_smi/`（exec，2 文件）

| 文件 | 说明 |
|------|------|
| `nvidia_smi.go` | Source 接口：Query() → []GPU(一次 exec 取全 GPU 9 字段)；GPU struct(Index/Utilization/MemUsed/MemTotal/Temperature/Power/FanSpeed/EccErrors/ClockFreq)；fetcher 注入；5s 超时；无缓存(指标需新鲜) |
| `nvidia_smi_test.go` | 4 测试（CSV 解析 + 输出解析 + mock 注入 + 空输入） |

---

## 三、NPU collector 重构（5→74 指标 + device 并行）

### 文件变更

| 文件 | 改动 |
|------|------|
| `npu/npu.go` | 重写：结构体(deviceIDs/prevEcc/staticCollected)、**device 并行 Collect()**(每 device 一个 goroutine + WaitGroup)、init |
| `npu/npu_linux.go`（新） | ensureDevices(dcmi.CardList)、collectStatic(npu_num/comm_topo/driver_version/chip_type，启动1次)、collectDevice(devID, now)(全部 74 指标)、emitEccMetrics(delta)、DCMI 常量(freq/rate/sensor/main_cmd/sub_cmd) |
| `npu/npu_other.go`（新） | `//go:build !linux` no-op stub(ensureDevices/collectStatic/collectDevice) |
| `npu/npu_test.go` | 重写 6 测试(DCMI mock + npu_smi/hccn_tool mock 注入) |

### device 并行设计

```
Collect() {
    Phase 1: collectStatic(now)   // 全局/静态指标，采1次
    Phase 2: for each deviceID {
        go collectDevice(devID, now)  // 每 device 一个 goroutine
    }
    wg.Wait()  // 等齐
    merge results
}
```

- 并行在 **collector 层**（来源层保持单 device 接口，简单可测）
- 单卡失败不影响其他卡（goroutine 独立，error 静默跳过）
- ECC delta 用 mutex 保护 prevEcc map

### 既有 5 改 DCMI

| 指标 | 旧来源 | 新来源 |
|------|--------|--------|
| utilization | npu-smi info | DCMI dcmi_get_device_utilization_rate(AICORE) |
| memory_usage | npu-smi info | DCMI dcmi_get_device_hbm_info(memory_usage/memory_size×100) |
| temperature | npu-smi info | DCMI dcmi_get_device_temperature |
| power_draw | npu-smi info | DCMI dcmi_get_device_power_info |
| health_status | npu-smi info | DCMI dcmi_get_device_health |

### 74 指标分布

| 组 | 指标数 | 来源 |
|----|:------:|------|
| 既有 5（改 DCMI） | 5 | dcmi |
| 基础信息 | 8 | dcmi + npu_smi(-t topo) |
| 电压/风扇 | 7 | dcmi(DeviceInfo LP) |
| 温度(13 路) | 13 | dcmi(SensorInfo) |
| 频率(7) | 7 | dcmi(Frequency/AicpuInfo) |
| 利用率(12) | 12 | dcmi(UtilizationRate/DvppRatio) |
| HBM 内存 | 2 | dcmi(HbmInfo) |
| ECC(8) | 8 | dcmi(EccInfo, delta) |
| LLC(3) | 3 | dcmi(LlcPerf) |
| 带宽/网络(9) | 9 | hccn_tool + npu_smi(-t hccs-bw) + dcmi(NetworkHealth) |
| **合计** | **74** | |

---

## 四、GPU collector 迁移（最后一个接入来源层）

| 文件 | 改动 |
|------|------|
| `gpu/gpu.go` | 重写：删 smiPath/available/mockOutput 字段 + SetMockOutput/SetAvailable 方法；Collect() 调 `nvidia_smi.Default().Query()` 取 []GPU 遍历构建 9 指标 |
| `gpu/gpu_test.go` | 重写：用 `nvidia_smi.SetMock(testdata)` 注入；4 测试 |

### 迁移前后对比

| 维度 | 迁移前 | 迁移后 |
|------|--------|--------|
| 数据获取 | 内联 `exec.Command("nvidia-smi", ...)` | `nvidia_smi.Default().Query()` |
| 解析逻辑 | 在 collector(parseOutput/parseCSVLine/parseFloat) | 在来源包(parseOutput/parseCSVLine/parseFloat) |
| Mock | `SetMockOutput(s)` + `SetAvailable(b)` | `nvidia_smi.SetMock(out)` |
| 行为 | 不变(7 指标, 2 GPU×9=18 条) | 不变 |

---

## 五、测试数据(testdata)

| 文件 | 用途 |
|------|------|
| `tests/testdata/npu-smi-topo-output.txt` | npu_smi Topo() 测试 |
| `tests/testdata/npu-smi-hccs-bw-output.txt` | npu_smi HccsBandwidth() 测试 |
| `tests/testdata/hccn-tool-bandwidth-output.txt` | hccn_tool Bandwidth() 测试 |
| `tests/testdata/hccn-tool-speed-output.txt` | hccn_tool Speed() 测试 |
| `tests/testdata/hccn-tool-link-output.txt` | hccn_tool Link() 测试 |

---

## 六、缺陷修复

1. **hccn_tool 缓存 key bug**：缓存按 devID 索引，Bandwidth/Speed/Link 三种 opt 互相覆盖。修复为 `strconv.Itoa(devID) + ":" + opt` 复合 key。
2. **Available() 门控**：npu_smi/hccn_tool 的 `Available()`(LookPath)在测试环境返回 false（无真命令），即使 mock 设了 fetcher 也被跳过。去掉 collector 里的 `if Available()` 门控，直接调+处理 error。

---

## 七、文档

| 文件 | 改动 |
|------|------|
| `docs/CATMonitor_indi_list.md` | NPU 小节 5→74（表格 74 行 + 采集方法 + 74 条逐条指标详情），汇总/附录 A/附录 B/统计汇总更新，合计 83→152 |
| `docs/test_report_v0.3.0_feature-wyx-add-metrics.md` | 本版系统测试报告 |
| `/mnt/d/wyx/doc_metrics/NPU_metrics.md` | NPU 指标清单(74 + 4 决策回填) |

---

## 八、架构决策（已确认）

| # | 决策 | 结论 |
|---|------|------|
| Q1 | DCMI 实现 | A: 全 CGo，`//go:build cgo && linux && dcmi` 隔离 |
| Q2a | RoCE 链路 | b: 保留两个，roce_link_status(DCMI) + roce_link_health(hccn_tool) |
| Q2b | ECC 命名 | b: 保留 single/double，贴合 DCMI 术语 |
| Q3 | 字符串指标 | a: 塞 labels，value 填 0/关联数值 |
| Q4 | DCMI 单位 | c: 文档推断 + 标待实测 |
| 确认1 | device 并行位置 | B: collector 层 |
| 确认2 | 命令类来源 | 拆 npu_smi + hccn_tool 两个包 |

---

## 九、当前来源层完整清单（14 包）

| 包 | 数据源 | 服务于 |
|---|---|---|
| source | 通用接口 | — |
| proc | /proc | cpu, memory, disk, network |
| sys | /sys | cpu, memory, disk, network |
| ipmi | ipmitool | cpu, memory |
| lscpu | lscpu | cpu |
| mce | mcelog/dmesg | cpu |
| dmesg | dmesg | memory, disk |
| dmidecode | dmidecode | memory |
| statfs | statfs(2) | disk |
| smartctl | smartctl | disk |
| **dcmi** | libdcmi.so(CGo) | **npu** |
| **npu_smi** | npu-smi -t | **npu** |
| **hccn_tool** | hccn_tool | **npu** |
| **nvidia_smi** | nvidia-smi | **gpu** |

**全部 6 个 collector 已接入来源层。**

---

## 十、未做 / 后续

- **DCMI CGo 真机验证**：需在 NPU 服务器 `go build -tags dcmi` 编译 + 实测单位
- **health 扣分扩展**：NPU ECC/温度扣分规则
- **DESIGN.md / README.md 更新**：来源层 14 包架构、152 指标、device 并行、CGo 隔离
- **SPEC.md 更新**：NPU CGo 例外说明
- **Git 提交**：今天的改动尚未 commit（昨天的 b114848 只含 v0.2.0）

---

*所有改动在本地工作树，未提交 git。*
