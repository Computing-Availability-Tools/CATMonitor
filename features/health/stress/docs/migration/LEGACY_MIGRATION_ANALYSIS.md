# 旧健康检查压测功能迁移洞察报告

> 文档类型：历史迁移分析，不是当前操作手册。本文保留旧项目行为和迁移
> 依据，其中脚本、状态或范围描述可能已被后续实现取代。当前只支持
> STREAM、HPL、HPCG；现行契约以 [STRESS_SPEC.md](../../STRESS_SPEC.md)
> 和 [STRESS_DESIGN.md](../../STRESS_DESIGN.md) 为准。

日期: 2026-07-24
分析范围: STREAM、HPL、HPCG 压测功能（L2 深度检测）
目标: 为 Go 版本重新实现提供功能约束和迁移决策依据

---

## 1. 文档目的和迁移边界

### 1.1 文档目的

本文档记录旧 C 版健康检查中 STREAM、HPL、HPCG 三大压测功能的**完整业务能力和关键实现约束**，使后续 Go 开发人员在无法查看旧 C 源码的情况下，能够：

- 理解旧版提供了什么压测能力
- 知道每个压测实际做了什么、依赖什么、怎么判定结果
- 明确哪些行为必须保留，哪些实现不应照搬
- 知道哪些参数需要按本地环境适配

### 1.2 文档中三类陈述的标识

本文档将每项内容标记为以下三类之一：

- **旧实现事实** — 由旧源码、脚本或配置直接证明
- **基于源码推断** — 根据多个代码位置推导，但未经实际运行验证
- **迁移建议** — 不属于旧功能事实，而是 Go 版本应如何改进

### 1.3 迁移边界

| 范围 | 包含                                                         | 不包含                                                |
| ---- | ------------------------------------------------------------ | ----------------------------------------------------- |
| 功能 | CLI 触发、L1/L2 流程、benchmark 选择、Shell 执行命令、结果解析、成功/失败判定 | OS 部署脚本、配置文件格式、日志系统、健康检查评分模型 |
| 代码 | 关键执行命令、解析逻辑、依赖声明                             | 完整源码、全部头文件、辅助函数、内存释放、错误日志    |
| 实现 | 功能行为约束、必须保留的用户接口语义                         | 函数名、目录结构、进程控制方式、C 语言特性            |

---

## 2. 压测能力总览

### 2.1 三种压测能力对比

| 维度                 | STREAM                                | HPL                                         | HPCG                                                         |
| -------------------- | ------------------------------------- | ------------------------------------------- | ------------------------------------------------------------ |
| **用途**             | 内存带宽测试                          | 高性能 Linpack（浮点计算性能）              | 共轭梯度法（真实应用模拟）                                   |
| **触发**             | catcli check-health -l L2 -b stream   | catcli check-health -l L2 -b hpl            | catcli check-health -l L2 -b hpcg                            |
| **默认是否启用**     | 是（默认集合包含）                    | 是（默认集合包含）                          | 是（默认集合包含）                                           |
| **执行工具**         | stream_c.exe + numactl                | xhpl + mpirun                               | xhpcg + mpirun                                               |
| **依赖 MPI**         | 否                                    | 是 (OpenMPI)                                | 是 (OpenMPI)                                                 |
| **依赖 OpenMP**      | 是（线程数通过 OMP_NUM_THREADS 设置） | 是（命令中固化 -x OMP_NUM_THREADS=32）      | 命令未设置 OMP_NUM_THREADS，映射参数为 pe=1                  |
| **输出来源**         | stdout                                | stdout                                      | 解析函数含 stdout 与结果文件双路径；结构化性能结果主要依赖 HPCG-Benchmark\*.txt 文件解析 |
| **正确性判断**       | 无（仅收集性能数据）                  | 无（不检查 residual，不检查 PASSED/FAILED） | 检查完整字符串 "HPCG result is VALID with a GFLOP/s rating of=" |
| **性能判断**         | 无阈值检查 (all_passed 始终为 true)   | 解析 Gflops 但不设阈值                      | 解析 Gflops 但不设阈值                                       |
| **二进制路径配置**   | STREAM_BIN_PATH（目录）               | HPL_BIN_PATH（目录）                        | HPCG_BIN_PATH（目录）                                        |
| **是否需要配置文件** | 否                                    | HPL.dat（预部署，代码不生成）               | 旧启动命令未显式传参；是否读取工作目录中的默认配置文件取决于实际 xhpcg 版本，需动态验证 |
| **root 提权**        | seteuid(0)                            | seteuid(0) + --allow-run-as-root            | seteuid(0) + --allow-run-as-root                             |
| **迁移复杂度**       | 低                                    | 高（MPI 参数复杂）                          | 中（结果文件处理特殊）                                       |

