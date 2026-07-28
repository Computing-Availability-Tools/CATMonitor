# CATMonitor health/stress Linux 离线部署与 Web 使用指南

更新时间：2026-07-28

适用节点：`51.62.10.87`（Linux ARM64）

适用分支：`feature/health-stress-v0.3.2`

> 文档类型：节点部署与使用指南。功能和状态契约以
> [STRESS_SPEC.md](../../STRESS_SPEC.md) 为准，设计实现以
> [STRESS_DESIGN.md](../../STRESS_DESIGN.md) 为准。本文中的路径、MPI 参数
> 和性能结果仅适用于 51.62.10.87，不得直接套用到其他节点。

## 1. 文档目标

本文说明如何在无外网或受限网络的 Linux 节点完成以下工作：

1. 安装独立 Go 1.25.1 工具链；
2. 离线编译 CATMonitor CLI 和 Web；
3. 将 STREAM、HPL、HPCG 接入节点专用的 `benchmark_check.sh`；
4. 使用 CLI 分别运行三项压测；
5. 启动 Web，并从另一台 Windows 机器通过 SSH 隧道访问；
6. 检查 JSON 报告、HPCG 结果文件和残留进程。

第一版压测仅支持 Linux。三个 benchmark 不设置性能阈值：

- 命令正常完成且必需结果解析成功：`healthy`；
- 达到 CATMonitor 配置的运行上限并被主动停止：`time_limit_reached`，按通过处理，可以没有最终性能值；
- 命令提前报错、HPL 校验失败、HPCG 没有生成本次有效结果文件：失败。

压测结果不直接计入健康总分，但压测产生的 CPU、内存、MPI 和 NUMA 负载可能使实时健康指标与健康分暂时变化。

## 2. 当前已验证基线

### 2.1 固定目录

```text
/opt/catmonitor/
├── packages/
│   ├── CATMonitor-feature-health-stress-v0.3.2.zip
│   └── go1.25.1.linux-arm64.tar.gz
├── toolchains/
│   └── go1.25.1/
├── CATMonitor-feature-health-stress-v0.3.2/
└── benchmarks/
    └── runtime/
        ├── stream/
        │   └── stream_omp
        ├── hpl/
        │   ├── xhpl
        │   └── HPL.dat
        └── hpcg/
            ├── xhpcg
            └── hpcg.dat
```

脚本最终使用的完整资产路径为：

```text
/opt/catmonitor/benchmarks/runtime/stream/stream_omp
/opt/catmonitor/benchmarks/runtime/hpl/xhpl
/opt/catmonitor/benchmarks/runtime/hpl/HPL.dat
/opt/catmonitor/benchmarks/runtime/hpcg/xhpcg
/opt/catmonitor/benchmarks/runtime/hpcg/hpcg.dat
```

### 2.2 软件环境

```text
Go       1.25.1 linux/arm64
MPI      MPICH 4.1.3
GCC      10.2.0
OpenBLAS 0.3.18
yaml.v3  v3.0.1
```

### 2.3 三项 CLI 实测结果

| Benchmark | CATMonitor 状态 | 性能结果 | CATMonitor 总耗时 |
|---|---|---:|---:|
| STREAM | `healthy` | Copy 86856.90、Scale 87821.60、Add 89099.30、Triad 91385.40 MB/s | 1.059 秒 |
| HPCG | `healthy` | 8.11 GFLOP/s，结果文件计算时间 61.27 秒 | 123.493 秒 |
| HPL | `healthy` | 205.13 GFLOP/s，N=50000，NB=256，P×Q=4×2 | 421.046 秒 |

HPL 的 CATMonitor 总耗时包含 MPI 启动、矩阵初始化、解析与退出，因此会比 HPL 结果行中的 `time_seconds=406.27` 更长。

HPCG 的总耗时也包含初始化、优化、校验、汇总及结果文件生成。只要本次结果文件包含 `HPCG result is VALID`，并能解析 GFLOP/s，链路即为正常。

