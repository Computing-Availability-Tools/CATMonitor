# stress 可靠性压测部署与验收指南

本文说明如何构建、部署、升级和验收 `features/stress`。开源文档只使用通用路径；
具体节点的 benchmark 路径、MPI/NUMA 参数、IP 地址和实测结果应保存在部署侧，
不得提交到开源仓库。

当前正式接口：

- CLI：`catmonitor stress`
- Web：`/stress/`
- API：`/api/stress/*`
- 配置：CATMonitor 主配置顶层 `stress:`
- 节点适配：源码目录外的 `benchmark_check.sh`

## 1. 部署原则

1. STREAM、HPL、HPCG 的绝对路径、环境变量、MPI/NUMA 参数都放在节点脚本中，
   不允许通过 Web 任意编辑。
2. `/etc/catmonitor/benchmark_check.sh` 一旦通过实机验证，升级时不得用仓库模板
   直接覆盖。
3. 升级应以新模板生成候选脚本，只迁移旧脚本顶部的节点变量；候选脚本通过
   `describe` 后才能切换。
4. 不要把旧脚本整体复制回新版本，否则会丢失新的 `describe` 协议；也不要把
   未适配的新模板直接投入运行，否则会丢失节点路径和 MPI 参数。
5. CLI 与 Web 使用同一份主配置、`report_path` 和 Linux 文件锁。两边能共享
   报告，但不能同时启动两组压测。
6. 交互式 SSH 终端不建议全局执行 `set -e`；检查命令失败时应保留终端，先查看
   返回码和日志。

## 2. 开发机检查

在仓库的 `CATMonitor` 目录执行：

```bash
bash -n features/stress/benchmark_check.sh
gofmt -w features/stress features/stress/cli
go test ./features/stress/... ./features/web ./cmd/catmonitor ./internal/config
go test -race ./features/stress/... ./features/web
go vet ./features/stress/... ./features/web ./cmd/catmonitor ./internal/config
```

按目标架构验证构建：

```bash
GOOS=linux GOARCH=amd64 go build ./cmd/catmonitor ./features/web
GOOS=linux GOARCH=arm64 go build ./cmd/catmonitor ./features/web
GOOS=windows GOARCH=amd64 go build ./cmd/catmonitor ./features/web
```

Windows 产物仅用于确认项目可构建；可靠性压测执行仍只支持 Linux。

## 3. 定位源码并构建候选二进制

Git 工作树和 ZIP 解压目录都可以使用。先显式设置路径，不要依赖当前目录：

```bash
REPO_ROOT=/path/to/CATHelper
CAT_ROOT="$REPO_ROOT/CATMonitor"
GO_BIN=/path/to/go

test -f "$CAT_ROOT/go.mod"
test -f "$CAT_ROOT/features/stress/benchmark_check.sh"
grep -q 'CATMONITOR_STRESS_DESCRIBE_PROTOCOL=1' \
  "$CAT_ROOT/features/stress/benchmark_check.sh"
```

若不知道 ZIP 的实际层级，可先定位唯一模板：

```bash
find /path/to/extracted-directory \
  -type f \
  -path '*/CATMonitor/features/stress/benchmark_check.sh' \
  -print
```

升级时先备份，再构建到临时目录，不直接覆盖运行中的二进制：

```bash
STAMP=$(date +%Y%m%d%H%M%S)
BACKUP_ROOT=/opt/catmonitor/backups/stress-upgrade-$STAMP
BUILD_DIR=/opt/catmonitor/build-stress-$STAMP

install -d -m 0750 "$BACKUP_ROOT"
install -d -m 0755 "$BUILD_DIR"

cp -a /etc/catmonitor/benchmark_check.sh "$BACKUP_ROOT/" 2>/dev/null || true
cp -a /etc/catmonitor/catmonitor.yaml "$BACKUP_ROOT/" 2>/dev/null || true
cp -a "$CAT_ROOT/bin/catmonitor" "$BACKUP_ROOT/" 2>/dev/null || true
cp -a "$CAT_ROOT/bin/catmonitor-web" "$BACKUP_ROOT/" 2>/dev/null || true
```

构建：

