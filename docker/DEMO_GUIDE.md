# CATMonitor 三阶段交互演示指南

本文用于已经取得源码和预构建镜像的 Linux/ARM64 节点，按顺序演示：

1. Generic Monitoring：Web 展示与宿主机终端调用 CLI；
2. Generic + CPU Stress：CLI 运行 STREAM，用户在 Web 发起并取消 HPL；
3. Ascend Full：Monitoring + CPU Stress + NPU Burn，用户在 Web 发起 NPU Burn。

本文只使用明确的 `docker run`，不要求 Docker Compose，也不构建镜像。所有命令都在
**目标 Linux 宿主机的 Bash** 中执行；`docker exec catmonitor catmonitor ...` 表示从
宿主机调用 Control 容器内的 CLI，它与 Web 连接同一个 daemon Controller。

> 三个阶段使用正式容器名，因此必须串行执行。开始前若发现同名正式容器或端口已被
> 占用，应停止演示并先确认维护窗口，不能直接删除未知容器。

当前演示镜像是 Linux/ARM64 Stress pre-release，不是最终 v0.3.6：

```text
ghcr.io/spike677/catmonitor-generic:arm64-v0.3.5-stress
ghcr.io/spike677/catmonitor-npu:arm64-v0.3.5-stress
ghcr.io/spike677/catmonitor-stress-cpu:arm64-v0.3.5-stress
ghcr.io/spike677/catmonitor-stress-npu:arm64-v0.3.5-stress
```

## 0. 公共预检

```bash
export CATMONITOR_SOURCE=/opt/catmonitor/CATMonitor
export CATMONITOR_REGISTRY=ghcr.io/spike677
export CATMONITOR_RELEASE=arm64-v0.3.5-stress
export CATMONITOR_GENERIC_IMAGE="$CATMONITOR_REGISTRY/catmonitor-generic:$CATMONITOR_RELEASE"
export CATMONITOR_NPU_IMAGE="$CATMONITOR_REGISTRY/catmonitor-npu:$CATMONITOR_RELEASE"
export CATMONITOR_CPU_STRESS_IMAGE="$CATMONITOR_REGISTRY/catmonitor-stress-cpu:$CATMONITOR_RELEASE"
export CATMONITOR_NPU_STRESS_IMAGE="$CATMONITOR_REGISTRY/catmonitor-stress-npu:$CATMONITOR_RELEASE"

cd "$CATMONITOR_SOURCE"

printf '\n=== source ===\n'
git branch --show-current
git rev-parse HEAD
git status --short

printf '\n=== host ===\n'
uname -m
docker version
docker info --format 'root={{.DockerRootDir}} driver={{.Driver}}'
df -h / /home 2>/dev/null || true

printf '\n=== name and port safety gate ===\n'
CATMONITOR_DEMO_CONFLICT=0
for name in catmonitor catmonitor-web catmonitor-dfee \
  catmonitor-stress-cpu catmonitor-stress-npu; do
  if docker container inspect "$name" >/dev/null 2>&1; then
    echo "STOP: container name is already in use: $name" >&2
    CATMONITOR_DEMO_CONFLICT=1
  fi
done

for port in 19320 19322 19323 9333; do
  if ss -lnt | awk 'NR > 1 {print $4}' | grep -Eq "(^|:)$port$"; then
    echo "STOP: TCP port is already in use: $port" >&2
    CATMONITOR_DEMO_CONFLICT=1
  fi
done

if [ "$(uname -m)" != aarch64 ]; then
  echo "STOP: this pre-release demo requires aarch64" >&2
  CATMONITOR_DEMO_CONFLICT=1
fi

if [ "$CATMONITOR_DEMO_CONFLICT" -eq 0 ]; then
  echo 'COMMON_PREFLIGHT=PASS'
else
  echo 'COMMON_PREFLIGHT=STOP_EXISTING_ENVIRONMENT'
fi
```

如该步骤输出 `STOP`，不要继续，也不要执行本文清理命令；先盘点现有容器归属。
该检查不会调用 `exit`，因此粘贴到交互式 SSH Shell 后不会主动断开连接。

## 1. Generic Monitoring

这一阶段只需一张 Generic Control 镜像，运行三个容器；不挂 Docker Socket，不创建
control socket，也不启动 CPU/NPU workload。

### 1.1 拉取并核对镜像

