# CATMonitor：Ascend NPU 节点

> 最新“按作业参数 / 模型 SDC”候选统一使用 `codex/stress-job-parameters`，
> 完整获取、A2/A3 镜像区分及 A3 五容器部署见 [候选交付指南](STRESS_JOB_RELEASE_GUIDE.md)。
> 本页旧 `arm64-v0.3.5-stress` 是 Monitoring/Stress V2 历史基线，不包含本轮全部功能。

适用于 Ascend NPU Linux 节点。支持 Ascend Monitoring、可选 CPU Stress 和可选
NPU Burn。当前 Stress 镜像只声明已完成 A2/Ascend910B4、CANN 8.3、`runc` 的功能验收；
其他 SoC/CANN 组合必须单独验收，不能由 A2 结果外推。

Docker Compose 是推荐入口；本页同时给出完整的 `docker run` 入口。两种入口使用
相同镜像、generator 配置、挂载、权限和 3/4/4/5 容器模型。

不想反复组合多个 `-f` 时，使用 [Compose 薄启动入口](README.md#61-简化-compose-启动与查询实际端口)。
Web/DFeE 会在起始端口占用时自动递增；启动后用 `urls` 查询实际地址，不要把
默认 19322 写成每次都保证的监听端口。

> 当前程序内部版本是 `0.3.6`；已发布的 ARM64 Stress pre-release 镜像标签是
> `arm64-v0.3.5-stress`（基于 v0.3.5 时期代码构建），正式 `v0.3.6` 镜像发布后替换。

## 1. 前置条件

- Linux/arm64；
- Docker Engine；
- 推荐 Docker Compose v2；旧 `docker-compose` 通过薄入口兼容时另需 Python 3 +
  PyYAML；只使用手工路径时不要求 Compose；
- 使用 NPU 手工路径时安装 Bash 与 Python 3，用于读取 generator 的
  `stress-profile.json`，不解析 Compose YAML；
- Ascend 驱动可用，至少存在一个 `/dev/davinciN`；
- `npu-smi info` 正常；
- NPU workload 镜像中的 CANN/torch_npu 与节点驱动兼容。

```bash
uname -m
npu-smi info
ls -1 /dev/davinci[0-9]*
docker version
docker compose version 2>/dev/null || true
python3 --version
```

generator 会动态发现实际存在的 `/dev/davinciN`。设备号不要求从 0 连续，不能用
最大设备号推导设备数量。

## 2. 镜像与源码

```bash
git clone https://github.com/Computing-Availability-Tools/CATMonitor.git
cd CATMonitor
git checkout develop   # v0.3.6（Stress V2 已合并，PR #13）

export CATMONITOR_RELEASE='arm64-v0.3.5-stress'
export CATMONITOR_REGISTRY='ghcr.io/spike677'
export CATMONITOR_IMAGE="${CATMONITOR_REGISTRY}/catmonitor-npu:${CATMONITOR_RELEASE}"
export CATMONITOR_CPU_STRESS_IMAGE="${CATMONITOR_REGISTRY}/catmonitor-stress-cpu:${CATMONITOR_RELEASE}"
export CATMONITOR_NPU_STRESS_IMAGE="${CATMONITOR_REGISTRY}/catmonitor-stress-npu:${CATMONITOR_RELEASE}"
```

按实际能力拉取，未启用 NPU Burn 时无需下载较大的 NPU workload 镜像：

```bash
docker pull "$CATMONITOR_IMAGE"
# CPU Stress 可选
docker pull "$CATMONITOR_CPU_STRESS_IMAGE"
# NPU Stress 可选
docker pull "$CATMONITOR_NPU_STRESS_IMAGE"
```

需要从源码构建 Ascend Control、CPU/NPU workload，选择 CANN builder/runtime base，
配置 Debian mirror，或制作 RC 镜像时，请使用
[镜像构建与发布开发者指南](DEVELOPER_GUIDE.md)。发布镜像必须来自同一 Stress
release source，不得复用 a2-r1 的镜像身份。

## 3. Monitoring-only

不启用 Stress 时只运行 `catmonitor`、`catmonitor-web`、`catmonitor-dfee`。不挂
Docker Socket，不启动 workload 容器，也不要求真实 `control.sock`。

### 3.1 Compose

```bash
export CATMONITOR_CONFIG="$PWD/docker/catmonitor.yaml"

CATMONITOR_IMAGE="$CATMONITOR_IMAGE" \
CATMONITOR_CONFIG="$CATMONITOR_CONFIG" \
  docker compose -p catmonitor \
  -f docker/docker-compose.yml \
  -f docker/docker-compose.config.yml \
  -f docker/docker-compose.npu.yml \
  up -d
```

### 3.2 Manual `docker run`

这是完整兼容入口。保留 Web 的历史 `-config` flag，但不挂 Stress 配置或
control volume。

```bash
docker volume create cm-snapshot
docker volume create cm-data
docker volume create cm-straggler
docker volume create cm-csv

docker run -d --name catmonitor --restart unless-stopped \
  --privileged --network host --pid host \
  -v /:/host:ro \
  -v /etc/os-release:/etc/os-release:ro \
  -v /usr/local/Ascend/driver:/usr/local/Ascend/driver:ro \
  -v /usr/local/Ascend/nnae:/usr/local/Ascend/nnae:ro \
  -v /usr/local/Ascend/ascend-toolkit:/usr/local/Ascend/ascend-toolkit:ro \
  -v /usr/bin/hccn_tool:/usr/bin/hccn_tool:ro \
  -v /usr/local/sbin/npu-smi:/usr/local/sbin/npu-smi:ro \
  -e LD_LIBRARY_PATH=/usr/local/Ascend/driver/lib64/driver:/usr/local/Ascend/driver/lib64/common:/usr/local/Ascend/ascend-toolkit/latest/aarch64-linux/lib64:/usr/local/Ascend/nnae/latest/lib64 \
  -v cm-snapshot:/var/lib/catmonitor/snapshot \
  -v cm-data:/var/lib/catmonitor/data \
  -v cm-straggler:/var/lib/catmonitor/straggler \
  "$CATMONITOR_IMAGE"

docker run -d --name catmonitor-web --restart unless-stopped --network host \
  --entrypoint /usr/local/bin/web \
  -v cm-snapshot:/var/lib/catmonitor/snapshot:ro \
  "$CATMONITOR_IMAGE" \
  -addr=:19322 \
  -snapshot-dir=/var/lib/catmonitor/snapshot \
  -config=/etc/catmonitor/catmonitor.yaml

docker run -d --name catmonitor-dfee --restart unless-stopped --network host \
  --entrypoint /usr/local/bin/dfee \
  -v cm-snapshot:/var/lib/catmonitor/snapshot:ro \
  -v cm-csv:/var/lib/catmonitor/csv \
  "$CATMONITOR_IMAGE" \
  -addr=:19323 \
  -snapshot-dir=/var/lib/catmonitor/snapshot \
  -exporter=enabled \
  -exporter-port=9333 \
  -csv=disabled \
  -csv-dir=/var/lib/catmonitor/csv \
  -csv-interval=10s
```

```bash
test "$(docker ps --filter name='^/catmonitor$' --filter status=running -q | wc -l)" -eq 1
test "$(docker ps --filter name='^/catmonitor-web$' --filter status=running -q | wc -l)" -eq 1
test "$(docker ps --filter name='^/catmonitor-dfee$' --filter status=running -q | wc -l)" -eq 1
docker exec catmonitor test -s /var/lib/catmonitor/snapshot/snapshot_npu.json
curl -fsS http://127.0.0.1:19320/-/ready
curl -fsS http://127.0.0.1:19322/ >/dev/null
curl -fsS http://127.0.0.1:19323/ >/dev/null
curl -fsS http://127.0.0.1:9333/metrics >/dev/null
```

### 3.3 Verify

Web 中 Stress 显示未启用；旧 Monitoring YAML 可以完全没有顶层 `stress:` 段。

### 3.4 只启动部分 Monitoring 服务

```bash
# daemon + DFeE（跳过 Web）
CATMONITOR_IMAGE="$CATMONITOR_IMAGE" \
CATMONITOR_CONFIG="$CATMONITOR_CONFIG" \
  docker compose -p catmonitor \
  -f docker/docker-compose.yml \
  -f docker/docker-compose.config.yml \
  -f docker/docker-compose.npu.yml \
  up -d catmonitor dfee

# 只启动 daemon
CATMONITOR_IMAGE="$CATMONITOR_IMAGE" \
CATMONITOR_CONFIG="$CATMONITOR_CONFIG" \
  docker compose -p catmonitor \
  -f docker/docker-compose.yml \
  -f docker/docker-compose.config.yml \
  -f docker/docker-compose.npu.yml \
  up -d catmonitor
```

### 3.5 自定义 Monitoring 配置

Monitoring 公共配置路径保持不变：

| 文件 | 容器内路径 | 所有者 |
|---|---|---|
| 主配置 | `/etc/catmonitor/catmonitor.yaml` | daemon |
| 指标目录 | `/etc/catmonitor/metrics.yaml` | daemon |

Compose 通过 `CATMONITOR_CONFIG` 挂载主配置。手工启动时如需替换两份配置，把下面
两个只读 bind mount 加到 `catmonitor` 容器；Web/DFeE 只消费 snapshot，不重复读取
daemon 配置。

```bash
-v /path/to/my-catmonitor.yaml:/etc/catmonitor/catmonitor.yaml:ro \
-v /path/to/my-metrics.yaml:/etc/catmonitor/metrics.yaml:ro
```

旧 Monitoring YAML 不含 `stress:` 时仍可启动，且不会创建 Stress Manager、
`control.sock` 或 Docker executor。旧 Stress V1 YAML 不兼容 V2。

### 3.6 独立运行 DFeE

daemon 已在宿主机写入 `/var/lib/catmonitor/snapshot` 时：

```bash
sudo install -d -m 0750 /var/lib/catmonitor/csv

docker run -d --name catmonitor-dfee --network host \
  --entrypoint /usr/local/bin/dfee \
  -v /var/lib/catmonitor/snapshot:/var/lib/catmonitor/snapshot:ro \
  -v /var/lib/catmonitor/csv:/var/lib/catmonitor/csv \
  "$CATMONITOR_IMAGE" \
  -addr=:19323 \
  -snapshot-dir=/var/lib/catmonitor/snapshot \
  -exporter=enabled -exporter-port=9333 \
  -csv=disabled -csv-dir=/var/lib/catmonitor/csv
```

daemon 位于另一个容器时改为共享 `cm-snapshot` named volume。DFeE 不需要 Ascend
设备、driver/toolkit 或 Docker Socket，它消费 daemon 已生成的 snapshot。

### 3.7 Monitoring 端口与数据

| 端口 | 组件 | 用途 |
|---:|---|---|
| `19320` | daemon | `/metrics`、`/-/healthy`、`/-/ready` |
| `19321` | faultsub | 启用后提供故障订阅 REST API |
| `19322` | Web | Monitoring；启用 V2 Stress 后同时提供 Run/Cancel/History |
| `19323` | DFeE | 能效页面与 API |
| `9333` | DFeE exporter | Prometheus metrics |

| 卷/宿主目录 | 容器内路径 | 用途 |
|---|---|---|
| `cm-snapshot` | `/var/lib/catmonitor/snapshot` | daemon 写，Web/DFeE 只读 |
| `cm-data` | `/var/lib/catmonitor/data` | daemon 原始 JSONL；按 retention 管理 |
| `cm-straggler`（可选） | `/var/lib/catmonitor/straggler` | straggler 专用输出 |
| `cm-csv` | `/var/lib/catmonitor/csv` | DFeE CSV |

要启用 faultsub，在 daemon 的 `catmonitor.yaml` 中设置：

```yaml
faultsub:
  enabled: true
  rest_addr: ":19321"
  webhook_timeout: 5s
  webhook_retry: 1
  event_buffer: 1024
  defaults:
    debounce_ms: 0
    min_severity: warning
```

可选的 straggler 专用输出仍使用原路径：

```yaml
straggler_output:
  enabled: true
  data_dir: /var/lib/catmonitor/straggler
  retention: 360h
  flush_interval: 60s
```

验证当前故障与最近事件：

```bash
curl -fsS http://127.0.0.1:19321/-/ready
curl -fsS http://127.0.0.1:19321/faultsub/snapshot
curl -fsS http://127.0.0.1:19321/faultsub/events
```

### 3.8 DFeE Exporter、Prometheus 与 Grafana

```bash
curl -fsS http://127.0.0.1:9333/metrics | head
```

Prometheus 增加：

```yaml
scrape_configs:
  - job_name: CATMonitor
    scrape_interval: 2s
    static_configs:
      - targets: ["<dfee_exporter_ip>:9333"]
```

在 `http://<prometheus-address>:9090/targets` 确认 target 为 `UP`，再在 Grafana 导入
`features/dfee/grafana-dashboard.json`。完整 DFeE 参数与独立二进制用法见
[DFeE 使用文档](../features/dfee/USAGE.md)。

## 4. Stress 运行目录

generator 只生成节点配置和只读 metadata，不启动或删除容器。以下 A2 参数是当前
96 核验收节点使用的 profile；换机器时必须让 HPL 的 MPI ranks 与 `HPL.dat` 的
`P×Q` 一致，并按在线 CPU 数设置 HPCG ranks。

```bash
export CATMONITOR_GENERATED_DIR=/etc/catmonitor/generated-stress
export CATMONITOR_STRESS_STATE_DIR=/var/lib/catmonitor/stress
export CATMONITOR_NPU_OUTPUT_DIR=/var/lib/catmonitor/stress/npu-burn-output

sudo install -d -m 0750 \
  "$CATMONITOR_GENERATED_DIR" \
  "$CATMONITOR_STRESS_STATE_DIR" \
  "$CATMONITOR_NPU_OUTPUT_DIR"
```

## 5. CPU Stress

### 5.1 Generate

```bash
sudo bash scripts/stress/generate_stress_deployment.sh \
  --output-dir "$CATMONITOR_GENERATED_DIR" \
  --control-image "$CATMONITOR_IMAGE" \
  --cpu-image "$CATMONITOR_CPU_STRESS_IMAGE" \
  --stream-threads 0 \
  --hpl-processes 8 \
  --hpl-threads 12 \
  --hpcg-processes 96 \
  --hpcg-threads 1 \
  --enable-web \
  --force
```

### 5.2 Compose

先执行第 4 节 CPU-only generator，然后：

```bash
CATMONITOR_IMAGE="$CATMONITOR_IMAGE" \
CATMONITOR_STRESS_STATE_DIR="$CATMONITOR_STRESS_STATE_DIR" \
  docker compose -p catmonitor \
  -f docker/docker-compose.yml \
  -f docker/docker-compose.npu.yml \
  -f docker/docker-compose.stress.yml \
  -f "$CATMONITOR_GENERATED_DIR/docker-compose.stress.generated.yml" \
  --profile stress-cpu up -d
```

### 5.3 Manual `docker run`

先执行第 4 节 CPU-only generator。下面命令完整创建 CPU workload、daemon、Web 与
DFeE 共 4 个容器；只有 daemon 挂 Docker Socket。

```bash
docker volume create cm-snapshot
docker volume create cm-data
docker volume create cm-straggler
docker volume create cm-control
docker volume create cm-csv
docker volume create cm-stress-cpu-state

docker run -d --name catmonitor-stress-cpu --restart unless-stopped \
  --read-only --network none \
  --cap-drop ALL \
  --cap-add CHOWN --cap-add DAC_OVERRIDE --cap-add FOWNER \
  --cap-add SETGID --cap-add SETPCAP --cap-add SETUID --cap-add SYS_NICE \
  --security-opt no-new-privileges:true \
  --pids-limit 4096 --shm-size=16g \
  --tmpfs /tmp:rw,noexec,nosuid,nodev,size=64m \
  -e STREAM_THREADS=0 \
  -e HPL_MPI_PROCESSES=8 \
  -e HPL_THREADS_PER_PROCESS=12 \
  -e HPCG_MPI_PROCESSES=96 \
  -e HPCG_THREADS_PER_PROCESS=1 \
  -e HPCG_NX=32 -e HPCG_NY=32 -e HPCG_NZ=32 \
  -e HPCG_RUNTIME_SECONDS=60 \
  -v cm-stress-cpu-state:/var/lib/catmonitor/stress \
  --health-cmd='/usr/bin/setpriv --bounding-set=-all --inh-caps=-all --ambient-caps=-all --reuid=65532 --regid=65532 --init-groups --no-new-privs /usr/local/bin/catmonitor-stress-exec describe --benchmark stream --json' \
  --health-interval=5s --health-timeout=3s --health-retries=12 \
  --health-start-period=5s \
  "$CATMONITOR_CPU_STRESS_IMAGE"

docker run -d --name catmonitor --restart unless-stopped \
  --privileged --network host --pid host \
  -v /:/host:ro \
  -v /etc/os-release:/etc/os-release:ro \
  -v /usr/local/Ascend/driver:/usr/local/Ascend/driver:ro \
  -v /usr/local/Ascend/nnae:/usr/local/Ascend/nnae:ro \
  -v /usr/local/Ascend/ascend-toolkit:/usr/local/Ascend/ascend-toolkit:ro \
  -v /usr/bin/hccn_tool:/usr/bin/hccn_tool:ro \
  -v /usr/local/sbin/npu-smi:/usr/local/sbin/npu-smi:ro \
  -e LD_LIBRARY_PATH=/usr/local/Ascend/driver/lib64/driver:/usr/local/Ascend/driver/lib64/common:/usr/local/Ascend/ascend-toolkit/latest/aarch64-linux/lib64:/usr/local/Ascend/nnae/latest/lib64 \
  -v "$CATMONITOR_GENERATED_DIR/catmonitor-stress.yaml:/etc/catmonitor/catmonitor.yaml:ro" \
  -v /var/run/docker.sock:/var/run/docker.sock \
  -v "$CATMONITOR_STRESS_STATE_DIR:/var/lib/catmonitor/stress" \
  -v cm-snapshot:/var/lib/catmonitor/snapshot \
  -v cm-data:/var/lib/catmonitor/data \
  -v cm-straggler:/var/lib/catmonitor/straggler \
  -v cm-control:/run/catmonitor \
  "$CATMONITOR_IMAGE"

docker run -d --name catmonitor-web --restart unless-stopped --network host \
  --entrypoint /usr/local/bin/web \
  -v cm-snapshot:/var/lib/catmonitor/snapshot:ro \
  -v cm-control:/run/catmonitor:ro \
  "$CATMONITOR_IMAGE" \
  -addr=:19322 \
  -snapshot-dir=/var/lib/catmonitor/snapshot \
  -control-socket=/run/catmonitor/control.sock

docker run -d --name catmonitor-dfee --restart unless-stopped --network host \
  --entrypoint /usr/local/bin/dfee \
  -v cm-snapshot:/var/lib/catmonitor/snapshot:ro \
  -v cm-csv:/var/lib/catmonitor/csv \
  "$CATMONITOR_IMAGE" \
  -addr=:19323 \
  -snapshot-dir=/var/lib/catmonitor/snapshot \
  -exporter=enabled -exporter-port=9333 \
  -csv=disabled -csv-dir=/var/lib/catmonitor/csv -csv-interval=10s
```

与 canonical Compose 的契约对照：Control 使用 host network/PID、privileged、
generated YAML、state、control 与 Docker Socket；CPU workload 使用 `network none`、
只读根文件系统、最小 capabilities、16 GiB shm、只读安全选项和独立可写 state；
Web/DFeE 不挂 Docker Socket。

### 5.4 Doctor / Run / Cancel

```bash
docker exec catmonitor catmonitor stress doctor \
  -c /etc/catmonitor/catmonitor.yaml -o table
docker exec catmonitor catmonitor stress run --bench stream -o table
docker exec catmonitor catmonitor stress status -o table
docker exec catmonitor catmonitor stress cancel --job '<job-id>'
docker top catmonitor-stress-cpu | grep -E 'xhpl|xhpcg|mpirun|hydra' && exit 1 || true
```

## 6. NPU Stress

### 6.1 Generate

```bash
sudo bash scripts/stress/generate_stress_deployment.sh \
  --output-dir "$CATMONITOR_GENERATED_DIR" \
  --control-image "$CATMONITOR_IMAGE" \
  --npu-image "$CATMONITOR_NPU_STRESS_IMAGE" \
  --npu-burn-device all \
  --npu-chip-generation A2 \
  --npu-runtime runc \
  --enable-web \
  --force
```

### 6.2 Device mapping

手工 NPU 命令从 `stress-profile.json` 读取 host IDs、映射数量与 runtime logical IDs：

```bash
export CATMONITOR_STRESS_PROFILE="$CATMONITOR_GENERATED_DIR/stress-profile.json"
mapfile -t CATMONITOR_NPU_PROFILE_FIELDS < <(
  python3 - "$CATMONITOR_STRESS_PROFILE" <<'PY'
import json
import sys

with open(sys.argv[1], encoding="utf-8") as stream:
    profile = json.load(stream)
npu = profile["npu"]
ids = npu["host_device_ids"]
if not npu["enabled"] or not ids:
    raise SystemExit("generated NPU profile is disabled or empty")
print(len(ids))
print(npu["runtime_visible_device_ids"])
print(npu["burn_logical_ids"])
for device_id in ids:
    print(device_id)
PY
)

export CATMONITOR_NPU_DEVICE_COUNT="${CATMONITOR_NPU_PROFILE_FIELDS[0]}"
export CATMONITOR_NPU_RUNTIME_VISIBLE_DEVICES="${CATMONITOR_NPU_PROFILE_FIELDS[1]}"
export CATMONITOR_NPU_BURN_DEVICE="${CATMONITOR_NPU_PROFILE_FIELDS[2]}"
CATMONITOR_NPU_DEVICE_ARGS=()
for device_id in "${CATMONITOR_NPU_PROFILE_FIELDS[@]:3}"; do
  test -e "/dev/davinci${device_id}"
  CATMONITOR_NPU_DEVICE_ARGS+=(
    "--device=/dev/davinci${device_id}:/dev/davinci${device_id}"
  )
done
test "${#CATMONITOR_NPU_DEVICE_ARGS[@]}" -eq "$CATMONITOR_NPU_DEVICE_COUNT"
```

例如宿主机观察到 `/dev/davinci2` 与 `/dev/davinci5`，只表示两个 host device
node；它们不代表 NPU Burn logical ID 是 `2,5`。generator 会得到 mapped count 2，
runtime logical namespace `0,1`，并在 workload preflight 中与 PCI topology、
`torch_npu.npu.device_count()` 交叉检查。

### 6.3 Compose

先执行第 4 节 NPU-only generator，然后：

```bash
CATMONITOR_IMAGE="$CATMONITOR_IMAGE" \
CATMONITOR_STRESS_STATE_DIR="$CATMONITOR_STRESS_STATE_DIR" \
  docker compose -p catmonitor \
  -f docker/docker-compose.yml \
  -f docker/docker-compose.npu.yml \
  -f docker/docker-compose.stress.yml \
  -f "$CATMONITOR_GENERATED_DIR/docker-compose.stress.generated.yml" \
  --profile stress-npu up -d
```

### 6.4 Manual `docker run`

先执行第 4 节 NPU-only generator和 profile 读取块。下面命令动态使用 generator
发现的所有 host nodes，不允许手工把稀疏 host ID 当成 NPU Burn logical ID。

```bash
docker volume create cm-snapshot
docker volume create cm-data
docker volume create cm-straggler
docker volume create cm-control
docker volume create cm-csv
docker volume create cm-stress-npu-state

docker run -d --name catmonitor-stress-npu --restart unless-stopped \
  --runtime runc --privileged --read-only --network none \
  "${CATMONITOR_NPU_DEVICE_ARGS[@]}" \
  --device=/dev/davinci_manager:/dev/davinci_manager \
  --device=/dev/devmm_svm:/dev/devmm_svm \
  --device=/dev/hisi_hdc:/dev/hisi_hdc \
  --tmpfs /tmp:rw,nosuid,nodev,size=256m \
  --tmpfs /opt/catmonitor/npuburn-home:rw,nosuid,nodev,size=1g,mode=0750 \
  --env-file "$CATMONITOR_GENERATED_DIR/npu-workload.env" \
  -v cm-stress-npu-state:/var/lib/catmonitor/stress \
  -v "$CATMONITOR_NPU_OUTPUT_DIR:/opt/catmonitor/npuburn-home/.ascend_npu_burn/output" \
  -v /sys/bus/pci:/sys/bus/pci:ro \
  -v /usr/local/Ascend/driver/lib64:/usr/local/Ascend/driver/lib64:ro \
  -v /usr/local/Ascend/driver/version.info:/usr/local/Ascend/driver/version.info:ro \
  -v /etc/ascend_install.info:/etc/ascend_install.info:ro \
  -v /usr/local/dcmi:/usr/local/dcmi:ro \
  -v /usr/local/bin/npu-smi:/usr/local/bin/npu-smi:ro \
  --health-cmd='/usr/local/bin/catmonitor-stress-exec describe --benchmark npu_burn --json --require-pass' \
  --health-interval=10s --health-timeout=35s --health-retries=12 \
  --health-start-period=20s \
  "$CATMONITOR_NPU_STRESS_IMAGE"

docker run -d --name catmonitor --restart unless-stopped \
  --privileged --network host --pid host \
  -v /:/host:ro \
  -v /etc/os-release:/etc/os-release:ro \
  -v /usr/local/Ascend/driver:/usr/local/Ascend/driver:ro \
  -v /usr/local/Ascend/nnae:/usr/local/Ascend/nnae:ro \
  -v /usr/local/Ascend/ascend-toolkit:/usr/local/Ascend/ascend-toolkit:ro \
  -v /usr/bin/hccn_tool:/usr/bin/hccn_tool:ro \
  -v /usr/local/sbin/npu-smi:/usr/local/sbin/npu-smi:ro \
  -e LD_LIBRARY_PATH=/usr/local/Ascend/driver/lib64/driver:/usr/local/Ascend/driver/lib64/common:/usr/local/Ascend/ascend-toolkit/latest/aarch64-linux/lib64:/usr/local/Ascend/nnae/latest/lib64 \
  -v "$CATMONITOR_GENERATED_DIR/catmonitor-stress.yaml:/etc/catmonitor/catmonitor.yaml:ro" \
  -v /var/run/docker.sock:/var/run/docker.sock \
  -v "$CATMONITOR_STRESS_STATE_DIR:/var/lib/catmonitor/stress" \
  -v cm-snapshot:/var/lib/catmonitor/snapshot \
  -v cm-data:/var/lib/catmonitor/data \
  -v cm-straggler:/var/lib/catmonitor/straggler \
  -v cm-control:/run/catmonitor \
  "$CATMONITOR_IMAGE"

docker run -d --name catmonitor-web --restart unless-stopped --network host \
  --entrypoint /usr/local/bin/web \
  -v cm-snapshot:/var/lib/catmonitor/snapshot:ro \
  -v cm-control:/run/catmonitor:ro \
  "$CATMONITOR_IMAGE" \
  -addr=:19322 -snapshot-dir=/var/lib/catmonitor/snapshot \
  -control-socket=/run/catmonitor/control.sock

docker run -d --name catmonitor-dfee --restart unless-stopped --network host \
  --entrypoint /usr/local/bin/dfee \
  -v cm-snapshot:/var/lib/catmonitor/snapshot:ro \
  -v cm-csv:/var/lib/catmonitor/csv \
  "$CATMONITOR_IMAGE" \
  -addr=:19323 -snapshot-dir=/var/lib/catmonitor/snapshot \
  -exporter=enabled -exporter-port=9333 \
  -csv=disabled -csv-dir=/var/lib/catmonitor/csv -csv-interval=10s
```

NPU workload 的根文件系统只读；`HOME=/opt/catmonitor/npuburn-home` 由 tmpfs 提供
运行期可写目录，结果另外持久化到 `$CATMONITOR_NPU_OUTPUT_DIR`。A2/CANN 8.3/runc
profile 只给 NPU workload `privileged=true`，其网络为 `none`；Web/DFeE 不获得该权限。

### 6.5 Doctor / Run / Cancel

```bash
docker exec catmonitor catmonitor stress doctor \
  -c /etc/catmonitor/catmonitor.yaml -o table
docker exec catmonitor catmonitor stress run --bench npu_burn -o table
docker exec catmonitor catmonitor stress status -o table
docker exec catmonitor catmonitor stress cancel --job '<job-id>'
docker top catmonitor-stress-npu | grep -E 'ascend_npu_burn|multiprocessing' && exit 1 || true
```

### 6.6 显式单设备兼容候选（硬件验收 PENDING）

仅适用于从本次源码构建、应用 `portable-runtime-v1.patch` 的新 workload 镜像，
不能直接用本页旧 pre-release tag。构建及本地输入见 [受限环境指南](RESTRICTED_ENV_GUIDE.md)。
旧完整部署仍使用 `full` 默认模式；这里不通过删除数量检查或加 privileged 绕过预检。

先确认设备空闲且获准使用。`NPU_HOST_NODE` 是实际宿主节点路径，**不是 runtime index**；
单设备模式要求 runtime count=1、选择 logical 0，并要求 CANN 的 `aclrtDeviceGetPCIBusId(0)`
与设备字符号的 kernel sysfs PCI 祖先一致，再取该 PCI 的真实 NUMA。接口缺失、不支持或
返回虚拟 PCI 而无法与 sysfs 一致时拒绝并报告 UNVERIFIED。不能凭 CANN 版本号推断可用，
全机 PCI 的第一个设备不能冒充 logical 0。

```bash
# 在宿主机执行；先填写新镜像及授权节点，不使用示例编号代替设备发现。
test -c "$NPU_HOST_NODE"
NPU_HOST_ID="${NPU_HOST_NODE##*davinci}"
bash scripts/stress/generate_stress_deployment.sh \
  --output-dir "$CATMONITOR_GENERATED_DIR" \
  --control-image "$CATMONITOR_IMAGE" \
  --npu-image "$CATMONITOR_NPU_STRESS_IMAGE" \
  --npu-output-dir "$CATMONITOR_NPU_OUTPUT_DIR" \
  --npu-device-mode single --npu-device-nodes "$NPU_HOST_ID" \
  --npu-burn-device 0 --npu-chip-generation "$NPU_CHIP_GENERATION" \
  --npu-runtime runc --npu-threads 1 \
  --npu-ascend-env-script "$CANN_ENV_SCRIPT" \
  --enable-web
```

线程 1 是初次单设备功能验证预算，不是性能默认建议。五项 BLAS/OpenMP 变量、CANN 脚本、
count/mode/visible IDs/选择均写入生成 Compose 和 `npu-workload.env`，spawn 子进程继承。
Compose 沿用 6.3，最后叠加生成文件（privileged=false）。手工方式将 6.4 的 **workload**
命令替换为以下命令，其余三个 Control/Web/DFeE 命令不变；先按 6.4 创建相同 named volumes：

```bash
docker run -d --name catmonitor-stress-npu --restart unless-stopped \
  --runtime runc --init --read-only --network none \
  --cap-drop ALL --security-opt no-new-privileges:true \
  --device="$NPU_HOST_NODE:$NPU_HOST_NODE" \
  --device=/dev/davinci_manager:/dev/davinci_manager \
  --device=/dev/devmm_svm:/dev/devmm_svm \
  --device=/dev/hisi_hdc:/dev/hisi_hdc \
  --tmpfs /tmp:rw,nosuid,nodev,size=256m \
  --tmpfs /opt/catmonitor/npuburn-home:rw,nosuid,nodev,size=1g,mode=0750 \
  --env-file "$CATMONITOR_GENERATED_DIR/npu-workload.env" \
  -v cm-stress-npu-state:/var/lib/catmonitor/stress \
  -v "$CATMONITOR_NPU_OUTPUT_DIR:/opt/catmonitor/npuburn-home/.ascend_npu_burn/output" \
  -v /sys/bus/pci:/sys/bus/pci:ro \
  -v /usr/local/Ascend/driver/lib64:/usr/local/Ascend/driver/lib64:ro \
  -v /usr/local/Ascend/driver/version.info:/usr/local/Ascend/driver/version.info:ro \
  -v /etc/ascend_install.info:/etc/ascend_install.info:ro \
  -v /usr/local/dcmi:/usr/local/dcmi:ro \
  -v /usr/local/bin/npu-smi:/usr/local/bin/npu-smi:ro \
  --health-cmd='/usr/local/bin/catmonitor-stress-exec describe --benchmark npu_burn --json --require-pass' \
  --health-interval=10s --health-timeout=35s --health-retries=3 \
  "$CATMONITOR_NPU_STRESS_IMAGE"
```

Compose/手工均只映射指定计算设备和必要管理节点，不能增加业务设备。若驱动权限不足或
runtime 映射不可证，不切换 privileged；记录失败待硬件适配。公开合成测试不证明该命令已在 A3 通过。
通过 doctor 后才按 6.5 从 Controller 运行。portable profile 的 `--timeout` 是 stream
同步超时；总作业预算由 Controller 请求传入，Worker 就绪另有不超过 60 秒的等待门禁。
超时为失败并回收 Worker；外部 Controller 超时/Cancel 继续由 V2 process group 回收。
Docker daemon 必须实际提供 `--init` 所需的 docker-init；缺失时先修复 Docker 安装，
不要为通过启动而删掉 init 或放大权限。
CSV 位于 output 下本轮独立 `run-*` 目录；必须验证每个选定设备 result/err_count/run_count。
完整新镜像验收顺序见受限环境指南第 5 节。

### 6.7 本次兼容候选：按作业选卡、选算子和小规模 HPL

上述 pre-release 镜像不包含本分支的新接口。新 Control、NPU workload（以及使用新 HPL
容量检查时的 CPU workload）需从同一修复源码构建；不能只更新 YAML/网页后复用旧执行器。
构建 profile、生成配置和手工部署差异见
[受限环境指南第 4 节](RESTRICTED_ENV_GUIDE.md#4-同一套正式部署)。

Web `/stress/` 提供逻辑设备复选框；列表来自实际 profile，授权由管理员配置决定。
CLI 可使用 `stress run --bench npu_burn --npu-devices 0,1 --npu-cases matmul`。
Web 同时提供实际 catalog 的算子选择和不可用原因。设备/算子选择只作用于该 Job，
不修改共享 YAML 或容器默认值；未传参数保留部署默认，空选择报错，不自动全选。
可见不代表空闲；当前整节点空闲门禁在发现任何 NPU 业务进程或无法解析进程表时拒绝启动。
这不是跨调度器的独占预约，仍须在获准维护窗口运行。

完整源码/配置保留、A3 按名合并配置、A2 独立配置和逐算子支持边界见
[NPU 算子指南](NPU_OPERATOR_GUIDE.md)。配置准入不等于实机验证；新镜像的硬件矩阵为 NOT_RUN。

## 7. CPU + NPU Full

### 7.1 Generate

```bash
sudo bash scripts/stress/generate_stress_deployment.sh \
  --output-dir "$CATMONITOR_GENERATED_DIR" \
  --control-image "$CATMONITOR_IMAGE" \
  --cpu-image "$CATMONITOR_CPU_STRESS_IMAGE" \
  --stream-threads 0 \
  --hpl-processes 8 \
  --hpl-threads 12 \
  --hpcg-processes 96 \
  --hpcg-threads 1 \
  --npu-image "$CATMONITOR_NPU_STRESS_IMAGE" \
  --npu-burn-device all \
  --npu-chip-generation A2 \
  --npu-runtime runc \
  --enable-web \
  --force
```

### 7.2 Compose

先执行第 4 节 Full generator，然后：

```bash
CATMONITOR_IMAGE="$CATMONITOR_IMAGE" \
CATMONITOR_STRESS_STATE_DIR="$CATMONITOR_STRESS_STATE_DIR" \
  docker compose -p catmonitor \
  -f docker/docker-compose.yml \
  -f docker/docker-compose.npu.yml \
  -f docker/docker-compose.stress.yml \
  -f "$CATMONITOR_GENERATED_DIR/docker-compose.stress.generated.yml" \
  --profile stress-cpu --profile stress-npu up -d
```

### 7.3 Manual `docker run`

先执行第 4 节 Full generator和 profile 读取块。下面是完整的 5 容器命令，不需要从
其他章节拼接隐藏参数。

```bash
docker volume create cm-snapshot
docker volume create cm-data
docker volume create cm-straggler
docker volume create cm-control
docker volume create cm-csv
docker volume create cm-stress-cpu-state
docker volume create cm-stress-npu-state

docker run -d --name catmonitor-stress-cpu --restart unless-stopped \
  --read-only --network none \
  --cap-drop ALL \
  --cap-add CHOWN --cap-add DAC_OVERRIDE --cap-add FOWNER \
  --cap-add SETGID --cap-add SETPCAP --cap-add SETUID --cap-add SYS_NICE \
  --security-opt no-new-privileges:true \
  --pids-limit 4096 --shm-size=16g \
  --tmpfs /tmp:rw,noexec,nosuid,nodev,size=64m \
  -e STREAM_THREADS=0 \
  -e HPL_MPI_PROCESSES=8 -e HPL_THREADS_PER_PROCESS=12 \
  -e HPCG_MPI_PROCESSES=96 -e HPCG_THREADS_PER_PROCESS=1 \
  -e HPCG_NX=32 -e HPCG_NY=32 -e HPCG_NZ=32 \
  -e HPCG_RUNTIME_SECONDS=60 \
  -v cm-stress-cpu-state:/var/lib/catmonitor/stress \
  --health-cmd='/usr/bin/setpriv --bounding-set=-all --inh-caps=-all --ambient-caps=-all --reuid=65532 --regid=65532 --init-groups --no-new-privs /usr/local/bin/catmonitor-stress-exec describe --benchmark stream --json' \
  --health-interval=5s --health-timeout=3s --health-retries=12 \
  --health-start-period=5s \
  "$CATMONITOR_CPU_STRESS_IMAGE"

docker run -d --name catmonitor-stress-npu --restart unless-stopped \
  --runtime runc --privileged --read-only --network none \
  "${CATMONITOR_NPU_DEVICE_ARGS[@]}" \
  --device=/dev/davinci_manager:/dev/davinci_manager \
  --device=/dev/devmm_svm:/dev/devmm_svm \
  --device=/dev/hisi_hdc:/dev/hisi_hdc \
  --tmpfs /tmp:rw,nosuid,nodev,size=256m \
  --tmpfs /opt/catmonitor/npuburn-home:rw,nosuid,nodev,size=1g,mode=0750 \
  --env-file "$CATMONITOR_GENERATED_DIR/npu-workload.env" \
  -v cm-stress-npu-state:/var/lib/catmonitor/stress \
  -v "$CATMONITOR_NPU_OUTPUT_DIR:/opt/catmonitor/npuburn-home/.ascend_npu_burn/output" \
  -v /sys/bus/pci:/sys/bus/pci:ro \
  -v /usr/local/Ascend/driver/lib64:/usr/local/Ascend/driver/lib64:ro \
  -v /usr/local/Ascend/driver/version.info:/usr/local/Ascend/driver/version.info:ro \
  -v /etc/ascend_install.info:/etc/ascend_install.info:ro \
  -v /usr/local/dcmi:/usr/local/dcmi:ro \
  -v /usr/local/bin/npu-smi:/usr/local/bin/npu-smi:ro \
  --health-cmd='/usr/local/bin/catmonitor-stress-exec describe --benchmark npu_burn --json --require-pass' \
  --health-interval=10s --health-timeout=35s --health-retries=12 \
  --health-start-period=20s \
  "$CATMONITOR_NPU_STRESS_IMAGE"

docker run -d --name catmonitor --restart unless-stopped \
  --privileged --network host --pid host \
  -v /:/host:ro \
  -v /etc/os-release:/etc/os-release:ro \
  -v /usr/local/Ascend/driver:/usr/local/Ascend/driver:ro \
  -v /usr/local/Ascend/nnae:/usr/local/Ascend/nnae:ro \
  -v /usr/local/Ascend/ascend-toolkit:/usr/local/Ascend/ascend-toolkit:ro \
  -v /usr/bin/hccn_tool:/usr/bin/hccn_tool:ro \
  -v /usr/local/sbin/npu-smi:/usr/local/sbin/npu-smi:ro \
  -e LD_LIBRARY_PATH=/usr/local/Ascend/driver/lib64/driver:/usr/local/Ascend/driver/lib64/common:/usr/local/Ascend/ascend-toolkit/latest/aarch64-linux/lib64:/usr/local/Ascend/nnae/latest/lib64 \
  -v "$CATMONITOR_GENERATED_DIR/catmonitor-stress.yaml:/etc/catmonitor/catmonitor.yaml:ro" \
  -v /var/run/docker.sock:/var/run/docker.sock \
  -v "$CATMONITOR_STRESS_STATE_DIR:/var/lib/catmonitor/stress" \
  -v cm-snapshot:/var/lib/catmonitor/snapshot \
  -v cm-data:/var/lib/catmonitor/data \
  -v cm-straggler:/var/lib/catmonitor/straggler \
  -v cm-control:/run/catmonitor \
  "$CATMONITOR_IMAGE"

docker run -d --name catmonitor-web --restart unless-stopped --network host \
  --entrypoint /usr/local/bin/web \
  -v cm-snapshot:/var/lib/catmonitor/snapshot:ro \
  -v cm-control:/run/catmonitor:ro \
  "$CATMONITOR_IMAGE" \
  -addr=:19322 -snapshot-dir=/var/lib/catmonitor/snapshot \
  -control-socket=/run/catmonitor/control.sock

docker run -d --name catmonitor-dfee --restart unless-stopped --network host \
  --entrypoint /usr/local/bin/dfee \
  -v cm-snapshot:/var/lib/catmonitor/snapshot:ro \
  -v cm-csv:/var/lib/catmonitor/csv \
  "$CATMONITOR_IMAGE" \
  -addr=:19323 -snapshot-dir=/var/lib/catmonitor/snapshot \
  -exporter=enabled -exporter-port=9333 \
  -csv=disabled -csv-dir=/var/lib/catmonitor/csv -csv-interval=10s
```

Full 必须恰好 5 个容器。daemon 因启用 Stress 获得 Docker Socket；Web 与 DFeE
仍没有 Docker Socket。

## 8. Web

```text
http://<node-address>:19322/
```

这是唯一 Web listener，同时提供 Monitoring、Stress Run、Cancel、Status 与 History。
Web/DFeE 不挂 Docker Socket。当前 operator API 尚无认证/RBAC，只能向可信管理网络
开放，或由带认证的反向代理保护。

## 9. 停止与清理

手工入口停止但保留容器：

```bash
docker stop catmonitor catmonitor-web catmonitor-dfee \
  catmonitor-stress-cpu catmonitor-stress-npu 2>/dev/null || true
```

重新启动只选择当前 profile 实际存在的容器。删除容器不会删除 named volumes；不要在
需要保留 snapshot/history 时删除这些 volumes。

切换到新的 Stress 镜像集合：

```text
OLD_STRESS_YAML_COMPATIBLE=false
```

必须重新运行 generator，不能复制旧 `script_path`、固定 NPU 容器、CPU Runner socket
或独立 Stress Web 配置。Monitoring-only YAML 可以没有 `stress:` 段。

## 10. 故障排查

### Compose 配置问题与驱动问题分开排查

先用薄入口的 `config` 检查相同的文件组合，再用 `ps/logs` 定位服务；不要维护
另一套手写的 `docker-compose.stress.v1.yml`。旧 Compose 的配置格式差异由入口
转换，NPU runtime 仍来自 generator 的 `--npu-runtime`，不能为了绕过报错手改 overlay。

`npu-smi` 的 `dcmi module initialize failed` 是容器运行后的驱动访问错误，不是
Compose YAML 解析错误。先比较宿主和 workload 容器的 `npu-smi info`，以及
容器 runtime、Devices、mounts、安全设置和业务占用；不能只凭 root 用户、数字
UID/GID 或切换 runtime 推断确切根因。设备被业务使用时不执行 Burn、不杀业务。

Docker 18.09 的 inspect 结构可能没有 `DeviceRequests`。检查本项目实际使用的
`--device` 映射应读 `Devices`，无需因此重建容器：

```bash
docker inspect catmonitor-stress-npu \
  --format 'runtime={{.HostConfig.Runtime}} privileged={{.HostConfig.Privileged}} readonly={{.HostConfig.ReadonlyRootfs}}'
docker inspect catmonitor-stress-npu --format '{{json .HostConfig.Devices}}'
docker inspect catmonitor-stress-npu --format '{{json .Mounts}}'
```

如 generator 指定了自定义容器名，使用该名称，不要假设一定为默认名。

### 有 NPU 业务时的容器与管理查询

创建/启动容器、CPU 绑核、访问 Ascend 驱动、允许执行 Burn 是四个不同的门禁。
NPU 被业务使用不意味着 Docker 不能创建容器或 CPU 不能绑核；但普通容器映射
同一物理设备时，驱动的设备命名空间隔离可能让新容器内 `npu-smi` 返回
`device is used / -8020`。不要靠增加 CPU/PID 额度或强制更换 runtime 绕过。

124 的 A2/910B4、Docker 18.09、驱动 25.5 对照中：双卡 vLLM TP=2 实际推理期间，
新普通容器的创建和有效 CPU 集合绑定通过，但管理查询失败；vLLM 退出后，同一
配置恢复正常。在此期间既有特权 Control 的管理查询仍可用。这不是所有 A2/A3
环境的认证，也不能据此给其他节点的 `-8005` 确认根因。

后续经授权的临时对照中，正式普通 NPU 容器保持运行，独立双卡 vLLM TP=2
完成 18 次真实推理：新特权探针的创建、CPU 绑核及 `npu-smi` 全部通过，普通
探针仍返回 `-8020`。特权探针还能查询到与宿主一致的业务 PID；独立 PID
命名空间下进程名可能为空，不得据此判为空闲。全部诊断容器随后删除，正式
容器的 ID、镜像、启动时间和权限均未改变。

- Monitoring 与业务可继续并存，管理查询使用既有、经过批准的 Control 边界；
  Web/DFeE 不因此获得 Docker Socket 或设备权限。
- `restricted` workload 维持隔离边界。驱动访问不确定时部署预检仍失败；业务占用
  本身不阻止部署就绪，但 Run 必须拒绝计算任务，不显示成“服务器 CPU 绑核失败”。
- 如果管理员确实要求新 workload 容器在同卡业务存在时直接执行 `npu-smi`，
  当前 generator 的显式 `--npu-device-mode full --npu-security-profile legacy-privileged`
  提供共享访问模式，已在上述 A2 临时对照验证；正式启用仍需管理员安全审查，
  不是自动降级策略。特权扩大宿主设备访问权限，不能把显式设备映射或 Controller
  逻辑白名单视为内核级单卡隔离。仅挂管理节点、盲目放开 capabilities 或忽略
  Busy 都不是等价修复。
- 新版镜像、generator 的 `npu-workload.env` 与 Compose 默认
  `CATMONITOR_NPU_REQUIRE_IDLE=0`：容器启动、healthcheck、普通 describe/doctor 只验证
  部署依赖与拓扑，不因业务占用拒绝就绪。预检通过不代表设备空闲或获得压测许可。
- CLI/Web 提交算子或模型 Job 时，Controller 强制设置
  `CATMONITOR_NPU_REQUIRE_IDLE=1`，不使用缓存的部署 PASS；真正执行前 workload
  再查一次空闲状态。直接调用新版 `catmonitor-stress-exec run` 也不能用 `0` 绕过。
  任意业务 PID（即使进程名为空）、未知/不完整进程表或管理查询失败均拒绝下发/启动，
  不杀业务、不自动排队重试。当前策略是保守的节点级空闲检查，不声称只检查选中卡，
  也不是对外部调度器的独占预约。
- 管理员可在部署阶段共存启动容器，但不能据此在业务卡上并发 Burn。单卡子集模式
  不自动提升为 privileged。该分层修复须更新 Control 和 NPU workload 镜像；只改旧
  容器环境变量不等于已有完整的运行前门禁，不能当作升级方案。

仅检查部署就绪（不运行负载）：

```bash
docker exec -e CATMONITOR_NPU_REQUIRE_IDLE=0 catmonitor-stress-npu \
  /usr/local/bin/catmonitor-stress-exec describe --benchmark npu_burn --json --require-pass
```

显式检查此刻是否可运行（不运行负载，失败返回非零）：

```bash
docker exec -e CATMONITOR_NPU_REQUIRE_IDLE=1 catmonitor-stress-npu \
  /usr/local/bin/catmonitor-stress-exec describe --benchmark npu_burn --json --require-pass
```

两者仍检查 CANN/torch_npu、Custom Ops、设备映射、PCI 拓扑及可写运行目录；`0`
只是不执行占用检查，不能修复 `-8020` 驱动访问问题。正式作业继续由 Controller 下发。

参考：[Ascend 官方容器占用说明](https://github.com/Ascend/RecSDK/blob/develop/docker/OVERVIEW.zh.md#非特权容器npu资源占用冲突)。

### 常见提示

- NPU Monitoring 为空：检查 `npu-smi info`、driver/toolkit 挂载和 daemon 日志。
- NPU Burn unavailable：执行 doctor，核对 CANN、torch_npu、lspci 与 device count。
- 稀疏设备映射错误：不要手写连续 `0..N-1` host node，让 generator 动态发现。
- `No available device`：核对容器内 `lspci -D -d 19e5:`、
  `ASCEND_RT_VISIBLE_DEVICES`、`torch_npu.npu.device_count()` 与 logical topology。
- `aclInit 507899` / `Resource_Busy` / `Device id is invalid` 不是同一种根因：先区分
  驱动命名空间占用、逻辑编号映射与软件栈配套问题，不把所有错误都用 privileged
  掩盖。只有管理员明确选择共享访问兼容模式时，才为 NPU workload 配置
  `legacy-privileged`；Web/DFeE 不获得该权限。
- HPL 立即失败：核对 `HPL_MPI_PROCESSES == P×Q`，不要盲用默认值 1。
- Web snapshot 未就绪：先检查 `19320/-/ready`，不要另起第二个 Web。
- Docker API 版本不匹配：当前 Stress Controller 会通过配置的 Unix socket 自动协商，
  用户不应手工设置 `DOCKER_API_VERSION`。
- Registry 不可达：离线传输同一标签和 Image ID 的镜像，不得替换成旧 a2-r1 镜像。