```bash
cd "$CAT_ROOT"

GOTOOLCHAIN=local GOPROXY=off GOSUMDB=off \
  "$GO_BIN" mod verify

GOTOOLCHAIN=local GOPROXY=off GOSUMDB=off \
  "$GO_BIN" build -buildvcs=false -trimpath \
  -o "$BUILD_DIR/catmonitor" ./cmd/catmonitor

GOTOOLCHAIN=local GOPROXY=off GOSUMDB=off \
  "$GO_BIN" build -buildvcs=false -trimpath \
  -o "$BUILD_DIR/catmonitor-web" ./features/web

"$BUILD_DIR/catmonitor" version
"$BUILD_DIR/catmonitor" stress --help
```

如果 `go mod verify` 因未缓存的非构建依赖失败，最终以两个 `GOPROXY=off go build`
是否成功为准；`go list -m all` 不适合作为离线构建的必要条件。

## 4. 新装节点脚本

仅在节点还没有正式脚本时，从模板创建部署副本：

```bash
install -d -m 0750 /etc/catmonitor /var/lib/catmonitor
install -m 0750 \
  "$CAT_ROOT/features/stress/benchmark_check.sh" \
  /etc/catmonitor/benchmark_check.sh
```

编辑脚本顶部配置区，写入节点真实值：

- STREAM：执行器、`numactl` 和线程数；
- HPL：工作目录、执行器、库目录、MPI launcher、进程数和线程数；
- HPCG：工作目录、执行器、MPI launcher、进程数、线程数、网格和运行时长。

所有执行器、工作目录和 launcher 必须使用绝对路径。仓库模板的 HPL/HPCG
命令只使用 MPICH/Hydra 与 OpenMPI 共同支持的 `-np`；厂商专用绑核、通信和
root 参数只能在验证过的节点部署副本中维护。

## 5. 升级现有节点脚本

已有实机脚本时，从新模板生成 candidate，再迁移节点变量：

```bash
OLD_SCRIPT=/etc/catmonitor/benchmark_check.sh
CANDIDATE=/etc/catmonitor/benchmark_check.sh.candidate.$STAMP
NEW_TEMPLATE="$CAT_ROOT/features/stress/benchmark_check.sh"

test -f "$OLD_SCRIPT"
cp -a "$NEW_TEMPLATE" "$CANDIDATE"
chmod 0750 "$CANDIDATE"
```

下面的脚本只复制已定义的配置赋值，不复制旧实现逻辑。若变量缺失或新模板中
出现重复定义，它会失败而不会写回不完整结果：

```bash
python3 - "$OLD_SCRIPT" "$CANDIDATE" <<'PY'
from pathlib import Path
import re
import sys

old_path = Path(sys.argv[1])
new_path = Path(sys.argv[2])
old = old_path.read_text(encoding="utf-8")
new = new_path.read_text(encoding="utf-8")

variables = [
    "STREAM_EXECUTABLE", "STREAM_NUMACTL", "STREAM_THREADS",
    "HPL_WORKDIR", "HPL_EXECUTABLE", "HPL_LIBRARY_DIR",
    "HPL_MPI_LAUNCHER", "HPL_MPI_PROCESSES", "HPL_THREADS_PER_PROCESS",
    "HPCG_WORKDIR", "HPCG_EXECUTABLE", "HPCG_MPI_LAUNCHER",
    "HPCG_MPI_PROCESSES", "HPCG_THREADS_PER_PROCESS",
    "HPCG_NX", "HPCG_NY", "HPCG_NZ", "HPCG_RUNTIME_SECONDS",
]

errors = []
for name in variables:
    old_match = re.search(rf"^{re.escape(name)}=.*$", old, re.MULTILINE)
    new_matches = list(re.finditer(rf"^{re.escape(name)}=.*$", new, re.MULTILINE))
    if old_match is None:
        errors.append(f"旧脚本缺少变量：{name}")
        continue
    if len(new_matches) != 1:
        errors.append(f"新模板中的 {name} 数量不是 1：{len(new_matches)}")
        continue
    new = re.sub(
        rf"^{re.escape(name)}=.*$",
        lambda _: old_match.group(0),
        new,
        count=1,
        flags=re.MULTILINE,
    )

if errors:
    raise SystemExit("\n".join(f"ERROR: {item}" for item in errors))

new_path.write_text(new, encoding="utf-8")
print("节点变量迁移完成")
PY
```