```bash
printf '\n=== Demo 1/3: Generic Monitoring image ===\n'
docker pull "$CATMONITOR_GENERIC_IMAGE"
docker image inspect "$CATMONITOR_GENERIC_IMAGE" \
  --format 'image={{index .RepoTags 0}} id={{.Id}} platform={{.Os}}/{{.Architecture}} size={{.Size}}'
test "$(docker image inspect "$CATMONITOR_GENERIC_IMAGE" --format '{{.Os}}/{{.Architecture}}')" = linux/arm64
```

### 1.2 创建隔离演示卷并启动三个容器

```bash
export DEMO_SNAPSHOT_VOLUME=catmonitor-demo1-snapshot
export DEMO_DATA_VOLUME=catmonitor-demo1-data
export DEMO_STRAGGLER_VOLUME=catmonitor-demo1-straggler
export DEMO_CSV_VOLUME=catmonitor-demo1-csv

docker volume create "$DEMO_SNAPSHOT_VOLUME"
docker volume create "$DEMO_DATA_VOLUME"
docker volume create "$DEMO_STRAGGLER_VOLUME"
docker volume create "$DEMO_CSV_VOLUME"

docker run -d --name catmonitor --restart unless-stopped \
  --privileged --network host --pid host \
  -v /:/host:ro \
  -v /etc/os-release:/etc/os-release:ro \
  -v "$DEMO_SNAPSHOT_VOLUME:/var/lib/catmonitor/snapshot" \
  -v "$DEMO_DATA_VOLUME:/var/lib/catmonitor/data" \
  -v "$DEMO_STRAGGLER_VOLUME:/var/lib/catmonitor/straggler" \
  "$CATMONITOR_GENERIC_IMAGE"

docker run -d --name catmonitor-web --restart unless-stopped --network host \
  --entrypoint /usr/local/bin/web \
  -v "$DEMO_SNAPSHOT_VOLUME:/var/lib/catmonitor/snapshot:ro" \
  "$CATMONITOR_GENERIC_IMAGE" \
  -addr=:19322 \
  -snapshot-dir=/var/lib/catmonitor/snapshot \
  -config=/etc/catmonitor/catmonitor.yaml

docker run -d --name catmonitor-dfee --restart unless-stopped --network host \
  --entrypoint /usr/local/bin/dfee \
  -v "$DEMO_SNAPSHOT_VOLUME:/var/lib/catmonitor/snapshot:ro" \
  -v "$DEMO_CSV_VOLUME:/var/lib/catmonitor/csv" \
  "$CATMONITOR_GENERIC_IMAGE" \
  -addr=:19323 \
  -snapshot-dir=/var/lib/catmonitor/snapshot \
  -exporter=enabled -exporter-port=9333 \
  -csv=disabled -csv-dir=/var/lib/catmonitor/csv -csv-interval=10s

docker ps --filter name='^/catmonitor' \
  --format 'table {{.Names}}\t{{.Image}}\t{{.Status}}'
```

### 1.3 验证 Monitoring 与宿主机 CLI

```bash
for attempt in $(seq 1 30); do
  curl -fsS http://127.0.0.1:19320/-/ready >/dev/null && break
  sleep 2
done

curl -fsS http://127.0.0.1:19320/-/ready
curl -fsS http://127.0.0.1:19320/metrics | head -n 20
docker exec catmonitor test -s /var/lib/catmonitor/snapshot/snapshot.json
curl -fsS http://127.0.0.1:19322/ >/dev/null
curl -fsS http://127.0.0.1:19323/ >/dev/null
curl -fsS http://127.0.0.1:9333/metrics | head -n 20

printf '\n=== host shell -> CATMonitor CLI ===\n'
docker exec catmonitor catmonitor list
docker exec catmonitor catmonitor collect -o table
docker exec catmonitor catmonitor health -o table

printf '\n=== Monitoring-only Stress capability ===\n'
curl -fsS http://127.0.0.1:19322/api/stress/config

printf '\n=== daemon mounts: Docker socket must be absent ===\n'
docker inspect catmonitor \
  --format '{{range .Mounts}}{{println .Source "->" .Destination}}{{end}}'
```

预期 `/api/stress/config` 返回禁用能力视图，daemon mounts 中没有
`/var/run/docker.sock`。

这里的 `stress.web_enabled=false` 只关闭 Web 上的 Stress Run/Cancel，不控制
Monitoring Web 服务；首页与 snapshot API 仍应返回 HTTP 200。

### 1.4 用户 Web 检查点

现在由用户打开：

```text
http://<node-address>:19322/
```

检查健康概览、CPU/Memory/Disk 页面；再进入 `/stress/`，确认页面明确显示 Stress
未启用，而不是页面错误。本阶段 Web 不应能够发起压测。