本节点更早一次独立运行 HPCG 曾得到 15.5948 GFLOP/s，CATMonitor 联调结果为 8.11 GFLOP/s。两次运行环境、资源状态或配置可能不同；CATMonitor 不以二者的数值差异判定健康，只检查本次执行和 VALID 结果。

## 3. 准备目录和安装包

```bash
set -euo pipefail

install -d -m 0755 \
  /opt/catmonitor/packages \
  /opt/catmonitor/toolchains \
  /opt/catmonitor/benchmarks/runtime
```

将以下文件上传到 `/opt/catmonitor/packages/`：

```text
CATMonitor-feature-health-stress-v0.3.2.zip
go1.25.1.linux-arm64.tar.gz
```

检查：

```bash
ls -lh /opt/catmonitor/packages
```

## 4. 安装独立 Go 工具链

```bash
set -euo pipefail

GO_ARCHIVE=/opt/catmonitor/packages/go1.25.1.linux-arm64.tar.gz
GO_ROOT=/opt/catmonitor/toolchains/go1.25.1

test -f "$GO_ARCHIVE"
test "$(uname -m)" = "aarch64"

install -d -m 0755 "$GO_ROOT"

tar -xzf "$GO_ARCHIVE" \
  -C "$GO_ROOT" \
  --strip-components=1

"$GO_ROOT/bin/go" version
```

预期：

```text
go version go1.25.1 linux/arm64
```

不要替换系统 `/usr/local/go`。本文后续始终使用：

```text
/opt/catmonitor/toolchains/go1.25.1/bin/go
```

如果需要重新安装，先将旧的 `GO_ROOT` 备份或明确确认后再删除，避免误删其他工具链。

## 5. 解压 CATMonitor

```bash
set -euo pipefail

CAT_ARCHIVE=/opt/catmonitor/packages/CATMonitor-feature-health-stress-v0.3.2.zip
CAT_ROOT=/opt/catmonitor/CATMonitor-feature-health-stress-v0.3.2
TMP_EXTRACT=$(mktemp -d /opt/catmonitor/.catmonitor-extract.XXXXXX)

test -f "$CAT_ARCHIVE"
command -v unzip

unzip -q "$CAT_ARCHIVE" -d "$TMP_EXTRACT"

find "$TMP_EXTRACT" -maxdepth 3 -type f -name go.mod -print

EXTRACTED_ROOT=$(
  dirname "$(
    find "$TMP_EXTRACT" \
      -maxdepth 3 \
      -type f \
      -name go.mod \
      | head -n 1
  )"
)

test -n "$EXTRACTED_ROOT"
test -f "$EXTRACTED_ROOT/go.mod"

if [ -e "$CAT_ROOT" ]; then
    BACKUP_ROOT="${CAT_ROOT}.backup.$(date +%Y%m%d%H%M%S)"
    mv "$CAT_ROOT" "$BACKUP_ROOT"
    echo "旧版本已备份到：$BACKUP_ROOT"
fi

mv "$EXTRACTED_ROOT" "$CAT_ROOT"
rm -rf -- "$TMP_EXTRACT"
```

如果旧版本仍在运行，应先停止对应进程再替换目录。上述步骤会移动旧目录作为带时间戳的备份，不会直接删除旧版本。

确认：

```bash
cd "$CAT_ROOT"

test -f go.mod
test -f go.sum
test -f features/health/stress/benchmark_check.sh

pwd
sed -n '1,12p' go.mod
```

## 6. 检查三项 benchmark 资产

```bash
set -euo pipefail

BENCH_RUNTIME=/opt/catmonitor/benchmarks/runtime

test -x "$BENCH_RUNTIME/stream/stream_omp"
test -x "$BENCH_RUNTIME/hpl/xhpl"
test -r "$BENCH_RUNTIME/hpl/HPL.dat"
test -x "$BENCH_RUNTIME/hpcg/xhpcg"
test -r "$BENCH_RUNTIME/hpcg/hpcg.dat"

ls -lh \
  "$BENCH_RUNTIME/stream/stream_omp" \
  "$BENCH_RUNTIME/hpl/xhpl" \
  "$BENCH_RUNTIME/hpl/HPL.dat" \
  "$BENCH_RUNTIME/hpcg/xhpcg" \
  "$BENCH_RUNTIME/hpcg/hpcg.dat"
```