旧脚本若早于上述变量模型，应停止自动迁移，人工把旧参数映射到新模板顶部，
不要为了通过脚本而复制旧的 `case`、解析函数或 MPI 命令实现。

检查候选脚本：

```bash
bash -n "$CANDIDATE"
grep -nE '^(STREAM_|HPL_|HPCG_)' "$CANDIDATE"
diff -u "$OLD_SCRIPT" "$CANDIDATE" | sed -n '1,260p' || true
```

不要用普通文本搜索把 Bash 的 `[ -x "$file" ]` 误判为 OpenMPI 的 `-x` 参数。
若要检查仓库通用模板，应只匹配命令行起始位置：

```bash
if grep -nE -- \
  '^[[:space:]]*(-x[[:space:]]|--allow-run-as-root([[:space:]]|$)|--map-by([[:space:]]|$)|--bind-to([[:space:]]|$)|-mca[[:space:]])' \
  "$CANDIDATE"
then
  echo 'ERROR: candidate contains unreviewed OpenMPI-specific arguments' >&2
  exit 1
fi
```

## 6. `describe` 无副作用预检

candidate 必须先通过三项描述检查：

```bash
for benchmark in stream hpl hpcg; do
  output="/tmp/catmonitor-describe-$benchmark.json"
  if ! "$CANDIDATE" describe "$benchmark" > "$output"; then
    echo "ERROR: describe $benchmark failed" >&2
    cat "$output" >&2
    exit 1
  fi
  python3 -m json.tool "$output"
done
```

`describe` 不得启动 benchmark，也不得生成新的 HPL/HPCG 结果文件。逐项确认：

- `protocol_version` 为 `1`，benchmark 名称正确；
- `parameters` 是节点实际路径与运行参数；
- 必需 `assets` 为 `pass`，文件资产带 SHA-256；
- `resources` 与 CPU、线程、MPI 进程数和问题规模一致；
- `mpi.status` 不得为明确的 ABI `fail`；无法判断时允许带原因的 `warn`；
- `preflight.status` 为 `pass`，或为经过人工确认的 `warn`。

MPI launcher 必须与 HPL/HPCG 编译时使用的 MPI ABI 匹配：

```bash
ldd /absolute/path/to/xhpl | grep -Ei 'mpi|mpich|open-rte|open-pal|pmix'
ldd /absolute/path/to/xhpcg | grep -Ei 'mpi|mpich|open-rte|open-pal|pmix'
/absolute/path/to/mpirun --version
```

## 7. 原子切换与统一配置

仅在 candidate 的三项 `describe` 都通过后切换。先停止旧 Web，再替换脚本和
候选二进制：

```bash
WEB_PID=$(pgrep -xo catmonitor-web || true)
if [ -n "$WEB_PID" ]; then kill "$WEB_PID"; fi

install -m 0750 "$CANDIDATE" /etc/catmonitor/benchmark_check.sh.new
mv -f /etc/catmonitor/benchmark_check.sh.new \
  /etc/catmonitor/benchmark_check.sh

install -d -m 0755 "$CAT_ROOT/bin"
install -m 0755 "$BUILD_DIR/catmonitor" "$CAT_ROOT/bin/catmonitor.new"
install -m 0755 "$BUILD_DIR/catmonitor-web" "$CAT_ROOT/bin/catmonitor-web.new"
mv -f "$CAT_ROOT/bin/catmonitor.new" "$CAT_ROOT/bin/catmonitor"
mv -f "$CAT_ROOT/bin/catmonitor-web.new" "$CAT_ROOT/bin/catmonitor-web"
```

主配置 `/etc/catmonitor/catmonitor.yaml` 使用顶层 `stress:`，不能放在
`health.stress`：

