# Spark 实施状态（当前）

> 本文反映仓库**当前**状态。历史从 ollama launcher 拆分的旧说明已过时，以本文件与 `README.md` 为准。

## 1. 定位

Spark 是 AI coding agent 的终端启动器与配置中心：

- 多 profile（模型、Base URL、API Key、接口类型）
- 启动并写入 `codex` / `claude` / `opencode` / `grok` 配置
- 本地兼容代理（Codex Responses、Claude Messages → 上游 Chat 等）
- MCP 与 Skills 管理
- Token usage 记录

## 2. 配置

- 配置目录：`~/.spark`
- 主配置：`~/.spark/config.json`
- Skills 注册表：`~/.spark/skill-registry.json`
- Skills 内容：`~/.spark/skills/`

旧 `~/.ollama/config.json` 仍可在首次加载时只读迁移 integration models（见 `internal/config/migrate.go`）。

## 3. 正式集成（registry）

| 名称 | 说明 |
|------|------|
| `codex` | 主要支持 + Responses 兼容代理 |
| `claude` | 主要支持 + Anthropic Messages 兼容代理 |
| `opencode` | OpenAI-compatible 配置写入并启动 |
| `grok` | Grok Build；隐藏别名 `grok-build` |

**未注册、源码仍保留（非入口）**：`droid`、`pi`、`openclaw`（见 `internal/integrations/`，`registry_names_test.go` 约束不会出现在 `Names()`）。

## 4. 兼容代理

- 实现位置：`internal/compat/`（`client` → `ir` → `target`，`gateway` 编排，`proxy` HTTP 入口）
- 运行时本地代理：
  - Codex：`POST /v1/responses`
  - Claude：`/v1/messages`（及 `/messages`）
- Gemini `generateContent`：client codec / profile 接口类型与探测，**不是**当前本地 HTTP 代理路由

## 5. Skills

已可用：

- CLI：`spark skill` 及 list/show/search/install/enable/disable/remove/sync/import/upgrade
- TUI：主面板 **Manage skills**
- 投影：`spark skill sync` → `.agents/skills`、`.codex/skills`、`.claude/skills`

注意：安装后需 **sync** 才会出现在 agent 目录；catalog 搜索依赖网络 / `npx skills find`。

## 6. 测试

```bash
go test ./...
```

重点包：`internal/config`、`internal/integrations`、`internal/tui`、`internal/compat/gateway`、`internal/compat/policy`、`internal/skills`。

## 7. 工程债（已知）

- TUI 大文件已按 profile 模式拆分 MCP（model/input/view/actions/helpers）；Skills 管理器仍可继续拆
- `droid` / `pi` / `openclaw` 源码可删除或打 experimental tag
- 兼容代理复杂 tool-call / 供应商差异需持续契约测试