若只能通过 Windows SSH 访问，在 Windows PowerShell 执行：

```powershell
ssh -N -o ExitOnForwardFailure=yes -o ServerAliveInterval=30 `
  -L 127.0.0.1:19322:127.0.0.1:19322 `
  -L 127.0.0.1:19323:127.0.0.1:19323 `
  root@<node-address> -p <ssh-port>
```

然后打开 `http://127.0.0.1:19322/`。用户确认页面后再执行下一节。

### 1.5 清理本阶段

```bash
docker rm -f catmonitor-web catmonitor-dfee catmonitor
docker volume rm \
  "$DEMO_SNAPSHOT_VOLUME" "$DEMO_DATA_VOLUME" \
  "$DEMO_STRAGGLER_VOLUME" "$DEMO_CSV_VOLUME"
echo 'GENERIC_MONITORING_DEMO=PASS'
```

## 2. Generic + CPU Stress

这一阶段使用 Generic Control 与 CPU workload 两张镜像、四个容器。CPU workload
包含 STREAM/HPL/HPCG；Web 与 CLI 通过同一个 daemon Controller 操作同一作业。

CPU 参数按节点当前在线 CPU 数生成。镜像内 `HPL.dat` 的 `P×Q=4×2`，因此 HPL
固定使用 8 ranks；每 rank 线程数取 `online CPUs / 8`，HPCG 使用每在线 CPU 一个
rank。在线 CPU 数不能被 8 整除时，管理员应按实际拓扑显式调整，不能直接照抄示例。

### 2.1 主机和镜像预检

```bash
printf '\n=== Demo 2/3: Generic + CPU Stress ===\n'
export CATMONITOR_ONLINE_CPUS="$(nproc)"
export CATMONITOR_HPL_PROCESSES=8
export CATMONITOR_HPL_THREADS="$((CATMONITOR_ONLINE_CPUS / CATMONITOR_HPL_PROCESSES))"
export CATMONITOR_HPCG_PROCESSES="$CATMONITOR_ONLINE_CPUS"
export CATMONITOR_HPCG_THREADS=1

printf 'ONLINE_CPUS=%s\n' "$CATMONITOR_ONLINE_CPUS"
printf 'HPL_PROFILE=%sx%s\n' "$CATMONITOR_HPL_PROCESSES" "$CATMONITOR_HPL_THREADS"
printf 'HPCG_PROFILE=%sx%s\n' "$CATMONITOR_HPCG_PROCESSES" "$CATMONITOR_HPCG_THREADS"
lscpu | sed -n '1,25p'
test "$CATMONITOR_ONLINE_CPUS" -ge "$CATMONITOR_HPL_PROCESSES"
test "$((CATMONITOR_ONLINE_CPUS % CATMONITOR_HPL_PROCESSES))" -eq 0

docker pull "$CATMONITOR_GENERIC_IMAGE"
docker pull "$CATMONITOR_CPU_STRESS_IMAGE"
docker image inspect "$CATMONITOR_GENERIC_IMAGE" "$CATMONITOR_CPU_STRESS_IMAGE" \
  --format 'image={{index .RepoTags 0}} id={{.Id}} platform={{.Os}}/{{.Architecture}} size={{.Size}}'
```

### 2.2 生成 CPU 节点配置

```bash
export CATMONITOR_GENERATED_DIR=/etc/catmonitor/demo-v036-cpu
export CATMONITOR_STRESS_STATE_DIR=/var/lib/catmonitor/demo-v036-cpu/stress

sudo install -d -m 0750 \
  "$CATMONITOR_GENERATED_DIR" \
  "$CATMONITOR_STRESS_STATE_DIR"

sudo bash scripts/stress/generate_stress_deployment.sh \
  --output-dir "$CATMONITOR_GENERATED_DIR" \
  --control-image "$CATMONITOR_GENERIC_IMAGE" \
  --cpu-image "$CATMONITOR_CPU_STRESS_IMAGE" \
  --stream-threads 0 \
  --hpl-processes "$CATMONITOR_HPL_PROCESSES" \
  --hpl-threads "$CATMONITOR_HPL_THREADS" \
  --hpcg-processes "$CATMONITOR_HPCG_PROCESSES" \
  --hpcg-threads "$CATMONITOR_HPCG_THREADS" \
  --hpcg-runtime 60 \
  --enable-web \
  --force

python3 -m json.tool "$CATMONITOR_GENERATED_DIR/stress-profile.json"
sed -n '1,220p' "$CATMONITOR_GENERATED_DIR/catmonitor-stress.yaml"
```

