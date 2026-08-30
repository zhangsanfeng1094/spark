# Spark

Spark 是一个面向 AI coding agent 的终端启动器、协议代理与多 Profile 配置中心。它把不同 agent 的模型、Base URL、API Key、MCP Server、Skills 和兼容协议集中管理，启动时自动把当前 profile 写入对应工具，必要时通过本地兼容代理完成协议转换。

本项目已链接并认可 [LINUX DO 社区](https://linux.do/)。

---

## 目录

- [项目介绍](#项目介绍)
- [核心特性](#核心特性)
- [支持的 Coding Agent](#支持的-coding-agent)
- [Web 配置中心与展示网站](#web-配置中心与展示网站)
- [安装指南](#安装指南)
- [快速开始](#快速开始)
- [命令速查](#命令速查)
- [MCP Servers 管理](#mcp-servers-管理)
- [Skills 管理与投影](#skills-管理与投影)
- [Token Usage 监控](#token-usage-监控)
- [版本更新](#版本更新)
- [配置文件说明](#配置文件说明)
- [环境变量覆盖](#环境变量覆盖)
- [本地开发与调试](#本地开发与调试)

---

## 项目介绍

Spark 适合同时使用 Codex、Claude Code、OpenCode、Grok Build、One 等 AI coding agent 的开发者。它把原本分散在各个工具里的模型、供应商、API Key、Base URL、MCP Server 和 Skills 配置收拢到一个终端界面或 Web 控制台中，通过 Profile 切换即可复用不同环境。

在启动 agent 时，Spark 会按当前 Profile 自动写入目标工具所需的配置；当上游 API 与 agent 期望的协议不一致时，会自动通过本地兼容代理做转换（例如 Codex 的 Responses 路径、Claude 的 Messages 路径等），同时支持 Gemini `generateContent` 与 OpenAI Chat Completions 等协议。

Spark 的目标不是替代 Codex 或 Claude，而是让这些工具在多模型、多供应商、多协议环境里更容易统一管理、安全隔离和快速启动。

---

## 核心特性

- **多 Agent 启动与配置注入**：支持 `codex`、`claude`、`opencode`、`grok`、`one`，自动生成对应工具的配置。
- **透明兼容代理 (Compat Gateway)**：本地代理覆盖 Codex `POST /v1/responses` 与 Claude `/v1/messages` 路径，可无缝对接到 OpenAI Chat Completions 等第三方上游。
- **多 Profile 与沙盒隔离**：为不同供应商、模型列表、默认模型、API Key 和 Base URL 保存独立配置；在启动 Grok 或 One 等 Agent 时通过临时 HOME 镜像隔离，避免全局凭据泄露或 OAuth 冲突。
- **双模体验 (TUI + Web Console)**：交互式终端 TUI 与浏览器 Web 管理控制台自由选择。
- **MCP Server 统一编排**：集中式添加、启用、禁用、导入、导出以及双向同步 MCP Server 到 Codex / Claude。
- **Skills 跨 Agent 投影**：支持本地/Git/Catalog 安装 Skill，一键自动投影分发至 `.agents`、`.codex`、`.claude` 目录。
- **Token Usage 记录**：本地零外部依赖记录代理请求消耗，支持在 TUI 中按 Today / 7D / 30D / All 查看统计。
- **一键自升级**：内置 `spark update`，支持 npm、bun、pnpm 全局安装及 GitHub Release 二进制原地更新。

---

## 支持的 Coding Agent

| 名称 | 状态 | 模式 / 协议 | 说明 |
| :--- | :--- | :--- | :--- |
| `codex` | 主要支持 | Responses / Compat Proxy | 配置并启动 Codex；上游为 Chat Completions 时自动挂载本地 Responses 兼容代理 |
| `claude` | 主要支持 | Messages / Compat Proxy | 配置并启动 Claude Code；自动挂载 Anthropic Messages 兼容代理，保留 thinking 思考过程 |
| `opencode` | 支持 | Direct OpenAI-compatible | 写入 OpenCode provider/model 配置并传递 `--model` 启动（OpenAI 规范） |
| `grok` | 支持 | Direct / GROK_HOME 隔离 | 写入 Grok Build `config.toml` 模型配置并在隔离临时环境中启动，避免 OAuth 401 拦截（别名 `grok-build`） |
| `one` | 支持 | Direct (4 Wire 协议) | 隔离 HOME 镜像启动（不改用户默认配置），原生匹配 responses / completions / messages / gemini 协议直连 |

---

## Web 配置中心与展示网站

### 1. Web 配置中心 (`spark httpserver`)

Spark 内置了可视化 Web 管理后台，可在浏览器中便捷管理 Prompts 预设与 Profiles 配置：

```bash
# 启动本地 Web 配置服务（默认监听 127.0.0.1:8765）
spark httpserver

# 自定义监听端口
spark httpserver --addr 127.0.0.1:9000
```

### 2. 独立展示网站 (`website/`)

项目根目录下的 `website/` 文件夹提供了 Spark 的专属独立介绍网站（基于 Tailwind CSS 与 Lucide 图标）：

- **路径**：`website/index.html`
- **内容**：全景特性展示、Agent 矩阵、架构工作流程、快速上手及 CLI 手册
- **运行方式**：可直接在浏览器中打开 `website/index.html`，或通过 `npx serve website` / 部署至静态托管平台

---

## 安装指南

### 方式一：npm 全局安装（推荐）

```bash
npm i -g @ngominhbinh708/spark
```

安装后直接在终端运行：

```bash
spark
```

### 方式二：从源码构建

```bash
git clone <repository-url>
cd spark
go build -o spark ./cmd/spark
```

> **环境要求**：Go `1.24.3` 或更高版本。

---

## 快速开始

### 交互式终端 TUI 流程

进入交互式主面板：

```bash
spark
```

1. 进入 `Manage profiles`，配置 `openai_base_url`、`api_key`、接口类型和模型。
2. 在模型列表中选择默认模型，必要时使用 `fetch / test` 检查端点连通性。
3. 返回主面板，选择 `Launch integration` 即可一键启动目标 Agent。

### 命令行直启

也可以无需进入 TUI 直接通过命令行启动：

```bash
# 启动 Codex 并指定模型与透传参数
spark launch codex --model gpt-4o -- --no-auto-approve

# 启动 Claude Code 并指定临时 Profile
spark launch claude --profile anthropic-official

# 启动 Grok Build 或 One
spark launch grok --model spark-grok-4.5
spark launch one --model gpt-4o
```

> `--` 之后的参数会原样传给目标集成工具。

---

## 命令速查

### 核心命令

```bash
spark                                     # 进入交互式 TUI
spark launch [agent] [--model <m>]       # 启动指定 Agent
spark config [agent] [--model <m>]       # 写入目标 Agent 配置但不启动
spark profile                             # 打开 Profile 管理界面
spark httpserver                          # 启动本地 Web 配置服务
spark usage [--model <m>]                 # 查看 Token 使用统计
spark version                             # 查看当前版本
spark version --check                     # 检查是否有新版本
spark update                              # 自动检查并升级到最新版
```

---

## MCP Servers 管理

集中管理 MCP Server 并双向同步至 Codex 与 Claude Code：

```bash
spark mcp                                 # 进入 MCP 交互式管理面板
spark mcp list                            # 列出所有已配置的 MCP Server
spark mcp show <name>                     # 查看指定 MCP Server 详情
spark mcp add <name> --command <cmd> --args <a,b,c>  # 添加 stdio 类型 MCP
spark mcp add <name> --url <http-url>     # 添加 SSE / HTTP 类型 MCP
spark mcp enable <name>                   # 启用 MCP Server
spark mcp disable <name>                  # 禁用 MCP Server
spark mcp remove <name>                   # 移除 MCP Server
spark mcp import codex                    # 从 Codex 导入 MCP 配置
spark mcp import claude                   # 从 Claude Code 导入 MCP 配置
spark mcp sync codex                      # 将 MCP 配置同步写入 Codex
spark mcp sync claude                     # 将 MCP 配置同步写入 Claude
spark mcp export claude                   # 导出为 Claude 配置格式
```

---

## Skills 管理与投影

统一安装 Agent 技能并自动投影分发：

```bash
spark skill                               # 进入 Skills 交互式管理面板
spark skill list                          # 列出本地已安装技能
spark skill show <name>                   # 查看技能详情与 Manifest
spark skill search <query>                # 在线搜索技能目录
spark skill install <name> --source <path> --source-type local  # 本地安装
spark skill install <name> --source <url> --source-type git     # Git 安装
spark skill enable <name>                 # 启用技能
spark skill disable <name>                # 禁用技能
spark skill remove <name>                 # 删除技能
spark skill sync                          # 投影已启用技能至 agent 目录
spark skill upgrade                       # 升级技能版本
```

> `spark skill sync` 默认投影目标为 `agents,codex,claude`，可通过 `--target` / `--targets` 自定义。

---

## Token Usage 监控

兼容代理请求会自动在本地记录 Token 消耗：

```bash
# 查看全局 Token 使用统计
spark usage

# 筛选特定模型
spark usage --model gpt-4o
```

在 TUI 面板中，可按 **Today / 7D / 30D / All** 维度查看输入/输出 Token、Prompt Caching 以及费用估算。

---

## 版本更新

Spark 支持智能识别安装方式并原地自升级：

```bash
# 检查新版本
spark update --check

# 执行升级
spark update
```

- **npm / bun / pnpm 全局安装**：自动执行包管理器全局更新命令。
- **二进制运行**：自动从 GitHub Release 下载匹配当前平台的二进制文件并校验 `checksums.txt` 原地替换。

---

## 配置文件说明

Spark 配置文件位于：`~/.spark/config.json`

```json
{
  "version": 1,
  "default_profile": "default",
  "profiles": {
    "default": {
      "openai_base_url": "https://api.openai.com/v1",
      "api_key": "sk-...",
      "openai_api_type": "responses,chat_completions",
      "models": ["gpt-4o", "gpt-4o-mini"],
      "default_model": "gpt-4o"
    }
  },
  "integrations": {
    "codex": {
      "profile": "default"
    }
  },
  "mcp_servers": {}
}
```

### Profile 字段详解

| 字段 | 说明 |
| :--- | :--- |
| `openai_base_url` | OpenAI-compatible API 端点 |
| `api_key` | 当前 Profile 的 API Key |
| `openai_api_type` | 接口类型，支持 `responses`、`chat_completions`、`gemini_generate_content`、`anthropic_messages`（可用逗号组合） |
| `openai_org` | OpenAI organization ID（可选） |
| `openai_project` | OpenAI project ID（可选） |
| `model_list_url` | 自定义模型列表获取接口（可选） |
| `anthropic_base_url` | Anthropic 专属 Base URL（可选） |
| `models` | 可选模型列表 |
| `default_model` | 默认模型 |

---

## 环境变量覆盖

启动集成时，可通过系统环境变量临时覆盖 Profile 配置：

| 环境变量 | 说明 |
| :--- | :--- |
| `OPENAI_BASE_URL` | 覆盖 OpenAI-compatible Base URL |
| `OPENAI_API_KEY` | 覆盖 OpenAI-compatible API Key |
| `ANTHROPIC_BASE_URL` | 覆盖 Anthropic Base URL |
| `ANTHROPIC_AUTH_TOKEN` | 覆盖 Anthropic Auth Token |

---

## 本地开发与调试

```bash
# 安装 Go 依赖
go mod tidy

# 运行完整测试套件
go test ./...

# 本地编译与运行
go build -o spark ./cmd/spark
./spark

# 渲染 TUI 状态快照（调试用）
spark debug snapshot dashboard --width 90 --height 24
spark debug snapshot profile --width 120 --height 36
spark debug snapshot token-usage --width 100 --height 30
spark debug snapshot mcp --width 120 --height 36
spark debug snapshot skill --width 120 --height 36
```