检查动态库：

```bash
for binary in \
  "$BENCH_RUNTIME/stream/stream_omp" \
  "$BENCH_RUNTIME/hpl/xhpl" \
  "$BENCH_RUNTIME/hpcg/xhpcg"
do
    echo "=== $binary ==="
    file "$binary"
    ldd "$binary" 2>&1

    if ldd "$binary" 2>&1 | grep -q 'not found'; then
        echo "ERROR: $binary 缺少动态库" >&2
        exit 1
    fi
done
```

`ldd` 显示 `not a dynamic executable` 表示产物是静态链接，不属于缺少动态库；只有出现 `not found` 才是依赖故障。

## 7. 适配节点专用 benchmark_check.sh

CATMonitor 的 Go 代码只负责作业、超时、状态和解析。执行器完整路径、MPI 参数、NUMA 策略和环境变量统一放在每台机器自己的：

```text
features/health/stress/benchmark_check.sh
```

本节点使用 MPICH，不能照搬旧 OpenMPI 环境中的：

```text
--allow-run-as-root
--map-by
--bind-to
-mca
```

先备份：

```bash
CAT_ROOT=/opt/catmonitor/CATMonitor-feature-health-stress-v0.3.2
SCRIPT="$CAT_ROOT/features/health/stress/benchmark_check.sh"

cp -a "$SCRIPT" "${SCRIPT}.before-51.62.10.87"
```

本节点已验证的脚本接口如下。CATMonitor 只向脚本传递一个 benchmark 名称。确认备份完成后执行：

```bash
cat > "$SCRIPT" <<'EOF'
#!/usr/bin/env bash
set -euo pipefail

benchmark_type="${1:-}"
BENCH_ROOT=/opt/catmonitor/benchmarks/runtime

STREAM_BIN="$BENCH_ROOT/stream/stream_omp"

HPL_DIR="$BENCH_ROOT/hpl"
HPL_BIN="$HPL_DIR/xhpl"

HPCG_DIR="$BENCH_ROOT/hpcg"
HPCG_BIN="$HPCG_DIR/xhpcg"

case "$benchmark_type" in
    stream)
        test -x "$STREAM_BIN"

        export OMP_NUM_THREADS=32
        export OMP_DYNAMIC=FALSE

        exec numactl --interleave=all "$STREAM_BIN"
        ;;

    hpl)
        test -x "$HPL_BIN"
        test -r "$HPL_DIR/HPL.dat"

        cd "$HPL_DIR"

        export OPENBLAS_NUM_THREADS=12
        export OMP_NUM_THREADS=12
        export OMP_DYNAMIC=FALSE

        exec mpirun -np 8 ./xhpl
        ;;

    hpcg)
        test -x "$HPCG_BIN"
        test -r "$HPCG_DIR/hpcg.dat"

        cd "$HPCG_DIR"

        export OMP_NUM_THREADS=1
        export OMP_DYNAMIC=FALSE

        exec mpirun -np 8 ./xhpcg
        ;;

    *)
        echo "unsupported benchmark: $benchmark_type" >&2
        exit 2
        ;;
esac
EOF
```

保存后检查：

```bash
chmod 755 "$SCRIPT"
bash -n "$SCRIPT"

grep -nE \
  'BENCH_ROOT|stream_omp|xhpl|xhpcg|mpirun|numactl' \
  "$SCRIPT"
```

三项执行器必须使用完整固定路径。不要再为每项 benchmark 增加额外 wrapper；机器差异集中在这一份脚本中。

## 8. 离线依赖与构建

### 8.1 恢复构建环境

每次重新连接后执行：