### 2.2 默认 benchmark 集合

**旧实现事实：**

```c
// cli_constants.h:45-48
#define ALL_DFX_BENCH            "hpl,hpcg,stream,osu"
#define ALL_C2_BENCH             "onchip-memtester,hpl,hpcg,stream,osu"
#define DEFAULT_BENCH            "onchip-memtester,hpl,hpcg,stream"  // 生产环境
#define DEFAULT_NON_PROD_BENCH   "hpl,hpcg,stream"                   // 非生产环境
```

关键规则：

- `-b all` → 展开为 `ALL_C2_BENCH`（生产）或 `ALL_DFX_BENCH`（非生产）
- `-b` 参数中 `onchip-memtester` 始终移到列表最前执行（`param_check_builder.c` 中的排序逻辑）
- 默认执行顺序：**onchip-memtester → hpl → hpcg → stream**
- `-b` 不传时 default 集合由 `check_health_params_init()` 根据 `is_prod_env` 决定

**迁移建议：** 默认集合和默认顺序建议继续保持旧版兼容，但需产品确认。Go 版本应将默认 benchmark 列表设计为可配置项。

---

## 3. 健康检查整体流程

### 3.1 L1/L2 执行顺序

**旧实现事实：**

```
CLI 请求: catcli check-health -l L2 [-b bench_list]
  │
  ├─ 1. L1 基础健康检查 (CmdHealthInfo)
  │   ├─ CPU 健康: 离线核、静默数据错误、CE/UCE、温度
  │   ├─ DDR 健康: 内存页隔离、CE/UCE、HBM 状态
  │   └─ 输出: health_score (初始 100.0, 阈值 75.0)
  │
  ├─ 2. 判断 L2 是否可以执行
  │   └─ 条件: g_l1_is_check_pass == true
  │       └─ 未通过 → 跳过所有压测，直接输出 L1 结果
  │
  ├─ 3. L2 压测执行 (CmdHealthBenchmark)
  │   ├─ 设置 LD_LIBRARY_PATH (SetLibraryPathConf)
  │   ├─ 设置 KML 环境变量 (InitPathEnv, 仅生产)
  │   ├─ 解析 benchmark 列表 (ParseModuleList)
  │   ├─ 获取 CPU 基本信息 (GetBasicModulesInfo: lscpu)
  │   └─ 逐个执行压测
  │       ├─ 单项失败 → 继续下一项 (不短路)
  │       └─ 记录 anyFailure 标志
  │
  ├─ 4. L1 再次检查 (CmdHealthInfo)
  │   └─ 目的: 压测后重新验证硬件健康状态
  │
  └─ 5. 输出最终结果
      ├─ 每个 benchmark 的 status/message
      ├─ health_condition: "Healthy"/"Unhealthy"
      ├─ health_score: 见下文第 10 章说明
      └─ exit_code: 见下文第 10 章说明
```

### 3.2 关键行为规则

**旧实现事实：**

| 问题                           | 答案                                                         |
| ------------------------------ | ------------------------------------------------------------ |
| 什么命令触发 L2？              | `catcli check-health -l L2`                                  |
| `-b` 是否支持单项和多项？      | 是，逗号分隔 (如 `-b hpl,stream`)                            |
| 不传 `-b` 时默认执行哪些       | 生产: `onchip-memtester,hpl,hpcg,stream`；非生产: `hpl,hpcg,stream` |
| 默认执行顺序是什么？           | onchip-memtester → hpl → hpcg → stream                       |
| L1 不通过时是否跳过压测？      | 是，完全跳过 L2                                              |
| 单 benchmark 失败后是否继续？  | 是，继续执行下一项                                           |
| 压测后为什么再次执行基础检查？ | 检测压测是否诱发了硬件故障                                   |
| 最终结果如何返回？             | stdout JSON，包含 per-benchmark 对象                         |

**旧实现证据：**

