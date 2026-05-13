# spark

[English](#english) | [中文](#中文)

---

<a name="english"></a>
## English

A unified launcher for AI coding agents with configurable OpenAI-compatible gateways.

## Features

- 🚀 **Multi-Agent Support**: Launch Claude Code, Codex, Droid, OpenCode, OpenClaw, Pi and more
- 🔧 **Flexible Configuration**: Multiple profiles with different API endpoints and models
- 🔄 **Compatibility Layer**: Automatic protocol translation between Anthropic/Responses and OpenAI Chat Completions
- 💾 **Configuration Persistence**: Save your settings and model history
- 🎯 **Interactive TUI**: User-friendly terminal interface for selection and configuration

## Installation

### Install via npm (Recommended)

```bash
npm i -g @ngominhbinh708/spark
spark
```

### Build from Source

```bash
git clone <repository-url>
cd spark
go build -o spark ./cmd/spark
```

### Install to PATH

```bash
go install ./cmd/spark
```

## Quick Start

```bash
# Interactive mode
spark

# Launch a specific integration
spark launch claude

# Configure without launching
spark config codex

# Manage gateway profiles
spark profile
```

## Usage

### Interactive Mode

Run without arguments to enter interactive mode:

```bash
spark
```

You'll see a menu with options:
- **Launch integration**: Select and configure an AI coding agent
- **Manage profiles**: Create/edit/delete gateway profiles
- **Show config file**: Display the configuration file path
- **Quit**: Exit the application

### Launch Command

```bash
# Launch with interactive selection
spark launch

# Launch a specific integration
spark launch claude
spark launch codex
spark launch droid
spark launch opencode
spark launch openclaw
spark launch pi

# Specify model and profile
spark launch claude --model claude-sonnet-4-20250514 --profile work

# Configure only (don't launch)
spark launch codex --config

# Pass arguments to the integration
spark launch codex -- resume abc123
spark launch codex --model gpt-4o -- --no-auto-approve
```

`--` 之前是 spark 自己的参数，`--` 之后的所有内容原样传给集成。

### Config Command

Configure an integration without launching:

```bash
spark config codex --model gpt-4o --profile default
```

### Profile Management

```bash
spark profile
```

This opens an interactive profile manager where you can:
- Add new profiles
- Edit existing profiles
- Delete profiles
- Set default profile
- Test connection

## Configuration

Configuration is stored at `~/.spark/config.json`

### Configuration Structure

```json
{
  "version": 1,
  "default_profile": "default",
  "profiles": {
    "default": {
      "openai_base_url": "https://api.openai.com/v1",
      "openai_api_key": "sk-...",
      "models": ["gpt-4o", "gpt-4o-mini"]
    },
    "work": {
      "openai_base_url": "https://api.company.com/v1",
      "openai_api_key": "...",
      "openai_org": "org-...",
      "models": ["custom-model"]
    },
    "anthropic": {
      "anthropic_base_url": "https://api.anthropic.com",
      "anthropic_auth_token": "...",
      "models": ["claude-sonnet-4-20250514"]
    }
  },
  "integrations": {
    "claude": {
      "profile": "anthropic"
    }
  },
  "history": {
    "last_selection": "claude",
    "last_model_input": "gpt-4o",
    "model_inputs": ["gpt-4o", "claude-sonnet-4-20250514"]
  }
}
```

### Profile Fields

| Field | Description |
|-------|-------------|
| `openai_base_url` | OpenAI-compatible API endpoint |
| `openai_api_key` | API key for authentication |
| `openai_org` | OpenAI organization ID (optional) |
| `openai_project` | OpenAI project ID (optional) |
| `anthropic_base_url` | Anthropic API endpoint (optional) |
| `anthropic_auth_token` | Anthropic auth token (optional) |
| `models` | Default models for this profile |
| `default_model` | Fallback model if models list is empty |

## Supported Integrations

| Integration | Type | Description |
|-------------|------|-------------|
| **Claude Code** | Runner | Anthropic's official coding agent |
| **Codex** | Runner | OpenAI's terminal-based coding agent |
| **Droid** | Editor | Factory AI's coding assistant |
| **OpenCode** | Editor | Open-source coding agent |
| **OpenClaw** | Editor | Alternative coding agent |
| **Pi** | Editor | Pi coding agent by @mariozechner |

### Integration Types

- **Runner**: Launches directly with environment configuration
- **Editor**: Modifies configuration files before launching

## Compatibility Adapters

spark includes automatic protocol translation for integrations that use non-OpenAI APIs:

### Codex (Responses API)

When your gateway doesn't support OpenAI's `/v1/responses` endpoint, spark automatically:
1. Detects gateway capabilities
2. Spins up a local compatibility proxy
3. Translates between Responses and Chat Completions formats
4. Handles streaming events and tool calls

### Claude (Anthropic API)

For Claude Code with non-Anthropic endpoints:
1. Starts a local Anthropic-to-OpenAI proxy
2. Translates Anthropic Messages API to OpenAI Chat Completions
3. Handles streaming with proper event formatting

## Environment Variables

spark respects these environment variables when launching integrations:

| Variable | Description |
|----------|-------------|
| `OPENAI_BASE_URL` | Override API base URL |
| `OPENAI_API_KEY` | Override API key |
| `ANTHROPIC_BASE_URL` | Anthropic-specific endpoint |
| `ANTHROPIC_AUTH_TOKEN` | Anthropic auth token |

## Development

### Prerequisites

- Go 1.24+

### Build & Test

```bash
# Download dependencies
go mod tidy

# Run tests
go test ./...

# Build binary
go build -o spark ./cmd/spark

# Run directly
go run ./cmd/spark

# Learn and store changed TUI navigation steps
node scripts/tui-state-navigator.js --target mcp.transfer --learn --trace
node scripts/tui-state-navigator.js --target mcp.transfer --trace --ansi --show-focus
node scripts/tui-state-navigator.js --all
node scripts/tui-state-navigator.js --map path/to/other-tui-map.json --cmd "your-tui-command" --all
```

### Release Flow

1. Merge changes to `main`.
2. `Release Please` opens or updates a release PR.
3. Merge the release PR to create tag `vX.Y.Z`.
4. `Release` workflow publishes binaries and npm package.

See `docs/deployment-workflow.md` for details.

## Troubleshooting

### Integration not found

Make sure the integration is installed:
- **Claude Code**: `claude` command or download from https://code.claude.com
- **Codex**: `npm install -g @openai/codex`
- **Droid**: Download from https://docs.factory.ai
- **OpenCode**: Download from https://opencode.ai
- **Pi**: `npm install -g @mariozechner/pi-coding-agent`

### Connection errors

1. Check your API base URL is correct
2. Verify your API key is valid
3. Use `spark profile` to test your connection

---

<a name="中文"></a>
## 中文

一个统一的 AI 编码代理启动器，支持可配置的 OpenAI 兼容网关。

## 特性

- 🚀 **多代理支持**: 启动 Claude Code、Codex、Droid、OpenCode、OpenClaw、Pi 等
- 🔧 **灵活配置**: 支持多个配置文件，不同的 API 端点和模型
- 🔄 **兼容层**: 自动在 Anthropic/Responses 和 OpenAI Chat Completions 之间转换协议
- 💾 **配置持久化**: 保存你的设置和模型历史
- 🎯 **交互式 TUI**: 用户友好的终端界面用于选择和配置

## 安装

### 通过 npm 安装（推荐）

```bash
npm i -g @ngominhbinh708/spark
spark
```

### 从源码构建

```bash
git clone <repository-url>
cd spark
go build -o spark ./cmd/spark
```

### 安装到 PATH

```bash
go install ./cmd/spark
```

## 快速开始

```bash
# 交互模式
spark

# 启动特定集成
spark launch claude

# 仅配置不启动
spark config codex

# 管理网关配置文件
spark profile
```

## 使用方法

### 交互模式

不带参数运行进入交互模式：

```bash
spark
```

你会看到以下选项菜单：
- **启动集成**: 选择并配置 AI 编码代理
- **管理配置文件**: 创建/编辑/删除网关配置文件
- **显示配置文件**: 显示配置文件路径
- **退出**: 退出应用程序

### 启动命令

```bash
# 交互选择启动
spark launch

# 启动特定集成
spark launch claude
spark launch codex
spark launch droid
spark launch opencode
spark launch openclaw
spark launch pi

# 指定模型和配置文件
spark launch claude --model claude-sonnet-4-20250514 --profile work

# 仅配置（不启动）
spark launch codex --config

# 传递参数给集成
spark launch codex -- resume abc123
spark launch codex --model gpt-4o -- --no-auto-approve
```

`--` 之前是 spark 自己的参数，`--` 之后的所有内容原样传给集成。

### 配置命令

不启动直接配置集成：

```bash
spark config codex --model gpt-4o --profile default
```

### 配置文件管理

```bash
spark profile
```

打开交互式配置管理器，你可以：
- 添加新配置文件
- 编辑现有配置文件
- 删除配置文件
- 设置默认配置文件
- 测试连接

## 配置

配置存储在 `~/.spark/config.json`

### 配置结构

```json
{
  "version": 1,
  "default_profile": "default",
  "profiles": {
    "default": {
      "openai_base_url": "https://api.openai.com/v1",
      "openai_api_key": "sk-...",
      "models": ["gpt-4o", "gpt-4o-mini"]
    },
    "work": {
      "openai_base_url": "https://api.company.com/v1",
      "openai_api_key": "...",
      "openai_org": "org-...",
      "models": ["custom-model"]
    },
    "anthropic": {
      "anthropic_base_url": "https://api.anthropic.com",
      "anthropic_auth_token": "...",
      "models": ["claude-sonnet-4-20250514"]
    }
  },
  "integrations": {
    "claude": {
      "profile": "anthropic"
    }
  },
  "history": {
    "last_selection": "claude",
    "last_model_input": "gpt-4o",
    "model_inputs": ["gpt-4o", "claude-sonnet-4-20250514"]
  }
}
```

### 配置文件字段

| 字段 | 描述 |
|------|------|
| `openai_base_url` | OpenAI 兼容 API 端点 |
| `openai_api_key` | 用于认证的 API 密钥 |
| `openai_org` | OpenAI 组织 ID（可选） |
| `openai_project` | OpenAI 项目 ID（可选） |
| `anthropic_base_url` | Anthropic API 端点（可选） |
| `anthropic_auth_token` | Anthropic 认证令牌（可选） |
| `models` | 此配置文件的默认模型 |
| `default_model` | 如果模型列表为空的备用模型 |

## 支持的集成

| 集成 | 类型 | 描述 |
|------|------|------|
| **Claude Code** | Runner | Anthropic 官方编码代理 |
| **Codex** | Runner | OpenAI 的终端编码代理 |
| **Droid** | Editor | Factory AI 的编码助手 |
| **OpenCode** | Editor | 开源编码代理 |
| **OpenClaw** | Editor | 替代编码代理 |
| **Pi** | Editor | @mariozechner 的 Pi 编码代理 |

### 集成类型

- **Runner**: 使用环境配置直接启动
- **Editor**: 在启动前修改配置文件

## 兼容性适配器

spark 为使用非 OpenAI API 的集成提供自动协议转换：

### Codex (Responses API)

当你的网关不支持 OpenAI 的 `/v1/responses` 端点时，spark 自动：
1. 检测网关能力
2. 启动本地兼容代理
3. 在 Responses 和 Chat Completions 格式之间转换
4. 处理流式事件和工具调用

### Claude (Anthropic API)

对于使用非 Anthropic 端点的 Claude Code：
1. 启动本地 Anthropic 到 OpenAI 的代理
2. 将 Anthropic Messages API 转换为 OpenAI Chat Completions
3. 使用正确的事件格式处理流式传输

## 环境变量

启动集成时 spark 会识别以下环境变量：

| 变量 | 描述 |
|------|------|
| `OPENAI_BASE_URL` | 覆盖 API 基础 URL |
| `OPENAI_API_KEY` | 覆盖 API 密钥 |
| `ANTHROPIC_BASE_URL` | Anthropic 专用端点 |
| `ANTHROPIC_AUTH_TOKEN` | Anthropic 认证令牌 |

## 故障排除

### 找不到集成

确保已安装集成：
- **Claude Code**: `claude` 命令或从 https://code.claude.com 下载
- **Codex**: `npm install -g @openai/codex`
- **Droid**: 从 https://docs.factory.ai 下载
- **OpenCode**: 从 https://opencode.ai 下载
- **Pi**: `npm install -g @mariozechner/pi-coding-agent`

### 连接错误

1. 检查你的 API 基础 URL 是否正确
2. 验证你的 API 密钥是否有效
3. 使用 `spark profile` 测试你的连接

## License

MIT