```bash
set -euo pipefail

CAT_ROOT=/opt/catmonitor/CATMonitor-feature-health-stress-v0.3.2
GO_ROOT=/opt/catmonitor/toolchains/go1.25.1

export PATH="/usr/local/mpich-4.1.3/bin:/usr/local/gcc10.2.0/bin:/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin"

cd "$CAT_ROOT"

"$GO_ROOT/bin/go" version
test -d /root/go/pkg/mod/gopkg.in/yaml.v3@v3.0.1
```

### 8.2 依赖验证

```bash
GOTOOLCHAIN=local \
GOPROXY=off \
GOSUMDB=off \
"$GO_ROOT/bin/go" mod verify
```

正常输出：

```text
all modules verified
```

不要把下面命令作为离线构建是否可行的判据：

```bash
GOPROXY=off go list -m all
```

`go list -m all` 会尝试解析完整模块图，可能访问当前构建不需要且尚未缓存的间接测试模块。此时出现：

```text
module lookup disabled by GOPROXY=off
```

不代表 `yaml.v3` 下载失败。最终以 CLI 和 Web 能否在 `GOPROXY=off` 下构建成功为准。

### 8.3 离线编译

```bash
mkdir -p bin
rm -f bin/catmonitor bin/catmonitor-web

GOTOOLCHAIN=local \
GOPROXY=off \
GOSUMDB=off \
"$GO_ROOT/bin/go" build \
  -buildvcs=false \
  -trimpath \
  -o bin/catmonitor \
  ./cmd/catmonitor

GOTOOLCHAIN=local \
GOPROXY=off \
GOSUMDB=off \
"$GO_ROOT/bin/go" build \
  -buildvcs=false \
  -trimpath \
  -o bin/catmonitor-web \
  ./features/web
```

### 8.4 检查构建产物

```bash
ls -lh bin/catmonitor bin/catmonitor-web
file bin/catmonitor bin/catmonitor-web

"$GO_ROOT/bin/go" version -m bin/catmonitor
"$GO_ROOT/bin/go" version -m bin/catmonitor-web
```

构建信息中应包含：

```text
dep gopkg.in/yaml.v3 v3.0.1
```

检查 Web 动态库：

```bash
ldd bin/catmonitor-web 2>&1

if ldd bin/catmonitor-web 2>&1 | grep -q 'not found'; then
    echo "ERROR: catmonitor-web 缺少动态库" >&2
    ldd bin/catmonitor-web 2>&1 | grep 'not found'
    exit 1
fi

echo "OK: CATMonitor CLI 和 Web 编译完成"
```

## 9. 创建 CLI 配置

CLI 和 Web 是两个独立进程，当前分别读取各自的配置文件。压测配置结构相同，但文件不会自动互相继承：

- CLI：`/etc/catmonitor/catmonitor.yaml`
- Web：`/etc/catmonitor/web.yaml`

先创建目录：

```bash
install -d -m 0750 /etc/catmonitor
install -d -m 0750 /var/lib/catmonitor
```

创建 CLI 配置：

```bash
CAT_ROOT=/opt/catmonitor/CATMonitor-feature-health-stress-v0.3.2

cat > /etc/catmonitor/catmonitor.yaml <<EOF
health:
  stress:
    enabled: true
    web_enabled: false
    script_path: ${CAT_ROOT}/features/health/stress/benchmark_check.sh
    report_path: /var/lib/catmonitor/stress-latest.json
    default_benchmarks:
      - stream

    benchmarks:
      stream:
        enabled: true
        timeout: 1m

      hpl:
        enabled: true
        timeout: 10m

      hpcg:
        enabled: true
        result_dir: /opt/catmonitor/benchmarks/runtime/hpcg
        timeout: 3m
EOF
```

说明：

- STREAM 实测约 1 秒，`1m` 足以覆盖启动抖动并及时处理异常挂起；
- HPL 实测约 421 秒，使用 `10m`；
- HPCG 实测约 123 秒，使用 `3m`；
- 只有 HPCG 需要在 YAML 中配置 `result_dir`，因为结果从本次生成的 `HPCG-Benchmark*.txt` 中读取；
- STREAM 和 HPL 的执行器路径不写入 YAML，均由 `benchmark_check.sh` 管理。