generator 只写配置，不启动容器。Web 不提供任意 MPI/线程/脚本编辑。

### 2.3 启动 CPU workload 与 Control 三容器

```bash
export DEMO_SNAPSHOT_VOLUME=catmonitor-demo2-snapshot
export DEMO_DATA_VOLUME=catmonitor-demo2-data
export DEMO_STRAGGLER_VOLUME=catmonitor-demo2-straggler
export DEMO_CONTROL_VOLUME=catmonitor-demo2-control
export DEMO_CSV_VOLUME=catmonitor-demo2-csv
export DEMO_CPU_STATE_VOLUME=catmonitor-demo2-cpu-state

for volume in "$DEMO_SNAPSHOT_VOLUME" "$DEMO_DATA_VOLUME" \
  "$DEMO_STRAGGLER_VOLUME" "$DEMO_CONTROL_VOLUME" \
  "$DEMO_CSV_VOLUME" "$DEMO_CPU_STATE_VOLUME"; do
  docker volume create "$volume"
done

docker run -d --name catmonitor-stress-cpu --restart unless-stopped \
  --read-only --network none \
  --cap-drop ALL \
  --cap-add CHOWN --cap-add DAC_OVERRIDE --cap-add FOWNER \
  --cap-add SETGID --cap-add SETPCAP --cap-add SETUID --cap-add SYS_NICE \
  --security-opt no-new-privileges:true \
  --pids-limit 4096 --shm-size=16g \
  --tmpfs /tmp:rw,noexec,nosuid,nodev,size=64m \
  -e STREAM_THREADS=0 \
  -e HPL_MPI_PROCESSES="$CATMONITOR_HPL_PROCESSES" \
  -e HPL_THREADS_PER_PROCESS="$CATMONITOR_HPL_THREADS" \
  -e HPCG_MPI_PROCESSES="$CATMONITOR_HPCG_PROCESSES" \
  -e HPCG_THREADS_PER_PROCESS="$CATMONITOR_HPCG_THREADS" \
  -e HPCG_NX=32 -e HPCG_NY=32 -e HPCG_NZ=32 \
  -e HPCG_RUNTIME_SECONDS=60 \
  -v "$DEMO_CPU_STATE_VOLUME:/var/lib/catmonitor/stress" \
  --health-cmd='/usr/bin/setpriv --bounding-set=-all --inh-caps=-all --ambient-caps=-all --reuid=65532 --regid=65532 --init-groups --no-new-privs /usr/local/bin/catmonitor-stress-exec describe --benchmark stream --json' \
  --health-interval=5s --health-timeout=3s --health-retries=12 \
  --health-start-period=5s \
  "$CATMONITOR_CPU_STRESS_IMAGE"

docker run -d --name catmonitor --restart unless-stopped \
  --privileged --network host --pid host \
  -v /:/host:ro \
  -v /etc/os-release:/etc/os-release:ro \
  -v "$CATMONITOR_GENERATED_DIR/catmonitor-stress.yaml:/etc/catmonitor/catmonitor.yaml:ro" \
  -v /var/run/docker.sock:/var/run/docker.sock \
  -v "$CATMONITOR_STRESS_STATE_DIR:/var/lib/catmonitor/stress" \
  -v "$DEMO_SNAPSHOT_VOLUME:/var/lib/catmonitor/snapshot" \
  -v "$DEMO_DATA_VOLUME:/var/lib/catmonitor/data" \
  -v "$DEMO_STRAGGLER_VOLUME:/var/lib/catmonitor/straggler" \
  -v "$DEMO_CONTROL_VOLUME:/run/catmonitor" \
  "$CATMONITOR_GENERIC_IMAGE"

docker run -d --name catmonitor-web --restart unless-stopped --network host \
  --entrypoint /usr/local/bin/web \
  -v "$DEMO_SNAPSHOT_VOLUME:/var/lib/catmonitor/snapshot:ro" \
  -v "$DEMO_CONTROL_VOLUME:/run/catmonitor:ro" \
  "$CATMONITOR_GENERIC_IMAGE" \
  -addr=:19322 -snapshot-dir=/var/lib/catmonitor/snapshot \
  -control-socket=/run/catmonitor/control.sock

docker run -d --name catmonitor-dfee --restart unless-stopped --network host \
  --entrypoint /usr/local/bin/dfee \
  -v "$DEMO_SNAPSHOT_VOLUME:/var/lib/catmonitor/snapshot:ro" \
  -v "$DEMO_CSV_VOLUME:/var/lib/catmonitor/csv" \
  "$CATMONITOR_GENERIC_IMAGE" \
  -addr=:19323 -snapshot-dir=/var/lib/catmonitor/snapshot \
  -exporter=enabled -exporter-port=9333 \
  -csv=disabled -csv-dir=/var/lib/catmonitor/csv -csv-interval=10s

docker ps --filter name='^/catmonitor' \
  --format 'table {{.Names}}\t{{.Image}}\t{{.Status}}'
```