- 文件: `src/catcli_process/catcli_process.c:23-168` (`ProcModuleHealthCheck`)
- 文件: `src/catcli_process/depth_detection.c:764-860` (`ProcessModulesBenchmark`)
- 流程: L2 分支中 `CmdHealthInfo()` → if `g_l1_is_check_pass` → `CmdHealthBenchmark()` → `CmdHealthInfo()` 再执行

### 3.3 迁移建议

- 建议保留 L1 → L2 → L1 再检查的三段流程
- 必须支持选择单项/多项 benchmark
- 必须支持默认 benchmark 集合
- benchmark 之间独立执行，单项失败不终止整体
- Go 版本不需要保留 `g_level_status`/`g_level_count`/`g_l1_is_check_pass` 这些全局变量的实现方式

---

## 4. STREAM 功能洞察

### 4.1 功能目的

**旧实现事实：**

- STREAM 在健康检查中用于**验证内存带宽性能**
- 采集四项操作（Copy/Scale/Add/Triad）的 Best Rate MB/s
- **旧版不基于带宽值做健康判定**，仅记录和展示性能数据

### 4.2 旧版执行方式

**旧实现事实：**

**C 构造命令** (`depth_detection.c:57-59`):

```c
snprintf_s(command, ..., "cd %s && bash benchmark_check.sh %s %s %d 2>&1",
    GetThirdPartyPath(), "stream", binaryPath, coresPerNuma);
// 例: cd /batch/agent/tools/cathelper/thirdparty &&
//     bash benchmark_check.sh stream /opt/benchmarks/stream 32 2>&1
```

`binaryPath` 是 `g_config.stream_conf.stream_path`，即配置项 `STREAM_BIN_PATH` 的值。

**Shell 脚本接收** (`benchmark_check.sh:32-39`):

```bash
stream)
    core_num=$1                    # coresPerNuma = total_cores / numa_count
    export OMP_NUM_THREADS=$core_num
    output=$(numactl --localalloc "$path"/stream_c.exe)
    echo "$output"
    ;;
```

`path=$2` 即 `STREAM_BIN_PATH` 的目录值，拼接 `"$path"/stream_c.exe` 得到完整二进制路径。

**实际执行命令:**

```bash
export OMP_NUM_THREADS=<cores_per_numa>
numactl --localalloc <STREAM_BIN_PATH>/stream_c.exe
```

例如: `STREAM_BIN_PATH=/opt/benchmarks/stream` → 执行 `/opt/benchmarks/stream/stream_c.exe`

**关键事实:**

- `STREAM_BIN_PATH` 配置项是包含 `stream_c.exe` 的**目录路径**，脚本自动拼接 `/stream_c.exe`
- `stream_c.exe` 是固定文件名（不通过配置指定）
- 线程数 = `total_cores / numa_count`，来自 `lscpu` 解析结果（`GetBasicModulesInfo`）
- 使用 `numactl --localalloc` 设置本地优先的内存分配策略，**但未指定 `--cpunodebind` 或 `--membind`**，因此未绑定到某个明确 NUMA 节点
- **不显式绑定 CPU affinity**
- **只执行一次**（未发现逐 NUMA 节点循环）
- 工作目录被 C 代码 `cd` 到 `GetThirdPartyPath()`，即 `<base>/thirdparty`；Shell 命令在该目录下执行
- stdout 和 stderr 通过 `2>&1` 合并采集

### 4.3 STREAM 输出解析

**旧实现事实：**

**预期输出格式:**

```
Function    Best Rate MB/s  Avg time      Min time      Max time
Copy:           12345.6     0.123         0.100         0.150
Scale:          12345.6     0.123         0.100         0.150
Add:            12345.6     0.123         0.100         0.150
Triad:          12345.6     0.123         0.100         0.150
Solution Validates: ...
```

**解析规则** (`depth_detection.c:123-170`, `ParseStreamOutput`):

1. 找到同时包含 "Function" 和 "Best Rate MB/s" 的行
2. 后续每行用 `sscanf_s(line, "%15s %lf %lf %lf %lf", func, &best, &avg, &min, &max)` 解析
3. 要求四项全部出现 (Copy, Scale, Add, Triad)
4. "Solution Validates" 行出现时提前停止解析
5. 解析成功条件: `*resultCount >= STREAM_RESULT_NUM(4)`

**采集的指标（每项）:**

- function[15]: 如 "Copy:" (包含冒号)
- bestRate (double): Best Rate MB/s
- avgTime (double): Average time
- minTime (double): Minimum time
- maxTime (double): Maximum time