检查：

```bash
cat /etc/catmonitor/catmonitor.yaml
```

## 10. CLI 验收

不要第一次同时运行三项。按 STREAM、HPCG、HPL 顺序逐项验收。

```bash
cd /opt/catmonitor/CATMonitor-feature-health-stress-v0.3.2
```

### 10.1 STREAM

```bash
./bin/catmonitor health stress run \
  --bench stream \
  -c /etc/catmonitor/catmonitor.yaml \
  -o table
```

正常应解析：

```text
Copy
Scale
Add
Triad
```

### 10.2 HPCG

```bash
./bin/catmonitor health stress run \
  --bench hpcg \
  -c /etc/catmonitor/catmonitor.yaml \
  -o table
```

成功条件：

1. `mpirun` 返回码为 0；
2. 本次生成或更新了 `HPCG-Benchmark*.txt`；
3. 文件包含 `HPCG result is VALID`；
4. 能解析 `GFLOP/s rating`。

### 10.3 HPL

```bash
./bin/catmonitor health stress run \
  --bench hpl \
  -c /etc/catmonitor/catmonitor.yaml \
  -o table
```

成功条件：

1. `mpirun` 返回码为 0；
2. 输出包含 HPL 结果行；
3. residual check 通过；
4. 能解析 N、NB、P、Q、Time 和 GFLOPS。

### 10.4 查看报告

每次运行后：

```bash
/usr/bin/python3.9 -m json.tool \
  /var/lib/catmonitor/stress-latest.json
```

JSON 报告是状态与数值的权威来源。当前 CLI 表格仍可能直接显示内部状态 `healthy`，多个性能值也可能在终端宽度不足时挤在一行；这是展示问题，不影响执行、解析和 JSON 报告。

### 10.5 检查残留进程

```bash
pgrep -af \
  '[s]tream_omp|[x]hpl|[x]hpcg|[m]pirun|[n]umactl' \
  || true
```

正常完成、主动取消或 CATMonitor 限时停止后，不应存在残留 benchmark/MPI 进程。

## 11. 检查 HPCG 最新结果

```bash
HPCG_DIR=/opt/catmonitor/benchmarks/runtime/hpcg

LATEST_HPCG_RESULT=$(
  find "$HPCG_DIR" \
    -maxdepth 1 \
    -type f \
    -name 'HPCG-Benchmark*.txt' \
    -printf '%T@ %p\n' |
  sort -nr |
  head -n 1 |
  cut -d' ' -f2-
)

echo "$LATEST_HPCG_RESULT"

grep -E \
  'HPCG result is VALID|GFLOP/s rating|execution time' \
  "$LATEST_HPCG_RESULT"
```

不要只按目录中“看起来最新”的历史文件判断 CATMonitor 本次作业。CATMonitor 会记录运行前文件状态，正常结束后只接受本次新增或变化的结果文件。

## 12. 创建 Web 配置

CLI 验收全部通过后再启用 Web：

```bash
CAT_ROOT=/opt/catmonitor/CATMonitor-feature-health-stress-v0.3.2

cat > /etc/catmonitor/web.yaml <<EOF
server:
  addr: "127.0.0.1:9527"

collector:
  refresh_interval: 5s
  history_points: 60

storage:
  snapshot_path: /var/lib/catmonitor/snapshot.json
  runtime_path: /var/lib/catmonitor/runtime.json

health:
  stress:
    enabled: true
    web_enabled: true
    script_path: ${CAT_ROOT}/features/health/stress/benchmark_check.sh
    report_path: /var/lib/catmonitor/stress-latest.json
    default_benchmarks:
      - stream

    benchmarks:
      stream:
        enabled: true
        timeout: 1m

      hpl:
        enabled: true
        timeout: 10m

      hpcg:
        enabled: true
        result_dir: /opt/catmonitor/benchmarks/runtime/hpcg
        timeout: 3m
EOF
```

Web 压测提交要求：

- `health.stress.enabled: true`：启用压测能力；
- `web_enabled: true`：额外允许 Web 提交高负载作业；
- Web 必须绑定回环地址；
- 请求必须来自回环连接。