### 2.4 宿主机 CLI：doctor 与 STREAM

以下命令由用户在宿主机执行：

```bash
docker exec catmonitor catmonitor stress doctor \
  -c /etc/catmonitor/catmonitor.yaml -o table

docker exec catmonitor catmonitor stress run \
  --bench stream -c /etc/catmonitor/catmonitor.yaml -o table

docker exec catmonitor catmonitor stress status \
  -c /etc/catmonitor/catmonitor.yaml -o table

curl -fsS http://127.0.0.1:19322/api/stress/latest | python3 -m json.tool
```

预期 doctor 为 STREAM/HPL/HPCG 3/3，STREAM 输出 Copy/Scale/Add/Triad，且 Web latest
与 CLI 为同一次作业。

### 2.5 用户 Web Run/Cancel 检查点

用户打开：

```text
http://<node-address>:19322/stress/
```

按下面顺序操作，不要同时勾选多个长任务：

1. 确认三个 CPU 项目均“预检通过”；
2. 选择 HPL，点击“开始可靠性压测”；
3. 页面出现 Running 后，在宿主机执行：

   ```bash
   docker exec catmonitor catmonitor stress status \
     -c /etc/catmonitor/catmonitor.yaml -o table
   docker top catmonitor-stress-cpu
   ```

4. 回到 Web 点击“取消”；
5. 再在宿主机验证作业和进程已结束：

   ```bash
   docker exec catmonitor catmonitor stress status \
     -c /etc/catmonitor/catmonitor.yaml -o table
   docker top catmonitor-stress-cpu | grep -E 'xhpl|xhpcg|mpirun|hydra' && exit 1 || true
   curl -fsS http://127.0.0.1:19322/api/stress/history | python3 -m json.tool
   ```

Web 上的开始与取消必须由用户完成；不要用 curl 伪装 UI 验收。

### 2.6 清理本阶段

用户确认 CPU Web 结果后执行：

```bash
docker rm -f catmonitor-web catmonitor-dfee catmonitor catmonitor-stress-cpu
docker volume rm \
  "$DEMO_SNAPSHOT_VOLUME" "$DEMO_DATA_VOLUME" \
  "$DEMO_STRAGGLER_VOLUME" "$DEMO_CONTROL_VOLUME" \
  "$DEMO_CSV_VOLUME" "$DEMO_CPU_STATE_VOLUME"
echo 'GENERIC_CPU_STRESS_DEMO=PASS'
```

保留 `$CATMONITOR_GENERATED_DIR` 和 `$CATMONITOR_STRESS_STATE_DIR`，便于审计报告；确认
不再需要后再由管理员归档或删除。

## 3. Ascend Full（CPU + NPU）

这一阶段使用 Ascend Control、CPU workload、NPU workload 三张镜像，运行五个容器。
当前 pre-release 只声明已验收 A2/Ascend910B4、CANN 8.3、`runc`；不能外推到 A3/A5。

### 3.1 Ascend、设备和镜像预检

```bash
printf '\n=== Demo 3/3: Ascend Full CPU + NPU ===\n'
npu-smi info
ls -l /dev/davinci[0-9]*
test -e /dev/davinci_manager
test -e /dev/devmm_svm
test -e /dev/hisi_hdc
test -d /usr/local/Ascend/driver
test -d /usr/local/Ascend/nnae
test -d /usr/local/Ascend/ascend-toolkit
test -x /usr/local/sbin/npu-smi
test -x /usr/local/bin/npu-smi

docker pull "$CATMONITOR_NPU_IMAGE"
docker pull "$CATMONITOR_CPU_STRESS_IMAGE"
docker pull "$CATMONITOR_NPU_STRESS_IMAGE"
docker image inspect "$CATMONITOR_NPU_IMAGE" \
  "$CATMONITOR_CPU_STRESS_IMAGE" "$CATMONITOR_NPU_STRESS_IMAGE" \
  --format 'image={{index .RepoTags 0}} id={{.Id}} platform={{.Os}}/{{.Architecture}} size={{.Size}}'
```