**成功/失败判定:**

```c
bool all_passed = true;   // ← 声明即赋值，后续整个函数中从未被修改
result->success = true;   // ← 始终为 true（只要命令执行和解析成功）
```

**旧实现事实结论：** STREAM 的成功条件是：命令执行成功 + 输出能正确解析出四项结果。不检查带宽阈值。`result->success` 始终为 `true`，不会有 `false` 的情况。

### 4.4 迁移建议

| 能力                                      | 建议等级       | 说明                                                         |
| ----------------------------------------- | -------------- | ------------------------------------------------------------ |
| 外部命令执行 (numactl + stream_c.exe)     | 必须           | 核心执行方式                                                 |
| 线程数配置 (OMP_NUM_THREADS)              | 必须           | 默认 total_cores/numa_count，但应可覆盖                      |
| NUMA 策略 (--localalloc 类似语义)         | 建议           | 内存带宽测试需要 NUMA-aware；是否保留 --localalloc、改成逐 NUMA 测试或允许绑定节点，应列为本地适配或产品决策，不是全部标记"必须" |
| 四项结果结构化输出 (Copy/Scale/Add/Triad) | 必须           | 旧接口的语义契约                                             |
| 超时和取消                                | **迁移优化**   | 旧版无超时，会永久阻塞                                       |
| 进程清理                                  | 必须           | 无论成功/失败都要回收子进程                                  |
| 输出格式异常检测                          | 必须           | 旧版已实现                                                   |
| 带宽阈值判断                              | **新产品决策** | 旧版不做，Go 版本是否需要由产品确定                          |

---

## 5. HPL 功能洞察

### 5.1 功能目的

**旧实现事实：**

- HPL 用于测量**浮点计算性能**（Gflops）
- 旧健康检查实际**只提取 Gflops 数值，不验证数值正确性**
- 不检查 `||Ax-b||` residual
- 不检查最终的 `PASSED`/`FAILED` 标记

### 5.2 旧版执行方式

**旧实现事实：**

**Shell 脚本完整命令** (`benchmark_check.sh:17-22`):

```bash
hpl)
    cd "$path"                # $path = HPL_BIN_PATH (xhpl 所在目录)
    mpirun --allow-run-as-root \
           --oversubscribe \
           -x OMP_NUM_THREADS=32 \
           --map-by ppr:16:node:pe=32 \
           -x PATH \
           -x LD_LIBRARY_PATH \
           -x UCX_TLS=self,sm \
           -mca pml ucx \
           -mca btl ^vader,tcp,openib,uct \
           ./xhpl
    ;;
```

**C 代码构造命令** (`depth_detection.c:575-577`):

```c
snprintf_s(command, ..., "cd %s && bash benchmark_check.sh %s %s 2>&1",
    GetThirdPartyPath(), "hpl", params->binary_path);
// 例: cd /batch/agent/tools/cathelper/thirdparty &&
//     bash benchmark_check.sh hpl /opt/benchmarks/hpl 2>&1
```

**关键事实:**

- `HPL_BIN_PATH` 配置项指向包含 `xhpl` 和预期 `HPL.dat` 的**目录**（脚本中 `cd "$path"` 进入该目录后运行 `./xhpl`）
- HPL.dat 必须**预部署**在该目录中，旧代码**不生成也不修改** HPL.dat
- 脚本中参数：
  - `--map-by ppr:16:node:pe=32`：请求每个分配节点放置 16 个 MPI rank，每个 rank 分配 32 个 processing elements。总 rank 数取决于实际 MPI allocation 中的节点数；processing element 与物理核、逻辑核或 slot 的对应关系取决于 OpenMPI、hwloc 和绑定策略。
  - `--oversubscribe`：允许超过可用 slot 的映射，不等于避免资源冲突
  - 命令没有显式 `--bind-to`
  - 实际绑定需要通过 `mpirun --report-bindings` 验证
  - UCX TLS = self,sm：体现本地共享内存通信配置，但不能仅凭此描述完整部署拓扑
- `--allow-run-as-root`：因为主进程已 seteuid(0)
- 工作目录：C 代码 cd → thirdparty → 脚本 cd → HPL 目录 → 执行 ./xhpl
- KML 环境变量在 C 代码中设置（仅生产环境）：`KML_BLAS_NOT_USE_HBM=1`, `KML_BLAS_THREAD_TYPE=OMP`