```yaml
stress:
  enabled: true
  web_enabled: false
  script_path: /etc/catmonitor/benchmark_check.sh
  report_path: /var/lib/catmonitor/stress-latest.json
  default_benchmarks: [stream]
  benchmarks:
    stream: { enabled: true, timeout: 1m }
    hpl: { enabled: true, timeout: 10m }
    hpcg:
      enabled: true
      result_dir: /absolute/path/to/hpcg/results
      timeout: 3m
```

说明：

- `enabled` 启用 stress 特性；
- `web_enabled` 只授权 Web 提交，CLI 不依赖它；
- `script_path` 指向节点部署副本；
- `report_path` 同时派生 history 和跨进程锁；
- STREAM/HPL/HPCG 的运行参数不写入 YAML；
- 只有 HPCG 需要 `result_dir`，用于读取本次生成或变化的结果文件；
- Web 的单次超时只能缩短 YAML 上限，不会修改配置文件。

新版 Web 不再使用独立 YAML。它是 daemon snapshot 的只读消费者，通过命令行
参数取得监听地址和 snapshot 目录，并从平台默认路径读取 CATMonitor 主配置：

```text
-addr 127.0.0.1:9527
-snapshot-dir /var/lib/catmonitor/snapshot
```

Linux 默认主配置为 `/etc/catmonitor/catmonitor.yaml`，因此常规部署无需额外参数。
非标准路径可用 `CATMONITOR_CONFIG` 环境变量或
`-config /path/to/catmonitor.yaml` 覆盖，显式 flag 优先。

## 8. CLI 实机验收

切换后再次验证正式脚本：

```bash
bash -n /etc/catmonitor/benchmark_check.sh
for benchmark in stream hpl hpcg; do
  /etc/catmonitor/benchmark_check.sh describe "$benchmark" \
    | python3 -m json.tool
done
```

按 STREAM、HPCG、HPL 逐项运行；首次不要同时选择三项：

```bash
cd "$CAT_ROOT"

./bin/catmonitor stress --bench stream -o table

./bin/catmonitor stress --bench hpcg -o table

./bin/catmonitor stress --bench hpl -o table
```

需要机器可读输出时使用 `-o json`。表格中的成功状态显示为 `OK`，JSON 使用稳定
内部状态 `healthy`，二者语义相同。

每次作业后检查：

```bash
python3 -m json.tool /var/lib/catmonitor/stress-latest.json
python3 -m json.tool /var/lib/catmonitor/stress-history.json
pgrep -af '[s]tream_omp|[x]hpl|[x]hpcg|[m]pirun|[n]umactl' || true
```

正常结束、主动取消或达到时限后都不应残留 benchmark、MPI 或 NUMA 进程。

## 9. Web 与 Windows 隧道验收

CLI 三项通过后，将主配置的 `stress.web_enabled` 改为 `true`，再启动 Web：

```bash
cd "$CAT_ROOT"
./bin/catmonitor-web \
  -addr 127.0.0.1:9527 \
  -snapshot-dir /var/lib/catmonitor/snapshot
```

另开 Linux 终端检查：

```bash
ss -lntp | grep ':9527'
curl -I http://127.0.0.1:9527/stress/
curl -fsS http://127.0.0.1:9527/api/stress/config \
  | python3 -m json.tool
curl -fsS 'http://127.0.0.1:9527/api/stress/history?limit=20' \
  | python3 -m json.tool
```

Windows PowerShell 通过 SSH 隧道访问，不要把控制接口直接暴露到业务网络：

```powershell
ssh -N `
  -o ExitOnForwardFailure=yes `
  -L 127.0.0.1:19527:127.0.0.1:9527 `
  user@linux-host
