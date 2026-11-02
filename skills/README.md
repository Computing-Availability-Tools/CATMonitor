# CATMonitor Agent Skills

面向 AI Agent（Claude Code 等 Agent Skills 生态）的技能包，把 CATMonitor 的监控能力封装成 Agent 可直接调用的技能。

## 技能清单

| 技能 | 用途 | 风险级别 |
|------|------|----------|
| [`catmonitor-health/`](./catmonitor-health/SKILL.md) | 服务器健康体检：0-100 健康分、等级、逐部件扣分明细、巡检报告 | 只读 |
| [`catmonitor-metrics/`](./catmonitor-metrics/SKILL.md) | 运行指标查询：CPU/内存/磁盘/GPU/NPU/网络/机箱 共 216 项指标 | 只读 |
| [`catmonitor-faults/`](./catmonitor-faults/SKILL.md) | NPU 故障事件查询与订阅（掉卡/ECC/RoCE 链路等） | 只读 + 订阅注册 |

三个技能均**只读**，不修改任何系统状态；不打包二进制，依赖目标机器上已安装的 `catmonitor`（Go 版 v0.3.6+）。

## 安装

```bash
# 用户级安装（~/.claude/skills/，对所有项目生效）
make skills-install

# 或指定目录
make skills-install DEST=~/.claude/skills

# 项目级安装（./.claude/skills/，仅当前项目生效）
bash scripts/install_skills.sh --project

# 只看会安装什么，不实际执行
bash scripts/install_skills.sh --list
```

安装后在 Claude Code 中直接用自然语言触发，例如：

- "帮我体检一下这台服务器" → `catmonitor-health`
- "查一下 NPU 的温度和利用率" → `catmonitor-metrics`
- "这台机器有没有掉卡记录？最近有什么 NPU 故障？" → `catmonitor-faults`

## 通过 npx 安装（免 clone 仓库）

npm 包 `catmonitor-skills` 提供一行命令安装（需要 Node.js ≥14）：

```bash
npx catmonitor-skills              # 用户级 ~/.claude/skills（Claude Code / OpenCode 等通用）
npx catmonitor-skills --project    # 项目级 ./.claude/skills
npx catmonitor-skills --list       # 只预览，不安装
npx catmonitor-skills@latest       # npx 有缓存，更新时显式指定 @latest
```

> 国内网络提示：npmjs.com 直连较慢时，可先 `npm config set registry https://registry.npmmirror.com`（新发布包同步到镜像有分钟级延迟）。
>
> 维护者发布流程：修改 `skills/` 后 bump 根目录 `package.json` 的 `version` → `npm publish`。

## 前置条件

- 目标机器已安装 CATMonitor（Go 版）：`catmonitor version` 输出形如 `CATMonitor v0.3.6 (Go 1.23+)`
  - 注意区分：老版 C 实现 `catmonitor_cli`（`-q/-m/-c` 参数风格）**不是**这些技能的目标
- `catmonitor-faults` 需要 daemon 开启 `faultsub.enabled: true`（默认关闭）
- 部分 NPU 指标需要 `-tags dcmi` 构建且目标机为昇腾环境；无硬件时相关指标优雅降级为空，不报错

## 目录结构约定

```
skills/
├── README.md                ← 本文件
└── <skill-name>/
    ├── SKILL.md             ← 技能入口：frontmatter + 触发条件 + 命令契约 + 输出解析
    ├── references/          ← 大表与细节文档（按需加载，SKILL.md 只放索引）
    └── evals/evals.json     ← 触发与行为评测用例（skill-creator 兼容格式）
```

## 编写规范

新增或修改技能时遵循以下规则（`scripts/lint_skills.py` 在 pre-commit 中强制校验部分规则）：

1. **frontmatter**：必须包含 `name`（与目录名一致的 kebab-case）和 `description`（中文功能描述 + 英文触发词，20–1024 字符）。可选自定义字段：`compatibility`（运行环境要求）、`metadata`（作者/版本等）。
2. **触发条件**：description 必须写清"什么场景该用本技能"，正文设"触发条件"一节列举用户话术示例。
3. **正文 ≤200 行**：指标清单、规则表、API 明细等大块内容放 `references/`，正文只留索引和常用项。
4. **前置检查先行**：正文第一步必须是 `command -v catmonitor` + `catmonitor version`，防止误调老版 C 二进制。
5. **不打包二进制**：技能只包含 Markdown 与（必要时）纯文本脚本；运行时依赖已安装的 `catmonitor`。
6. **输出解析写坑位**：JSONL 流 vs JSON 数组、默认输出格式、退出码等易错点必须显式写明。
7. **写操作需守门**：任何产生副作用的技能（未来的 stress 等）必须在 SKILL.md 中声明安全边界与确认流程。
8. **命令与代码对齐**：SKILL.md 中的命令、字段、端口必须与源码/官方文档一致；不确定时以 `docs/User_Manual.md` 与 `features/*/` 规格文档为准。

## Roadmap（暂未实现）

- [ ] `catmonitor-stress`：可靠性压测技能（受控写操作，需强制 `stress doctor` 预检 + 运行前向用户确认）
- [ ] MCP server：`catmonitor mcp` 子命令，将能力暴露为标准 MCP tools（覆盖 Claude 之外的 MCP 客户端）
- [ ] 故障注入评测闭环：故障注入 → 健康度/故障事件检测 → 对照打分，评测检测规则覆盖率
- [ ] 定时巡检模式：CronCreate + 巡检报告归档
