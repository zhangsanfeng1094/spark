# Spark 实施状态（当前）

> 本文反映仓库**当前**状态。以本文件与 `README.md` 为准。

## 1. 定位

Spark 是 AI coding agent 的终端启动器与配置中心：

- 多 profile（模型、Base URL、API Key、接口类型）
- 启动并写入 `agy` / `codex` / `claude` / `opencode` / `grok` / `one` 配置
- 本地兼容代理与共享守护进程（Codex Responses、Claude Messages、Gemini generateContent → Bifrost Core → 上游 LLM）
- MCP 与 Skills 管理
- Token usage 记录与统计

## 2. 配置

- 配置目录：`~/.spark`
- 主配置：`~/.spark/config.json`
- Skills 注册表：`~/.spark/skill-registry.json`
- Skills 内容：`~/.spark/skills/`
- Usage 数据库：`~/.spark/token_usage.db`

## 3. 正式集成（registry）

| 名称 | 说明 |
|------|------|
| `codex` | 主要支持 + Responses 兼容代理/Daemon |
| `claude` | 主要支持 + Anthropic Messages 兼容代理/Daemon |
| `agy` | Antigravity；Gemini generateContent 直连，其它 Profile 走兼容代理/Daemon |
| `opencode` | OpenAI-compatible 配置写入并启动 |
| `grok` | Grok Build；隐藏别名 `grok-build` |
| `one` | One；按 Profile 接口类型直连 |

**未注册、源码仍保留（非入口）**：`droid`、`pi`、`openclaw`（见 `internal/integrations/`，`registry_names_test.go` 约束不会出现在 `Names()`）。

## 4. 兼容代理与 Bifrost 引擎

- **引擎层**：`internal/compat/engine`（基于 `github.com/maximhq/bifrost/core`）
- **Ingress 协议适配**：
  - OpenAI Ingress：`internal/compat/ingress/openai`（`POST /v1/chat/completions`、`POST /chat/completions`、`GET /v1/models`、`GET /models` 及 OpenAI SSE 流式输出）
  - Codex Ingress：`internal/compat/ingress/codex`（`POST /v1/responses`、`POST /responses` 及 Responses SSE 流式输出）
  - Claude Ingress：`internal/compat/ingress/claude`（`POST /v1/messages`、`POST /messages` 及 Anthropic SSE 流式输出）
  - Gemini Ingress：`internal/compat/ingress/gemini`（`POST /v1beta/models/{model}:generateContent` / `streamGenerateContent` 及 Gemini SSE 流式输出）
- **运行模式**：
  - **Shared Daemon**（推荐/默认）：`spark daemon`（默认监听 `127.0.0.1:42337`），支持跨 session 复用连接池与动态 profile 路由
  - **In-process Proxy**（降级模式）：`SPARK_SHARED_PROXY=0` 时按进程监听随机端口

## 5. 认证与登录态托管 (CLIProxyAPI / Auth)

- **存储目录**：`~/.spark/auth/<provider>.json`（权限 `0600`）
- **支持 Provider**：Claude Code、Codex、Command Code、Gemini / Google AI、Grok / xAI
- **登录方式**：
  - **Browser OAuth + PKCE**：本地回调服务器、生成 state / code_challenge 并换取 token
  - **Device Authorization Flow**（RFC 8628）：设备码轮询，适用于无浏览器/远程环境
  - **自动刷新**：`EnsureFresh` 在凭证临近过期时自动刷新 access_token
- **集成与注入**：
  - Ingress Handler 请求级注入：通过 `engine.ResolveRequestCredential` 优先匹配 profile / provider 登录态
  - Agent 启动注入：在启动 `claude` / `codex` / `grok` 时动态注入登录态 token，安全隔离环境
  - Daemon 管理 API：`/v0/management/accounts`、`/v0/management/providers`、`/v0/management/{provider}-auth-url`、`/v0/management/logout`
- **操作入口**：
  - CLI：`spark login`、`spark logout`、`spark auth status`
  - TUI：主面板 **Manage logins**

## 6. Skills

已可用：

- CLI：`spark skill` 及 list/show/search/install/enable/disable/remove/sync/import/upgrade
- TUI：主面板 **Manage skills**
- 投影：`spark skill sync` → `.agents/skills`、`.codex/skills`、`.claude/skills`

## 7. 测试

```bash
go test ./...
```

重点包：`internal/auth`、`internal/auth/oauth`、`internal/config`、`internal/integrations`、`internal/tui`、`internal/compat/engine`、`internal/compat/ingress/claude`、`internal/compat/ingress/codex`、`internal/compat/ingress/gemini`、`internal/compat/daemon`、`internal/compat/proxy`、`internal/skills`、`internal/usage`。
