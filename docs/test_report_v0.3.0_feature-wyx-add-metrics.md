# CATMonitor 测试报告

> **项目**: CATMonitor (Computing Availability Tools Monitor)  
> **版本**: v0.3.0（NPU 指标扩展 + device 并行 + GPU 接入来源层）  
> **日期**: 2026-07-14  
> **测试执行**: 自动化 Go testing 框架 + mock 驱动

---

## 1. 测试概述

### 1.1 测试目标

验证 v0.3.0 NPU 指标扩展与 GPU 迁移的完整功能，包括：

- **NPU 来源层 3 包**：DCMI(CGo)、npu_smi、hccn_tool 的解析、缓存、超时、可用性检测
- **NPU collector 重构**：5→74 指标，既有 5 改 DCMI，新增 69 指标，device 并行采集
- **GPU 迁移**：新建 nvidia_smi 来源包，gpu collector 从内联 exec 改为调来源层
- 跨平台编译（Linux + Windows）与零回归

### 1.2 测试结果汇总

| 指标 | 结果 |
|------|------|
| 测试总数（全量） | **149**（含昨天的 v0.2.0 测试） |
| 其中今天新增/重写 | **23**（dcmi 3 + npu_smi 3 + hccn_tool 3 + nvidia_smi 4 + npu 6 + gpu 4） |
| 通过 | **149** |
| 失败 | **0** |
| 通过率 | **100%** |
| `go build ./...` | 通过（Linux） |
| `GOOS=windows go build ./...` | 通过（Windows 交叉编译） |
| `go vet ./...` | 通过，零警告 |

---

## 2. 测试环境

| 项目 | 配置 |
|------|------|
| 操作系统 | Linux (WSL2, x86_64) |
| CPU | Intel Core i7-14700 (28 逻辑核) |
| Go 版本 | go1.23.4 linux/amd64 |
| 外部依赖 | gopkg.in/yaml.v3 v3.0.1 |
| 测试框架 | Go 原生 testing |
| NPU/CANN | 无（DCMI 走 mock 测试） |
| nvidia-smi | 无（走 mock 测试） |

> 注：该环境无 NPU 硬件、无 CANN SDK、无 nvidia-smi，所有 NPU/GPU 测试由 mock 驱动。

---

## 3. 来源层新增 3 包验证

### 3.1 DCMI 来源（CGo）

| 文件 | 说明 | 测试 |
|------|------|:---:|
| `dcmi.go` | 接口 + 7 Go struct + defaultSource delegation | ✅ |
| `dcmi_cgo.go` | CGo 绑定 22 个 dcmi_* 函数，`//go:build cgo && linux && dcmi` | 排除编译 |
| `dcmi_mock.go` | MockProvider（map 索引，逐字段 mock） | ✅ |
| `dcmi_test.go` | 3 测试 | ✅ |

| 测试 | 验证点 | 结果 |
|------|--------|:----:|
| TestNotAvailableWithoutCGo | 无 CGo 时 Available()=false、所有方法返回 errNotAvailable | PASS |
| TestMockProvider | 9 项 DCMI 方法（Temperature/Power/HbmInfo/UtilizationRate/Frequency/EccInfo/ChipInfo/DriverVersion/LlcPerf/CardList）mock 返回值正确 | PASS |
| TestMockMissing | 未设 mock 的方法返回 errNotAvailable | PASS |

覆盖率：30.9%（CGo 文件排除，mock + delegation 部分覆盖）。

### 3.2 npu_smi 来源

| 测试 | 验证点 | 结果 |
|------|--------|:----:|
| TestTopo | Topo() 解析 topo 输出 | PASS |
| TestTopoCachesAcrossCalls | Topo 常驻缓存（调 2 次 exec 1 次） | PASS |
| TestHccsBandwidth | HccsBandwidth(0) 解析 TX/RX | PASS |

覆盖率：70.0%。

### 3.3 hccn_tool 来源

| 测试 | 验证点 | 结果 |
|------|--------|:----:|
| TestBandwidth | 解析 NetTX/RX/PcieTX/RX 4 路 | PASS |
| TestSpeed | 解析 "100Gbps" | PASS |
| TestLink | 解析 "ACTIVE" | PASS |

覆盖率：71.7%。per-devID:opt 复合缓存 key 修复验证通过。

---

## 4. NPU collector 重构验证

### 4.1 74 指标覆盖

| 测试 | 验证点 | 结果 |
|------|--------|:----:|
| TestCollectDevice | device 0 产出 40+ 指标，断言 20+ 关键指标值 | PASS |
| TestCollectEccDelta | 两次 ECC 快照差值（3→5, delta=2） | PASS |
| TestCollectIntegration | 全量 Collect：静态 + 并行 2 device，断言 9 个 name 存在 | PASS |
| TestCollectParallelMultiDevice | 2 device 并行，device 0 指标存在 | PASS |
| TestNoDCMIAvailable | 无 DCMI 时 Collect 不报错，输出无 DCMI 指标 | PASS |
| TestCollectorInterface | Name/Component/Priority/Interval/Enabled | PASS |