运行 NPU Burn 前必须确认设备无外部业务；发现业务时停止 NPU workload 演示，不得杀死
不属于本次演示的进程。

### 3.2 生成 Full 配置并读取动态设备映射

```bash
export CATMONITOR_ONLINE_CPUS="$(nproc)"
export CATMONITOR_HPL_PROCESSES=8
export CATMONITOR_HPL_THREADS="$((CATMONITOR_ONLINE_CPUS / CATMONITOR_HPL_PROCESSES))"
export CATMONITOR_HPCG_PROCESSES="$CATMONITOR_ONLINE_CPUS"
export CATMONITOR_HPCG_THREADS=1

test "$CATMONITOR_ONLINE_CPUS" -ge "$CATMONITOR_HPL_PROCESSES"
test "$((CATMONITOR_ONLINE_CPUS % CATMONITOR_HPL_PROCESSES))" -eq 0

export CATMONITOR_GENERATED_DIR=/etc/catmonitor/demo-v036-full
export CATMONITOR_STRESS_STATE_DIR=/var/lib/catmonitor/demo-v036-full/stress
export CATMONITOR_NPU_OUTPUT_DIR=/var/lib/catmonitor/demo-v036-full/npu-burn-output

sudo install -d -m 0750 \
  "$CATMONITOR_GENERATED_DIR" \
  "$CATMONITOR_STRESS_STATE_DIR" \
  "$CATMONITOR_NPU_OUTPUT_DIR"

sudo bash scripts/stress/generate_stress_deployment.sh \
  --output-dir "$CATMONITOR_GENERATED_DIR" \
  --control-image "$CATMONITOR_NPU_IMAGE" \
  --cpu-image "$CATMONITOR_CPU_STRESS_IMAGE" \
  --stream-threads 0 \
  --hpl-processes "$CATMONITOR_HPL_PROCESSES" \
  --hpl-threads "$CATMONITOR_HPL_THREADS" \
  --hpcg-processes "$CATMONITOR_HPCG_PROCESSES" \
  --hpcg-threads "$CATMONITOR_HPCG_THREADS" \
  --hpcg-runtime 60 \
  --npu-image "$CATMONITOR_NPU_STRESS_IMAGE" \
  --npu-burn-device all \
  --npu-chip-generation A2 \
  --npu-runtime runc \
  --npu-output-dir "$CATMONITOR_NPU_OUTPUT_DIR" \
  --enable-web \
  --force

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

printf 'HOST_DEVICE_IDS=%s\n' "${CATMONITOR_NPU_PROFILE_FIELDS[*]:3}"
printf 'MAPPED_DEVICE_COUNT=%s\n' "$CATMONITOR_NPU_DEVICE_COUNT"
printf 'RUNTIME_VISIBLE_IDS=%s\n' "$CATMONITOR_NPU_RUNTIME_VISIBLE_DEVICES"
printf 'NPU_BURN_LOGICAL_IDS=%s\n' "$CATMONITOR_NPU_BURN_DEVICE"
python3 -m json.tool "$CATMONITOR_STRESS_PROFILE"
```

宿主机的稀疏 `/dev/davinciN` 编号不等于 NPU Burn logical ID；后续命令只能使用
generator 输出的数组，不能手写 `0..7` 或把最大设备号当成设备数。

### 3.3 启动两个 workload 与三个 Control 容器