### 5.3 HPL 输出解析

**旧实现事实：**

**预期输出格式:**

```
================================================================================
HPLinpack 2.3  --  High-Performance Linpack benchmark  --  ...
================================================================================

...

T/V                N    NB     P     Q     Time                 Gflops
--------------------------------------------------------------------------------
WC00C2C4         ?     ?     ?     ?     xx.xx   x.xxxe+02
--------------------------------------------------------------------------------

...

===========================================
PASSED                    <-- 未检查
===========================================
```

**解析规则** (`depth_detection.c:377-426`, `parse_hpl_result`):

```c
// 匹配表头行
if (strstr(line, "T/V") && strstr(line, "Time") && strstr(line, "Gflops")) {
    skipLines = 1;    // 跳过后续分隔线
    foundKeyword = true;
    continue;
}

// 解析结果行（跳过前 8 个字符后）
sscanf_s(line + 8, "%*d %*d %*d %*d %f %f", &time, &gflops);
// %*d 跳过: N, NB, P, Q (4 个整数字段)
// %f 读取: Time, Gflops
// residual (第7字段) 被完全跳过
```

**NOT 检查的项:**

- residual (`||Ax-b||`): 完全跳过
- PASSED/FAILED: 不搜索
- NaN/Inf/0 Gflops: 不检查（任何 float 值都算成功）
- 多个 T/V 表: 只取第一个匹配

**成功条件:**

- 解析出 Time 和 Gflops → `result->success = true`（只要输出可解析即为 success）

### 5.4 HPL.dat 配置

**旧实现事实：** 旧代码不生成或修改 HPL.dat，只依赖 xhpl 工作目录中已有文件。当前静态扫描未形成可信的 HPL.dat 参数基线。

**基于源码推断：** `HPL.dat` 的 N, NB, P, Q 等参数必须与硬件拓扑匹配。P×Q 应等于总 MPI rank 数。旧脚本中 `--map-by ppr:16:node:pe=32` 与 HPL.dat 中的 P、Q 值可能存在对应关系，但无法从当前代码确认。

**迁移建议：** 迁移前需要取得真实部署文件，或者由新实现根据内存容量、rank 数和拓扑生成并校验。不是简单沿用旧配方的项。

### 5.5 迁移建议

| 能力                                 | 建议等级       | 说明                                                   |
| ------------------------------------ | -------------- | ------------------------------------------------------ |
| HPL 工具调用 (xhpl)                  | 必须           | 核心                                                   |
| HPL.dat 校验或生成                   | 必须           | 旧版假设预部署，Go 应有配置校验或生成能力              |
| 性能结果采集 (Gflops)                | 必须           |                                                        |
| 数值正确性结果采集 (residual/PASSED) | **迁移优化**   | 旧版不做，Go 应考虑                                    |
| MPI 进程生命周期管理                 | 必须           | 超时、取消、清理                                       |
| 结构化退出状态解码                   | 必须           | 区分正常非零退出和异常                                 |
| 固定的 ppr:16:node:pe=32 等 MPI 参数 | **仅默认参考** | 必须允许按本地环境适配                                 |
| --allow-run-as-root                  | **应消除**     | 优先以专用普通用户执行；仅为确实需要的资源授予最小权限 |

---

## 6. HPCG 功能洞察

### 6.1 功能目的

**旧实现事实：**

- HPCG 测量**真实应用模拟性能**（共轭梯度法），比 HPL 更贴近实际负载
- 旧实现**检查结果有效性**（匹配字符串 "HPCG result is VALID with a GFLOP/s rating of="）——这是三款中唯一尝试做正确性判断的
- 解析 Gflops 但不做阈值检查

### 6.2 旧版执行方式

**旧实现事实：**

**Shell 脚本完整命令** (`benchmark_check.sh:25-29`):

```bash
hpcg)
    mpirun --allow-run-as-root \
           -x LD_LIBRARY_PATH -x PATH -x PWD \
           -map-by ppr:608:node:pe=1 \
           -mca pml ucx \
           -mca btl ^vader,tcp,openib,uct \
           -mca io romio321 \
           "$path"/xhpcg
    ;;
```

**C 代码构造命令** (`depth_detection.c:683-685`):