```

浏览器打开：

```text
http://127.0.0.1:19527/stress/
```

页面应在作业启动前显示真实执行 profile：执行器、MPI launcher、进程/线程数、
问题规模、资产状态、ABI 预检和配置哈希；页面不得提供脚本、绝对路径或任意参数
编辑入口。展开的 profile 在 2 秒自动刷新后仍应保持展开。

## 10. 报告、历史和配置哈希

`report_path` 保存当前/最近作业。最终作业还会写入同目录的
`stress-history.json`，按新到旧最多保留 100 条。`latest` 只显示一条是正常设计，
历史页用于切换此前的 STREAM、HPL 和 HPCG 作业。

报告应保存：

- `initiator` 和 `job_id`；
- 每项实际执行 profile；
- 脚本及输入资产 SHA-256；
- 每项和聚合 `configuration_sha256`；
- 实际超时、运行状态、耗时与指标。

在 Web 中对同一 benchmark 使用一次默认时限、一次更短的单次时限，预期：

- 脚本和输入资产哈希不变；
- 实际配置哈希变化；
- YAML 文件没有被修改；
- 作业达到较短窗口且此前没有错误时，状态为 `time_limit_reached` 并按通过展示。

## 11. CLI/Web 互斥验收

在 CLI 运行 HPCG 或 HPL 时观察 Web：

- Web 在约 2 秒内显示相同 `job_id` 和 `initiator=cli`；
- Web 不允许取消 CLI 发起的作业；
- Web 同时提交新作业应被拒绝；
- 反向由 Web 运行时，第二个 CLI 也应收到“已有作业运行”的错误；
- 任一作业完成后，两端读取到同一最终报告。

实际互斥由 `${report_path}.lock` 的非阻塞内核文件锁实现。锁文件留在磁盘是正常
现象，不能用文件是否存在判断是否正在运行；进程退出后内核会释放锁。

## 12. 状态判定

| 状态 | 含义 | 通过 |
|---|---|---|
| `healthy` | 命令成功且必需结果已解析 | 是 |
| `time_limit_reached` | 达到配置窗口，停止前没有检测到错误 | 是 |
| `running` | 正在运行 | 未完成 |
| `cancelled` | 用户主动取消 | 否 |
| `unhealthy` | 命令、校验或解析失败 | 否 |
| `unavailable` / `unsupported` | 资产、配置或平台不满足 | 否 |

HPL、HPCG 和 STREAM 都允许以“受控运行窗口”工作。达到 CATMonitor 时限前没有
检测到错误时，即使尚未产生 GFLOP/s 或 MB/s，也记录为通过；正常退出时则必须
完成各自结果校验和必需指标解析，不设置性能阈值。

## 13. 回滚

升级异常时停止 Web，并从本次备份恢复：

```bash
WEB_PID=$(pgrep -xo catmonitor-web || true)
if [ -n "$WEB_PID" ]; then kill "$WEB_PID"; fi

install -m 0750 "$BACKUP_ROOT/benchmark_check.sh" \
  /etc/catmonitor/benchmark_check.sh
install -m 0755 "$BACKUP_ROOT/catmonitor" \
  "$CAT_ROOT/bin/catmonitor"
install -m 0755 "$BACKUP_ROOT/catmonitor-web" \
  "$CAT_ROOT/bin/catmonitor-web"

cp -a "$BACKUP_ROOT/catmonitor.yaml" /etc/catmonitor/catmonitor.yaml
```

备份中某个文件原本不存在时跳过对应恢复命令。

## 14. 最终验收清单

- [ ] CLI 和 Web 在目标架构构建成功
- [ ] 正式节点脚本位于源码目录外并通过 `bash -n`
- [ ] 三项 `describe` 无副作用且无阻断性资产/ABI 错误
- [ ] 主配置只有顶层 `stress:`，Web 默认读取平台路径且可显式覆盖
- [ ] CLI 依次完成 STREAM、HPCG、HPL
- [ ] JSON 报告、历史、profile 和配置哈希完整
- [ ] 正常结束、取消和超时后无残留进程
- [ ] Web 只监听回环地址并通过 SSH 隧道访问
- [ ] profile 展开状态不被自动刷新清除
- [ ] CLI 与 Web 双向共享报告并拒绝并发作业
- [ ] 单次缩短超时会改变执行配置哈希但不修改 YAML
- [ ] 回滚文件和操作步骤已验证

推荐顺序：

```text
备份
→ 临时目录构建 CLI/Web
→ 新模板生成 candidate
→ 迁移节点变量
→ candidate describe
→ 原子切换
→ CLI：STREAM → HPCG → HPL
→ 检查 report/history/profile/hash
→ 启动 Web
→ 验证页面、单次超时与跨进程互斥
```
