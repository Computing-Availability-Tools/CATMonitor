# CATMonitor 容器部署

本页只做导航。部署命令按硬件保留在对应 README；构建、代理和发布统一放在开发者指南，
不再另维护一套演示或迁移安装流程。

## 从哪里开始

| 目标 | 阅读入口 |
| --- | --- |
| 当前 A2 配套镜像、digest、按作业参数和完整部署 | [当前候选交付指南](STRESS_JOB_RELEASE_GUIDE.md) |
| Generic Monitoring / CPU Stress | [Generic 节点指南](README-generic.md) |
| NVIDIA Monitoring / CPU Stress | [GPU 节点指南](README-gpu.md) |
| Ascend Monitoring / CPU / NPU / Full、受限单设备 | [NPU 节点指南](README-npu.md) |
| Stress CLI/Web、参数、报告与故障排查 | [Stress 用户指南](../features/stress/STRESS_USER_GUIDE.md) |
| GPT3/Llama2、参考生成、SDC 与显存策略 | [模型 SDC 指南](NPU_MODEL_SDC_GUIDE.md) |
| 源码构建、镜像源/代理、离线输入和发布 | [开发者指南](DEVELOPER_GUIDE.md) |
| 自动化与各硬件验收边界 | [Stress 测试指南](../features/stress/STRESS_TEST_GUIDE.md) |

当前按作业参数候选以交付指南中的源码提交、镜像 ID/digest 为准。硬件 README 中
`arm64-v0.3.5-stress` 是旧 pre-release 示例，不能据此验证新参数功能；GPU 旧候选
可能需要 GHCR 登录。不要按程序版本或同名 tag 推断镜像内容相同。

## 镜像、容器与权限

| 场景 | 所需镜像 | 容器数 |
| --- | --- | ---: |
| Monitoring-only | 对应硬件的一张 Control | 3：daemon、Web、DFeE |
| CPU Stress | Control + CPU workload | 4 |
| NPU Stress | Ascend Control + NPU workload | 4 |
| Ascend Full | Ascend Control + CPU + NPU workload | 5 |

Control 同时包含 daemon/CLI、Web 和 DFeE；无需再找“专用 daemon 镜像”。
CPU-only 不需下载 NPU 镜像。Web 与 DFeE 不挂 Docker socket；启用 Stress 后仅
daemon 通过 Docker Executor 调用 workload。运行链路为：

```text
CLI / Web :19322 → /run/catmonitor/control.sock → daemon Controller
                 → Docker exec → workload executor → typed plugin
```

Monitoring-only 保留原配置和容器路径，不需要 control.sock、Docker socket 或 workload。
旧 Monitoring YAML 兼容；旧 Stress V1 YAML 不兼容，升级须重新生成配置。
当前 Stress Run/Cancel 尚无认证/RBAC，只对可信网络开放。