```c
snprintf_s(command, ..., "cd %s && bash benchmark_check.sh %s %s 2>&1",
    GetThirdPartyPath(), "hpcg", params->binary_path);
// 例: cd /batch/agent/tools/cathelper/thirdparty &&
//     bash benchmark_check.sh hpcg /opt/benchmarks/hpcg 2>&1
```

**关键事实:**

- `HPCG_BIN_PATH` 指向包含 `xhpcg` 的**目录**，脚本中拼接 `"$path"/xhpcg` 得到完整路径
  - 假设: `HPCG_BIN_PATH=/opt/benchmarks/hpcg` → 最终执行 `/opt/benchmarks/hpcg/xhpcg`
- `-map-by ppr:608:node:pe=1`：请求每个分配节点放置 608 个 MPI rank，每个 rank 分配一个 PE。它明显包含特定机器拓扑假设，但 PE 是否对应物理核，以及总 rank 数，需要结合实际 allocation 和 OpenMPI 配置确认。
- 命令未显式设置 `OMP_NUM_THREADS`，映射参数为 `pe=1`；仅凭启动命令不能确认 xhpcg 二进制内部完全不使用线程
- 包含 ROMIO MCA 参数 (`-mca io romio321`)
- MPI 工作目录：`GetThirdPartyPath()`（C 代码 cd 到该目录，脚本中不 cd）
- 结果文件写入 `GetThirdPartyPath()` 目录（与 xhpcg 所在目录不同）

**基于源码推断：** 旧启动命令未显式传递 HPCG 参数或配置文件。是否读取工作目录中的默认配置文件（如 `hpcg.dat`）取决于实际 xhpcg 版本和部署内容，需要检查部署目录并动态验证。

### 6.3 HPCG 输出解析（双路径设计）

**旧实现事实：**

旧版 HPCG 结果解析函数存在两条处理路径 (`depth_detection.c:513-530`):

```c
static int parse_hpcg_result(char *line, int buffSize, char **message, FILE *fpoint)
{
    if (fgets(line, buffSize, fpoint) == NULL) {
        // 管道为空 → 回退到文件解析
        char *latestFile = GetLatestFile(GetThirdPartyPath(), HPCG_RESULT_PREFIX);
        if (latestFile) {
            ExtractValues(latestFile, message);
            SafeFree(&latestFile);
            DeleteHpcgTxt(GetThirdPartyPath());  // 成功时清理
        } else {
            LOG_E("No matching files found.\n");
            return -1;
        }
    }
    // 如果 fgets 读到内容，直接返回 0，不解析该行内容
    return 0;
}
```

**路径 A — stdout 非空时：** 读取一行后直接返回 0，`message` 保持为空（`calloc` 初始化为全零）。上层 `HpcgBenchmarkTask` 在解析成功后设置 `result->success = true`，用户最终看到空 message 但状态为 Healthy。

**路径 B — stdout 为空时：** 回退到从 `HPCG-Benchmark*.txt` 文件解析，这是实际产生结构化性能结果的路径。

**基于源码推断：** HPCG 不同版本的 stdout 输出行为不同。旧实现依赖的是 HPCG 将结果写入文件而不输出到 stdout 的行为。实际在目标环境上的行为需要动态验证。

**结果文件格式** (`HPCG-Benchmark_*.txt`):

```
Final Summary::HPCG result is VALID with a GFLOP/s rating of=XX.XX
Final Summary::Results are valid but execution time (sec) is=XX.XX
```

**解析逻辑** (`depth_detection.c:428-477`, `ExtractValues`):

```c
if (strstr(line, "Final Summary::HPCG result is VALID with a GFLOP/s rating of=") != NULL) {
    sscanf_s(line, "Final Summary::HPCG result is VALID with a GFLOP/s rating of=%lf", &gflops);
    gflopsFound = 1;
} else if (strstr(line, "Final Summary::Results are valid but execution time (sec) is=")) {
    sscanf_s(line, "Final Summary::Results are valid but execution time (sec) is=%lf", &time);
    timeFound = 1;
}
```

**关于 "NOT VALID" 的判断：** 旧解析器匹配的是完整字符串 `"Final Summary::HPCG result is VALID with a GFLOP/s rating of="`。由于 "NOT VALID" 中间插入了 "NOT"，该连续字符串不会被 `strstr` 匹配到。因此 `HPCG result is NOT VALID` **不会被误判为 VALID**。未被解析时 `gflopsFound` 保持 0，最终输出 "Required values not found in the file." 日志，message 保持为空。用户最终看到空 message 但 `result->success = true`。**旧解析器不显式识别 NOT VALID。**