覆盖率：**96.0%**。

### 4.2 device 并行采集验证

| 验证点 | 结果 |
|--------|:----:|
| 2 device goroutine 并行，WaitGroup 等齐 | ✅ |
| 单 device 失败不影响其他 device | ✅（TestNoDCMIAvailable） |
| 静态指标（npu_num/comm_topo/driver_version/chip_type）仅采 1 次 | ✅ |
| ECC delta 跨周期正确（prev 状态在 mutex 保护下） | ✅ |

### 4.3 74 指标实现完整性

| 组 | 指标数 | 来源 | 实现 |
|----|:------:|------|:----:|
| 既有 5（改 DCMI） | 5 | dcmi | ✅ |
| 基础信息 | 8 | dcmi + npu_smi | ✅ |
| 电压/风扇 | 7 | dcmi | ✅ |
| 温度(13 路) | 13 | dcmi | ✅ |
| 频率(7) | 7 | dcmi | ✅ |
| 利用率(12) | 12 | dcmi | ✅ |
| HBM 内存 | 2 | dcmi | ✅ |
| ECC(8) | 8 | dcmi(delta) | ✅ |
| LLC(3) | 3 | dcmi | ✅ |
| 带宽/网络(9) | 9 | hccn_tool + npu_smi | ✅ |
| **合计** | **74** | | ✅ |

---

## 5. GPU 迁移验证

### 5.1 nvidia_smi 来源包

| 测试 | 验证点 | 结果 |
|------|--------|:----:|
| TestParseCSVLine | CSV 9 字段解析 | PASS |
| TestParseOutput | 2 GPU 解析，值正确 | PASS |
| TestQueryWithMock | Query() mock 注入 | PASS |
| TestQueryEmpty | 空输入返回空 | PASS |

覆盖率：69.7%。

### 5.2 GPU collector 迁移

| 测试 | 验证点 | 结果 |
|------|--------|:----:|
| TestCollectWithMock | 2 GPU × 9 指标 = 18 条，Component=gpu | PASS |
| TestCollectEmpty | 空 Query 返回 0 指标 | PASS |
| TestCollectorInterface | 接口契约 | PASS |
| TestRoundFloat | 浮点精度 | PASS |

覆盖率：**97.0%**。

---

## 6. 代码质量

| 检查项 | 结果 |
|--------|:----:|
| `go build ./...` | ✅ |
| `GOOS=windows go build ./...` | ✅ |
| `go vet ./...` | ✅ 零警告 |
| 外部依赖 | 仅 yaml.v3（无新增） |
| CGo 隔离 | dcmi_cgo.go `//go:build cgo && linux && dcmi`，默认编译排除 |
| 非 Linux 隔离 | npu_other.go `//go:build !linux` |

---

## 7. 期间发现并修复的缺陷

1. **hccn_tool 缓存 key 互相覆盖**：原按 devID 缓存，不同 opt（-bandwidth/-speed/-link）返回错误结果。修复为 `devID:opt` 复合 key。
2. **npu_smi/hccn_tool Available() 门控**：collector 用 `Available()`（LookPath）门控命令指标，但测试环境 mock 设了 fetcher 却无真命令→Available()=false→跳过。去掉门控，直接调+处理 error。

---

## 8. 已知限制

1. **DCMI CGo 未真机验证**：`dcmi_cgo.go` 在 `dcmi` build tag 后，本机无 CANN SDK 无法编译。需在真 NPU 服务器 `go build -tags dcmi` 验证。
2. **DCMI 原始单位待实测**：voltage(V/mV)、temperature(°C/毫摄氏度)、llc hit_rate(%/小数)等需真机对照 `npu-smi info` 反推。
3. **GPU 无真机验证**：本机无 nvidia-smi，所有测试 mock 驱动。
4. **device 并行未在真多卡环境验证**：mock 2 device，真 8 卡并发行为待真机验证。

---

## 9. 结论

CATMonitor v0.3.0 完成 **NPU 指标扩展(5→74) + device 并行采集 + GPU 接入来源层**。今天新增/重写 23 个测试全部通过，全量 149 个测试通过，Linux/Windows 双平台编译通过，`go vet` 零警告。NPU 是项目第一个 device 并行 + CGo collector，GPU 是最后一个接入来源层的 collector。来源层从 v0.2.0 的 10 个包扩展到 14 个。

**测试结论：全部通过，v0.3.0 NPU 扩展与 GPU 迁移可用。**

---

*测试执行时间: 2026-07-14*  
*测试执行人: Automated (OpenCode + Go testing framework)*