当前源码的 generator 在显式部署 Stress 时默认启用 Web Run/Cancel；`--disable-web`
可关闭 Web 操作而保留 CLI。NPU 默认开放映射范围内全部 logical ID、GPT3/Llama2
以及单卡参考生成，默认仍只选 logical 0 和 matmul；不自动启动任务或生成参考。
管理员可收紧设备/模型列表、关闭参考生成；空闲与参考匹配检查不变。旧部署文件不会
自动改变，须重新生成并让本项目 Controller 读取。详见
[默认能力与管理员限制](STRESS_JOB_RELEASE_GUIDE.md#当前源码的默认能力与管理员限制)。

```text
OLD_MONITORING_YAML_COMPATIBLE=true
OLD_STRESS_YAML_COMPATIBLE=false
```

## Compose 与手工启动

推荐 Compose；各硬件 README 同时保留完整 `docker run`、自定义配置、部分服务、
独立 DFeE 及 Prometheus/Grafana 入口。不需要安装 Compose 才能构建镜像或手工部署。

`docker/compose.sh` 只组合已有 Compose 和 generator 输出，不是安装器，不构建镜像、
不运行压测、不停止其他项目。先按节点指南设置 `CATMONITOR_IMAGE`；Stress 先执行
generator，再选择 `--mode cpu|npu|full` 和对应生成目录。例如已生成 Full 配置后：

```bash
bash docker/compose.sh --hardware npu --mode full \
  --generated /etc/catmonitor/generated-stress config
bash docker/compose.sh --hardware npu --mode full \
  --generated /etc/catmonitor/generated-stress up
bash docker/compose.sh --hardware npu --mode full \
  --generated /etc/catmonitor/generated-stress --public-host '<node-address>' urls
```

所有操作沿用同一 hardware/mode/generated/project。新 project 名不解决固定 workload
名称或 host-network 端口冲突。入口优先 Compose v2；旧 `docker-compose` 需要 Python3 /
PyYAML，会临时转换 profiles 和 `bind.create_host_path` 语法，检查源路径并保持实际
runtime/devices/权限不变；不支持的其他配置明确失败。`down` 默认不删除卷。

### v2、旧版 Compose 与 NPU 权限

默认 `--backend auto` 优先使用 Compose v2；可以显式选择 `--backend v2` 或
`--backend legacy`。旧 Docker Engine 不等于必须安装/使用哪一版 Compose；先检查
实际 CLI。旧版路径仍使用同一 canonical 配置，不维护第二套六容器实现。

NPU full 默认沿用 privileged/runc；`--npu-security-profile restricted` 会显式覆盖
privileged。不能因另一节点用了 `ascend` 就全局改 runtime：确认目标 Docker 已安装
该 runtime 后，使用 generator 的 `--npu-runtime ascend` 重新生成。DCMI 初始化失败
与设备忙碌是两层问题，`npu-smi info` 失败不能靠关闭空闲检查解决。

Stress 模式的 `up` 使用 Python3 在创建容器前检查合并后的镜像/容器名是否匹配 Controller，NPU 权限是否匹配
生成 profile、是否残留 restricted 的 cap_drop/security_opt，以及只读根、network none、
设备节点/数量和 runtime 是否一致/存在。其他项目占用固定 workload 名称时拒绝启动，
不会删除或重命名旧容器。`restart` 不读取新的容器配置；改权限/环境/挂载后用
`up --no-build --force-recreate`，不要通过手改 generated YAML 叠加权限。
Compose v2 的 Monitoring-only config/up 不因此新增 Python3 依赖；旧版兼容路径及
`urls` 原本就需要 Python3（legacy 另需 PyYAML）。

### 旁路部署：独立名称、目录、卷与宿主端口

正式默认仍是 host network、Web `:19322`。需要与旧实例同时运行时，先在 generator
中使用独立 `--cpu-container`、`--npu-container`、`--output-dir` 和 `--npu-output-dir`；
同一新 `--project` 隔离 Compose 自动容器名和 named volumes，不能替代 workload 名称
及 bind 目录隔离。不要复用旧项目的显式 volume name、external volume 或 state 目录。

旁路的少量差异可以放在仓库外 override，通过新增的 `--overlay FILE` 追加（可重复）；
无需再定义临时 `r1()`。下面是宿主端口池示例，保存为自己的 `sidecar.yml`：

```yaml
services:
  catmonitor:
    network_mode: bridge
    ports:
      - "127.0.0.1:19327-19360:19320"
  web:
    network_mode: bridge
    ports:
      - "${CATMONITOR_SIDECAR_BIND:-127.0.0.1}:19327-19360:19322"
  dfee:
    network_mode: bridge
    ports:
      - "${CATMONITOR_SIDECAR_BIND:-127.0.0.1}:19327-19360:19323"
      - "127.0.0.1:19327-19360:9333"
```

示例默认只供本机访问；允许可信管理网络直连时，将 `CATMONITOR_SIDECAR_BIND` 设置为
节点管理 IP，并检查防火墙。容器内 Web 应保持 wildcard 监听，不能绑定容器内回环。
Docker 从池中选择空闲端口，不保证连续或重建后不变；不要猜 Web 的宿主端口。
桥接改变 network collector 的网络命名空间视角，不能当作原 host-network 采集完全等价。
若基础配置已有 ports，Compose 会合并；先看 `config`，不要误保留旧端口或使用旧版
Compose 不支持的 YAML `!override` 标签。

```bash
# 先按交付指南获取配套的 CONTROL_IMAGE、CPU_IMAGE、NPU_IMAGE。
: "${CONTROL_IMAGE:?set the paired control image}"
: "${CPU_IMAGE:?set the paired CPU image}"
: "${NPU_IMAGE:?set the paired NPU image}"
export CATMONITOR_IMAGE="$CONTROL_IMAGE"
export SIDECAR_ROOT=/srv/catmonitor/sidecar
export GENERATED="$SIDECAR_ROOT/generated"
export SIDECAR_OVERLAY="$SIDECAR_ROOT/sidecar.yml"
export CATMONITOR_STRESS_STATE_DIR="$SIDECAR_ROOT/controller-state"
install -d -m 0750 "$SIDECAR_ROOT" "$CATMONITOR_STRESS_STATE_DIR" "$SIDECAR_ROOT/npu-output"
# 将上面的 YAML 保存到 SIDECAR_OVERLAY，再执行；已有生成文件时先备份，勿盲目 --force。
test -f "$SIDECAR_OVERLAY"
bash scripts/stress/generate_stress_deployment.sh \
  --output-dir "$GENERATED" --control-image "$CONTROL_IMAGE" \
  --cpu-image "$CPU_IMAGE" --cpu-container catmonitor-sidecar-cpu \
  --npu-image "$NPU_IMAGE" --npu-container catmonitor-sidecar-npu \
  --npu-output-dir "$SIDECAR_ROOT/npu-output" --npu-chip-generation A2
bash docker/compose.sh --hardware npu --mode full --project catmonitor-sidecar \
  --generated "$GENERATED" --overlay "$SIDECAR_OVERLAY" config
bash docker/compose.sh --hardware npu --mode full --project catmonitor-sidecar \
  --generated "$GENERATED" --overlay "$SIDECAR_OVERLAY" up --no-build
bash docker/compose.sh --hardware npu --mode full --project catmonitor-sidecar \
  --generated "$GENERATED" --overlay "$SIDECAR_OVERLAY" --public-host '<node-address>' urls
```

`urls` 在 host 模式读本次启动日志，在 bridge 模式同时读取 Docker 实际发布端口；
端口未发布或 Web 只绑定容器回环时明确报错，不把容器 `19322` 当成节点地址。
后续 ps/logs/exec/stop/start/down 使用完全相同的选项。以上只启动服务容器，不发起 Burn；
真实压测仍需确认设备空闲并主动提交 Job。
以上生成命令明确是 A2；A3 必须改为匹配的镜像及 `--npu-chip-generation A3`，
不能按设备数量推断代际，也不代表 A3 硬件已通过验收。

### Web 与端口

Web 默认 `:19322`、DFeE 页面默认 `:19323`；仅在端口占用时依次递增。
`compose.sh ... urls` 从本次启动日志读取实际端口，不把默认值当成已监听结果。
Metrics `19320`、faultsub `19321`、DFeE Exporter `9333` **不属于**该回退机制。

Compose 使用 host network，服务实际监听端口就是节点端口；`--public-host` 只打印
地址，不创建 NAT/SSH 转发，也不开放防火墙。网络可达时直接访问节点 IP，无需隧道。
受限端口范围可显式 `export CATMONITOR_WEB_ADDR=:5001`，但自动递增仍可能超出防火墙
范围；固定外部地址须预留端口或使用反向代理。

## 部署后核验

```bash
docker ps --format 'table {{.Names}}\t{{.Image}}\t{{.Status}}'
curl -fsS http://127.0.0.1:19320/-/ready
curl -fsS http://127.0.0.1:19322/ >/dev/null
curl -fsS http://127.0.0.1:19323/ >/dev/null
# 仅在启用 Stress 后执行；Compose 项目可用同一入口 exec catmonitor。
docker exec catmonitor catmonitor stress doctor -c /etc/catmonitor/catmonitor.yaml -o table
```

文档/Compose fixture 通过不等于每种硬件均已实测。A2 当前候选的有限实测结果见
交付指南；Generic/GPU 及 A3 验收必须分别记录，不能由 A2 外推。历史文档和实验记录
保留在 Git 历史，不作为当前安装入口。