### 6.4 结果文件管理

**旧实现事实：**

**文件选择** (`utils.c:187-224`, `GetLatestFile`):

- 打开 `GetThirdPartyPath()` 目录
- 扫描文件名以 "HPCG-Benchmark" 开头的文件
- 比较 `st_mtime`，选择最新文件
- 返回 `strdup(fullPath)` 或 NULL

**文件清理** (`depth_detection.c:479-511`, `DeleteHpcgTxt`):

- 匹配规则: `DT_REG` AND `.txt` 后缀 AND (`"hpcg"` 前缀 OR `"HPCG-Benchmark"` 前缀)
- 对每个匹配文件调用 `remove()`

**旧实现事实确认的风险（来自源码，无需动态验证即可断言）：**

1. **读取历史残留文件** — 通过 mtime 选择"最新"文件，可能选中非本次运行产生的文件
2. **多 catcli 任务结果互相污染** — 所有任务共享 `GetThirdPartyPath()` 目录，无任务 ID 隔离
3. **一个任务删除另一个任务的文件** — `DeleteHpcgTxt` 按前缀批量删除，不区分任务归属
4. **结果与当前运行缺乏唯一关联** — 无任务 ID、无进程 PID 与结果文件绑定
5. **失败路径残留文件** — 文件解析成功后才调用 `DeleteHpcgTxt`，早期失败（popen 失败、parse_hpcg_result 返回 -1）不触发清理

**基于源码推断（需动态验证）：**

- 文件未刷新时 `GetLatestFile` 可能读到不完整文件 — 这依赖文件系统 flush 行为和 HPCG 写入完成时间

### 6.5 迁移建议

| 能力                  | 建议等级 | 说明                           |
| --------------------- | -------- | ------------------------------ |
| HPCG 工具调用 (xhpcg) | 必须     |                                |
| MPI 进程管理          | 必须     | 超时、取消、清理               |
| 结果文件处理          | 必须     | 但应重新设计（见下）           |
| VALID 状态检查        | 必须     | 唯一做了正确性检查的 benchmark |
| Gflops 和 Time 解析   | 必须     |                                |
| 清理本次结果文件      | 必须     |                                |

**Go 版本应重新设计的：**

- **每任务独立工作目录**，而非共享 `GetThirdPartyPath()`
- 本次运行结果与任务 ID 绑定
- 不通过扫描共享目录找最新文件
- 结果文件**始终清理**（无论成功/失败）
- 进程和结果归属明确
- 支持超时和取消
- MPI 进程树完整清理

---

## 7. Shell 脚本设计

### 7.1 benchmark_check.sh 完整脚本

**旧实现事实：**

```bash
#!/bin/bash
BENCHMARK_PATH=@BENCHMARK_PATH@

# HPCKit 环境初始化
if [ -f "${BENCHMARK_PATH}"/HPCKit/latest/setvars.sh ];then
    source "${BENCHMARK_PATH}"/HPCKit/latest/setvars.sh --use-bisheng >/dev/null 2>&1
fi

# 参数验证
if [ $# -lt 2 ]; then
   echo "Insufficient number of parameters."
   exit 1
fi
benchmark_type=$1
path=$2
shift 2

case "$benchmark_type" in
    hpl)
        if [ $# -ne 0 ]; then exit 1; fi
        cd "$path"
        mpirun --allow-run-as-root --oversubscribe \
               -x OMP_NUM_THREADS=32 \
               --map-by ppr:16:node:pe=32 \
               -x PATH -x LD_LIBRARY_PATH -x UCX_TLS=self,sm \
               -mca pml ucx -mca btl ^vader,tcp,openib,uct \
               ./xhpl
        ;;

    hpcg)
        if [ $# -ne 0 ]; then exit 1; fi
        mpirun --allow-run-as-root \
               -x LD_LIBRARY_PATH -x PATH -x PWD \
               -map-by ppr:608:node:pe=1 \
               -mca pml ucx -mca btl ^vader,tcp,openib,uct \
               -mca io romio321 \
               "$path"/xhpcg
        ;;

    stream)
        if [ $# -ne 1 ]; then exit 1; fi
        core_num=$1
        export OMP_NUM_THREADS=$core_num
        output=$(numactl --localalloc "$path"/stream_c.exe)
        echo "$output"
        ;;

    *)
        echo "Unknown parameter."; exit 1 ;;
esac
```