```bash
export DEMO_SNAPSHOT_VOLUME=catmonitor-demo3-snapshot
export DEMO_DATA_VOLUME=catmonitor-demo3-data
export DEMO_STRAGGLER_VOLUME=catmonitor-demo3-straggler
export DEMO_CONTROL_VOLUME=catmonitor-demo3-control
export DEMO_CSV_VOLUME=catmonitor-demo3-csv
export DEMO_CPU_STATE_VOLUME=catmonitor-demo3-cpu-state
export DEMO_NPU_STATE_VOLUME=catmonitor-demo3-npu-state

for volume in "$DEMO_SNAPSHOT_VOLUME" "$DEMO_DATA_VOLUME" \
  "$DEMO_STRAGGLER_VOLUME" "$DEMO_CONTROL_VOLUME" "$DEMO_CSV_VOLUME" \
  "$DEMO_CPU_STATE_VOLUME" "$DEMO_NPU_STATE_VOLUME"; do
  docker volume create "$volume"
done

docker run -d --name catmonitor-stress-cpu --restart unless-stopped \
  --read-only --network none \
  --cap-drop ALL \
  --cap-add CHOWN --cap-add DAC_OVERRIDE --cap-add FOWNER \
  --cap-add SETGID --cap-add SETPCAP --cap-add SETUID --cap-add SYS_NICE \
  --security-opt no-new-privileges:true \
  --pids-limit 4096 --shm-size=16g \
  --tmpfs /tmp:rw,noexec,nosuid,nodev,size=64m \
  -e STREAM_THREADS=0 \
  -e HPL_MPI_PROCESSES="$CATMONITOR_HPL_PROCESSES" \
  -e HPL_THREADS_PER_PROCESS="$CATMONITOR_HPL_THREADS" \
  -e HPCG_MPI_PROCESSES="$CATMONITOR_HPCG_PROCESSES" \
  -e HPCG_THREADS_PER_PROCESS="$CATMONITOR_HPCG_THREADS" \
  -e HPCG_NX=32 -e HPCG_NY=32 -e HPCG_NZ=32 \
  -e HPCG_RUNTIME_SECONDS=60 \
  -v "$DEMO_CPU_STATE_VOLUME:/var/lib/catmonitor/stress" \
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
  -e CATMONITOR_NPU_DEVICE_COUNT="$CATMONITOR_NPU_DEVICE_COUNT" \
  -e ASCEND_RT_VISIBLE_DEVICES="$CATMONITOR_NPU_RUNTIME_VISIBLE_DEVICES" \
  -e NPU_BURN_DEVICE="$CATMONITOR_NPU_BURN_DEVICE" \
  -e NPU_BURN_RUN_CASE=matmul -e NPU_BURN_GROUP= \
  -e NPU_BURN_CHIP_GENERATION=A2 \
  -e NPU_BURN_INTERNAL_TIMEOUT_SECONDS=300 \
  -v "$DEMO_NPU_STATE_VOLUME:/var/lib/catmonitor/stress" \
  -v "$CATMONITOR_NPU_OUTPUT_DIR:/opt/catmonitor/npuburn-home/.ascend_npu_burn/output" \
  -v /sys/bus/pci:/sys/bus/pci:ro \
  -v /usr/local/Ascend/driver/lib64:/usr/local/Ascend/driver/lib64:ro \
  -v /usr/local/Ascend/driver/version.info:/usr/local/Ascend/driver/version.info:ro \
  -v /etc/ascend_install.info:/etc/ascend_install.info:ro \
  -v /usr/local/dcmi:/usr/local/dcmi:ro \
  -v /usr/local/bin/npu-smi:/usr/local/bin/npu-smi:ro \
  --health-cmd='/usr/local/bin/catmonitor-stress-exec describe --json' \
  --health-interval=10s --health-timeout=5s --health-retries=12 \
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
  -v "$DEMO_SNAPSHOT_VOLUME:/var/lib/catmonitor/snapshot" \
  -v "$DEMO_DATA_VOLUME:/var/lib/catmonitor/data" \
  -v "$DEMO_STRAGGLER_VOLUME:/var/lib/catmonitor/straggler" \
  -v "$DEMO_CONTROL_VOLUME:/run/catmonitor" \
  "$CATMONITOR_NPU_IMAGE"

docker run -d --name catmonitor-web --restart unless-stopped --network host \
  --entrypoint /usr/local/bin/web \
  -v "$DEMO_SNAPSHOT_VOLUME:/var/lib/catmonitor/snapshot:ro" \
  -v "$DEMO_CONTROL_VOLUME:/run/catmonitor:ro" \
  "$CATMONITOR_NPU_IMAGE" \
  -addr=:19322 -snapshot-dir=/var/lib/catmonitor/snapshot \
  -control-socket=/run/catmonitor/control.sock

docker run -d --name catmonitor-dfee --restart unless-stopped --network host \
  --entrypoint /usr/local/bin/dfee \
  -v "$DEMO_SNAPSHOT_VOLUME:/var/lib/catmonitor/snapshot:ro" \
  -v "$DEMO_CSV_VOLUME:/var/lib/catmonitor/csv" \
  "$CATMONITOR_NPU_IMAGE" \
  -addr=:19323 -snapshot-dir=/var/lib/catmonitor/snapshot \
  -exporter=enabled -exporter-port=9333 \
  -csv=disabled -csv-dir=/var/lib/catmonitor/csv -csv-interval=10s

docker ps --filter name='^/catmonitor' \
  --format 'table {{.Names}}\t{{.Image}}\t{{.Status}}'
test "$(docker ps --filter name='^/catmonitor' --filter status=running -q | wc -l)" -eq 5
```

### 3.4 验证 Monitoring、Full doctor 与宿主机 CLI