因此建议保持 `127.0.0.1:9527`，从 Windows 通过 SSH 隧道访问，不要直接将压测控制接口暴露到业务网络。

## 13. 启动并验证 Web

先检查端口：

```bash
ss -lntp | grep ':9527' || true
pgrep -af '[c]atmonitor-web' || true
```

前台启动：

```bash
cd /opt/catmonitor/CATMonitor-feature-health-stress-v0.3.2

./bin/catmonitor-web \
  -config /etc/catmonitor/web.yaml
```

保持终端打开，在第二个 Linux 终端检查：

```bash
ss -lntp | grep ':9527'
curl -sS -I http://127.0.0.1:9527/

curl -sS \
  http://127.0.0.1:9527/api/health/stress/config \
  | /usr/bin/python3.9 -m json.tool
```

预期：

- 监听地址为 `127.0.0.1:9527`；
- `enabled`、`feature_enabled`、`web_enabled`、`loopback` 均为 `true`；
- STREAM/HPL/HPCG 均为 `available: true`；
- STREAM 的 `timeout_seconds` 为 60；
- HPL 为 600；
- HPCG 为 180。

旧接口：

```bash
curl -i http://127.0.0.1:9527/api/stress/config
```

返回 `404` 是正常的，正式接口是：

```text
/api/health/stress/config
```

## 14. 从 Windows 访问 Web

Windows PowerShell：

```powershell
ssh -N `
  -o ExitOnForwardFailure=yes `
  -L 127.0.0.1:9527:127.0.0.1:9527 `
  root@51.62.10.87
```

保持该窗口打开。另开 PowerShell：

```powershell
Test-NetConnection 127.0.0.1 -Port 9527
```

预期：

```text
TcpTestSucceeded : True
```

浏览器访问：

```text
http://127.0.0.1:9527/#/stress
```

## 15. Web 验收顺序

第一次仍按以下顺序分别运行：

1. STREAM；
2. HPCG；
3. HPL。

页面首次进入默认只勾选 STREAM，这是 `default_benchmarks: [stream]` 的含义，不代表 HPL/HPCG 未配置。

勾选 HPL、HPCG 后先等待超过 5 秒，确认页面自动刷新不会取消选择。若勾选项重新变成只有 STREAM，说明 Web 二进制未包含表单刷新修复，需要使用包含提交 `8bbde84`（`fix(web): preserve stress form across refresh`）的源码重新编译。

“单次缩短超时（秒）”仅作用于本次作业：

- 可以比 YAML 上限短；
- 不能延长 YAML 上限；
- 不填写时使用各项目 YAML 上限；
- 为获取完整 GFLOPS/带宽结果，首次验收建议不填写。

每次 Web 作业完成后检查：

```bash
/usr/bin/python3.9 -m json.tool \
  /var/lib/catmonitor/stress-latest.json

pgrep -af \
  '[s]tream_omp|[x]hpl|[x]hpcg|[m]pirun|[n]umactl' \
  || true
```

不要同时从 CLI 和 Web 启动作业。两个进程各有自己的作业管理器，并共享节点计算资源和报告路径，同时运行可能造成资源争用或报告互相覆盖。

## 16. 状态与结果判定

| 状态 | 含义 | 是否通过 |
|---|---|---|
| `healthy` | 命令成功，必需结果已解析 | 是 |
| `time_limit_reached` | 达到配置上限，被 CATMonitor 主动停止 | 是 |
| `running` | 正在运行 | 未完成 |
| `cancelled` | 用户主动取消 | 否 |
| `unhealthy` | 命令、校验或解析失败 | 否 |
| `unavailable` / `unsupported` | 资产、配置或平台不满足 | 否 |

HPL/HPCG 计算量较大时，可能在产生最终 GFLOPS 前达到 CATMonitor 上限。只要运行期间未提前报错，`time_limit_reached` 按压测窗口执行成功处理，没有 GFLOPS/Time 属于正常情况。

正常退出则必须完成结果校验：

- STREAM：必须解析 Copy、Scale、Add、Triad；
- HPL：必须解析结果行并确认 residual check 通过；
- HPCG：必须找到本次结果文件，包含 `HPCG result is VALID` 并解析 GFLOP/s。

## 17. 常见问题

### 17.1 `go list -m all` 离线失败

忽略该命令，直接执行 `GOPROXY=off` 的两个 `go build`。只要 CLI 和 Web 构建成功，当前构建依赖即已完整。

### 17.2 Web 显示未启用

检查：

```bash
grep -nE \
  'addr:|enabled:|web_enabled:|script_path:|result_dir:|timeout:' \
  /etc/catmonitor/web.yaml