### 7.2 每个参数的含义

| 参数/选项                    | 所属     | 含义                                                         | 本地适配建议                                                 |
| ---------------------------- | -------- | ------------------------------------------------------------ | ------------------------------------------------------------ |
| `--allow-run-as-root`        | HPL/HPCG | 允许 root 运行 MPI 进程                                      | 新版本应优先非 root；仅为确实需要的资源授予最小权限          |
| `--oversubscribe`            | HPL      | 允许超过可用 slot 的映射                                     | 按实际拓扑调整                                               |
| `-x OMP_NUM_THREADS=32`      | HPL      | 通过 mpirun 传递环境变量，设置 OpenMP 线程数                 | 按 core/rank 比调整                                          |
| `--map-by ppr:16:node:pe=32` | HPL      | 每节点 16 rank，各 32 个 PE（不直接等于物理核）              | **旧硬件假设，必须适配**；实际绑定需 `--report-bindings` 验证 |
| `-x UCX_TLS=self,sm`         | HPL      | UCX 仅用共享内存通信                                         | 单机场景参考；多机需补充 ib/tcp                              |
| `-mca pml ucx`               | HPL/HPCG | OpenMPI 使用 UCX PML                                         | 按已安装 MPI 版本适配                                        |
| `-mca btl ^...`              | HPL/HPCG | 排除特定 BTL 传输                                            | 按已安装 MPI 版本适配                                        |
| `-map-by ppr:608:node:pe=1`  | HPCG     | 每节点 608 rank，各 1 个 PE                                  | **旧硬件假设，必须适配**                                     |
| `-mca io romio321`           | HPCG     | ROMIO MPI-IO 实现                                            | 按 MPI 版本支持情况                                          |
| `-x PWD`                     | HPCG     | 传递工作目录环境变量                                         | 按新运行时目录设计                                           |
| `numactl --localalloc`       | STREAM   | 本地优先内存分配，非强制 NUMA 绑定                           | 推荐保留，但可考虑增加 `--cpunodebind` 支持                  |
| `HPCKit setvars.sh`          | ALL      | 如存在则 source 该脚本并传 `--use-bisheng`；实际设置哪些编译器、MPI、UCX、BLAS 和动态库环境需读取脚本或执行后检查 | 如果 Go 直接执行二进制，需保留环境初始化（轻量 wrapper / 部署时预生成环境 / 配置显式提供环境变量） |

### 7.3 迁移建议

- Shell 脚本需要被记录和理解，但 Go 版本不需要保留
- Go 应直接调用外部二进制，而非通过 shell 脚本转发
- HPCKit 的环境初始化逻辑如果被依赖，则不能简单删除；可选方案包括：保留轻量环境初始化 wrapper、部署时预生成环境、由配置显式提供环境变量。当前报告只提出能力要求，不锁定具体实现。

STREAM:
C 构造: cd /batch/agent/tools/cathelper/thirdparty && bash benchmark_check.sh stream /opt/benchmarks/stream 32 2>&1
脚本内: export OMP_NUM_THREADS=32 ; output=$(numactl --localalloc /opt/benchmarks/stream/stream_c.exe) ; echo "$output"

HPL:
C 构造: cd /batch/agent/tools/cathelper/thirdparty && bash benchmark_check.sh hpl /opt/benchmarks/hpl 2>&1
脚本内: cd /opt/benchmarks/hpl ;
  mpirun --allow-run-as-root --oversubscribe -x OMP_NUM_THREADS=32 \
    --map-by ppr:16:node:pe=32 \
    -x PATH -x LD_LIBRARY_PATH -x UCX_TLS=self,sm \
    -mca pml ucx -mca btl ^vader,tcp,openib,uct \
    ./xhpl

HPCG:
C 构造: cd /batch/agent/tools/cathelper/thirdparty && bash benchmark_check.sh hpcg /opt/benchmarks/hpcg 2>&1
脚本内: mpirun --allow-run-as-root \
    -x LD_LIBRARY_PATH -x PATH -x PWD \
    -map-by ppr:608:node:pe=1 \
    -mca pml ucx -mca btl ^vader,tcp,openib,uct \
    -mca io romio321 \
    /opt/benchmarks/hpcg/xhpcg