以下命令由用户在宿主机执行：

```bash
for attempt in $(seq 1 30); do
  curl -fsS http://127.0.0.1:19320/-/ready >/dev/null && break
  sleep 2
done

curl -fsS http://127.0.0.1:19320/-/ready
curl -fsS http://127.0.0.1:19320/metrics | grep -i npu | head -n 20
docker exec catmonitor test -s /var/lib/catmonitor/snapshot/snapshot_npu.json
curl -fsS http://127.0.0.1:19322/ >/dev/null
curl -fsS http://127.0.0.1:19323/ >/dev/null

docker exec catmonitor catmonitor collect -o table
docker exec catmonitor catmonitor health -o table
docker exec catmonitor catmonitor stress doctor \
  -c /etc/catmonitor/catmonitor.yaml -o table

curl -fsS http://127.0.0.1:19322/api/stress/config | python3 -m json.tool
```

预期 doctor 为 STREAM/HPL/HPCG/NPU Burn 4/4，配置 API 中四项均 available。

先从宿主机 CLI 运行一次短 STREAM：

```bash
docker exec catmonitor catmonitor stress run \
  --bench stream -c /etc/catmonitor/catmonitor.yaml -o table
docker exec catmonitor catmonitor stress status \
  -c /etc/catmonitor/catmonitor.yaml -o table
```

### 3.5 用户 Web 发起 NPU Burn

再次在宿主机确认 NPU 无外部业务：

```bash
npu-smi info
docker top catmonitor-stress-npu
```

然后由用户打开 `http://<node-address>:19322/stress/`：

1. 确认 Full 四项均预检通过；
2. 只选择 NPU Burn；
3. 点击“开始可靠性压测”；
4. 页面显示 Running 后，在宿主机观察同一作业：

   ```bash
   docker exec catmonitor catmonitor stress status \
     -c /etc/catmonitor/catmonitor.yaml -o table
   docker top catmonitor-stress-npu
   npu-smi info
   ```

5. 等待 Web 显示完成；不要同时启动 CPU 作业；
6. 在宿主机核对报告和 CSV：

   ```bash
   docker exec catmonitor catmonitor stress status \
     -c /etc/catmonitor/catmonitor.yaml -o table
   curl -fsS http://127.0.0.1:19322/api/stress/latest | python3 -m json.tool
   curl -fsS http://127.0.0.1:19322/api/stress/history | python3 -m json.tool
   find "$CATMONITOR_NPU_OUTPUT_DIR" -maxdepth 2 -type f -printf '%TY-%Tm-%Td %TH:%TM:%TS %p\n' | sort
   ```

预期 NPU Burn 结果为 PASS/healthy、`err_count=0` 且生成新 CSV。若要演示取消，由用户
再次从 Web 启动作业、确认进程出现后点击“取消”，再执行：

```bash
docker top catmonitor-stress-npu | grep -E 'ascend_npu_burn|multiprocessing' && exit 1 || true
docker exec catmonitor catmonitor stress status \
  -c /etc/catmonitor/catmonitor.yaml -o table
```

### 3.6 演示结束状态

为便于用户继续审视 Web，本阶段默认保持五个容器运行：

```bash
docker ps --filter name='^/catmonitor' \
  --format 'table {{.Names}}\t{{.Image}}\t{{.Status}}'
echo 'ASCEND_FULL_DEMO_RUNNING=PASS'
```

需要停止但保留容器与数据时：

```bash
docker stop catmonitor-web catmonitor-dfee catmonitor \
  catmonitor-stress-cpu catmonitor-stress-npu
```

只有明确不再需要演示环境时，才删除这五个演示容器；不要删除
`$CATMONITOR_STRESS_STATE_DIR`、`$CATMONITOR_NPU_OUTPUT_DIR` 或演示 volumes，除非已完成
报告与 CSV 归档。

## 4. 通过标准

| 阶段 | 通过条件 |
|---|---|
| Generic Monitoring | 3 容器；metrics/snapshot/Web/DFeE/CLI 正常；Stress 禁用；无 Docker Socket |
| Generic + CPU | 4 容器；doctor 3/3；CLI STREAM；Web HPL Run/Cancel；无残留 MPI 进程 |
| Ascend Full | 5 容器；doctor 4/4；CLI STREAM；Web NPU Burn；CSV 且 `err_count=0` |

本演示不把静态 README 检查当成实机结果。Generic/CPU 和 Ascend Full 只有在对应宿主机
执行完上述命令并保存输出后，才能标记该次演示通过。