```

同时确认 Web 实际使用：

```bash
-config /etc/catmonitor/web.yaml
```

不要误用 CLI 的 `catmonitor.yaml` 启动 Web。

### 17.3 HPL 在 4 分钟附近被停止

本节点完整 HPL 约 421 秒，必须将 YAML 上限设为至少 `10m`。如果 Web 单次超时填写得更短，也会提前停止。

### 17.4 HPCG 正常退出但 CATMonitor 报错

检查：

```bash
ls -lt /opt/catmonitor/benchmarks/runtime/hpcg/HPCG-Benchmark*.txt | head
```

确认：

- `result_dir` 指向 `/opt/catmonitor/benchmarks/runtime/hpcg`；
- 本次确实生成或更新了结果文件；
- 文件包含 `HPCG result is VALID`；
- 文件名匹配 `HPCG-Benchmark*.txt`。

### 17.5 Web 勾选项每 5 秒恢复成 STREAM

这是旧前端每次重绘都按 `default_benchmarks` 初始化造成的。使用包含 `8bbde84` 的源码重新构建 `catmonitor-web`。

### 17.6 Web 能打开但不能提交

检查配置 API 中：

```text
enabled=true
feature_enabled=true
web_enabled=true
loopback=true
```

并确认通过 Windows SSH 隧道访问 `127.0.0.1:9527`，而不是直接访问服务器业务地址。

### 17.7 CLI 中显示 `healthy` 而不是 `OK`

`healthy` 是内部状态值，JSON 报告同样使用该值；Web 会将成功状态显示为绿色 `OK`。CLI 状态映射与宽表格换行属于后续展示优化，不影响验收。

## 18. 最终验收清单

- [ ] Go 1.25.1 位于 `/opt/catmonitor/toolchains/go1.25.1`
- [ ] `yaml.v3@v3.0.1` 已缓存
- [ ] CLI 可在 `GOPROXY=off` 下构建
- [ ] Web 可在 `GOPROXY=off` 下构建
- [ ] 三项 benchmark 二进制无缺失动态库
- [ ] `benchmark_check.sh` 使用 `/opt/catmonitor/benchmarks/runtime`
- [ ] 脚本通过 `bash -n`
- [ ] STREAM CLI 为 `healthy`
- [ ] HPCG CLI 为 `healthy` 且结果为 VALID
- [ ] HPL CLI 为 `healthy` 且 residual check 通过
- [ ] 三项运行后无残留进程
- [ ] Web 仅监听 `127.0.0.1:9527`
- [ ] Web 配置 API 显示三项均可用
- [ ] Windows SSH 隧道连通
- [ ] Web 勾选项跨 5 秒刷新后仍保留
- [ ] Web 逐项运行 STREAM、HPCG、HPL 成功

## 19. 推荐日常操作顺序

```text
检查资产与动态库
        ↓
检查 benchmark_check.sh
        ↓
离线构建 CLI / Web
        ↓
CLI：STREAM → HPCG → HPL
        ↓
检查 JSON、HPCG 文件与残留进程
        ↓
启动 Web
        ↓
Windows 建立 SSH 隧道
        ↓
Web：STREAM → HPCG → HPL
```

CLI 三项已在 51.62.10.87 验收通过。下一阶段重点是 Web 的逐项运行、取消、限时停止和 5 秒刷新表单保持验证。
