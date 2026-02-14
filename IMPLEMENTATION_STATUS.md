# spark 实施状态导出

## 1. 当前目标
将 `ollama launch` 能力剥离为独立模块 `spark/`，支持：
- 通用 OpenAI Chat 接口配置
- TUI/交互配置
- 配置文件落盘
- 保留并接入全部既有 integration（claude/codex/droid/opencode/openclaw/pi）

## 2. 已完成内容

### 2.1 独立模块骨架
已创建目录与入口：
- `spark/go.mod`
- `spark/cmd/spark/main.go`
- `spark/internal/app/cli.go`
- `spark/internal/tui/prompt.go`
- `spark/internal/config/*`
- `spark/internal/integrations/*`

### 2.2 配置系统（独立于 ollama）
已实现：
- 配置路径：`~/.code-sparker/config.json`
- 多 Profile + 默认 Profile
- Integration 绑定 profile + models + aliases
- 历史模型输入记录（手动输入+历史）
- 原子写入 + 备份机制（临时文件 + rename）
- 备份目录：`/tmp/spark-backups`
- 首次加载时尝试从 `~/.ollama/config.json` 迁移 integration models（只读迁移）

核心文件：
- `spark/internal/config/config.go`
- `spark/internal/config/files.go`
- `spark/internal/config/migrate.go`

### 2.3 CLI 与交互流程
已实现命令：
- `spark`（交互主菜单）
- `spark launch [integration] [-- ...]`
- `spark config [integration]`
- `spark profile`

交互能力：
- 选择 integration
- 选择/输入 profile
- 输入模型（CSV，含历史默认）
- 配置模式 `--config` 后可选择是否立即启动
- 对 Editor 类 integration 显示将被修改的文件路径与备份目录

核心文件：
- `spark/internal/app/cli.go`
- `spark/internal/tui/prompt.go`

### 2.4 全部 integration 已接线
已实现并注册：
- `claude` / `codex` / `droid` / `opencode` / `openclaw` / `pi`
- 别名：`clawdbot`、`moltbot` -> `openclaw`

核心文件：
- `spark/internal/integrations/registry.go`
- `spark/internal/integrations/types.go`
- `spark/internal/integrations/util.go`
- `spark/internal/integrations/claude.go`
- `spark/internal/integrations/codex.go`
- `spark/internal/integrations/droid.go`
- `spark/internal/integrations/opencode.go`
- `spark/internal/integrations/openclaw.go`
- `spark/internal/integrations/pi.go`

### 2.5 Codex 兼容适配（responses/chat-completions）
已实现 Codex 启动前的网关能力探测与自动兼容：
- 优先探测 `POST /v1/responses`
- 若不支持 `responses` 但支持 `POST /v1/chat/completions`，自动启用本地兼容代理
- 兼容代理将 `responses` 请求转为 `chat/completions` 请求，并回写最小可用 `responses` 格式（含流式事件）

核心文件：
- `spark/internal/integrations/codex.go`
- `spark/internal/integrations/codex_compat_proxy.go`
- `spark/internal/integrations/codex_test.go`
- `spark/internal/integrations/codex_compat_proxy_test.go`

## 3. 当前未完成项

### 3.1 已完成：编译与测试验证
已完成验证：
- `go mod tidy`
- `go test ./...`

当前结果：
- 依赖已下载并生成 `go.sum`
- `internal/app`、`internal/config` 单测通过
- 其余包当前无测试文件

### 3.2 代码质量增强项（剩余）
以下属于后续可迭代优化：
- integration 层单测（`internal/integrations/*`）
- Bubble Tea 风格 TUI（当前为轻量交互式 prompt）
- 更严格 schema 校验与配置版本升级路径
- claude/codex 更细粒度参数兼容性校验
- compat proxy 分层重构（handler/translator/executor/writer）
  - 迁移执行清单：`spark/docs/compat-proxy-architecture.md`

## 4. 已知风险
1. integration 层目前仍缺少自动化测试，后续重构有回归风险。
2. 部分 integration 的配置写入是“可用优先”的简化实现，尚未做到与上游 `ollama` 等价的字段兼容深度。
3. Codex 兼容代理当前是“最小可用”映射，复杂工具调用/高级事件字段仍需持续补齐。

## 5. 下一步建议（你本机可直接执行）
建议优先做：

```bash
cd /home/fxh/code/codeAgentLauncher/spark
GOCACHE=/tmp/go-build GOMODCACHE=/tmp/go-mod go test ./...
GOCACHE=/tmp/go-build GOMODCACHE=/tmp/go-mod go run ./cmd/spark
```

如要继续补质量：

```bash
cd /home/fxh/code/codeAgentLauncher/spark
go test ./internal/config -run TestSaveLoadRoundTrip -v
go test ./internal/app -run TestProfileNamesSorted -v
```

## 6. 文件清单（当前新增）
- `spark/go.mod`
- `spark/go.sum`
- `spark/cmd/spark/main.go`
- `spark/internal/app/cli.go`
- `spark/internal/app/cli_test.go`
- `spark/internal/tui/prompt.go`
- `spark/internal/config/config.go`
- `spark/internal/config/config_test.go`
- `spark/internal/config/files.go`
- `spark/internal/config/migrate.go`
- `spark/internal/integrations/types.go`
- `spark/internal/integrations/registry.go`
- `spark/internal/integrations/util.go`
- `spark/internal/integrations/claude.go`
- `spark/internal/integrations/codex.go`
- `spark/internal/integrations/codex_test.go`
- `spark/internal/integrations/codex_compat_proxy.go`
- `spark/internal/integrations/codex_compat_proxy_test.go`
- `spark/internal/integrations/droid.go`
- `spark/internal/integrations/opencode.go`
- `spark/internal/integrations/openclaw.go`
- `spark/internal/integrations/pi.go`

---

如需继续，建议下一步：
1. 为 `internal/integrations/*` 增加最小冒烟测试（至少覆盖 `registry` 和每个 integration 的参数映射）。
2. 增加配置 schema/版本升级测试，覆盖旧配置兼容路径。
