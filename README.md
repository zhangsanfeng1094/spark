# spark

A unified launcher for AI coding agents with configurable OpenAI-compatible gateways.

## Features

- 🚀 **Multi-Agent Support**: Launch Claude Code, Codex, Droid, OpenCode, OpenClaw, Pi and more
- 🔧 **Flexible Configuration**: Multiple profiles with different API endpoints and models
- 🔄 **Compatibility Layer**: Automatic protocol translation between Anthropic/Responses and OpenAI Chat Completions
- 💾 **Configuration Persistence**: Save your settings and model history
- 🎯 **Interactive TUI**: User-friendly terminal interface for selection and configuration

## Installation

### Build from Source

```bash
git clone <repository-url>
cd spark
go build -o spark ./cmd/spark
```

### Install via npm (wrapper)

This repository includes an npm wrapper at `npm/` so users can install `spark` as a global command.

```bash
cd npm
npm install -g .
spark
```

For public publishing steps, see `npm/README.md`.

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

# Pass extra arguments to the integration
spark launch claude -- --dangerously-skip-permissions
```

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

# Run tests with race detector
go test -race ./...

# Build binary
go build -o spark ./cmd/spark

# Run directly
go run ./cmd/spark
```

### Release flow

Detailed SOP: `docs/deployment-workflow.md`

Recommended flow:
1. Merge changes to `main`.
2. `Release Please` (`.github/workflows/release-please.yml`) opens or updates a release PR.
3. Merge the release PR to create and push tag `vX.Y.Z`.
4. `Release` (`.github/workflows/release.yml`) runs on that tag and publishes:
   - GitHub release binaries via GoReleaser
   - npm package from `npm/`

Manual fallback:
- `release.yml` also supports `workflow_dispatch` with an existing tag input (for example `v0.1.6`).

The GoReleaser output names are defined in `./.goreleaser.yml` and must stay aligned with `npm/bin/install.js`:
- `spark-darwin-amd64`
- `spark-darwin-arm64`
- `spark-linux-amd64`
- `spark-linux-arm64`
- `spark-windows-amd64.exe`
- `spark-windows-arm64.exe`

### Project Structure

```
spark/
├── cmd/spark/              # Application entry point
│   └── main.go
├── internal/
│   ├── app/                # CLI commands and logic
│   ├── config/             # Configuration management
│   ├── integrations/       # Integration implementations
│   └── tui/                # Terminal UI components
├── docs/                   # Architecture documentation
├── go.mod
└── README.md
```

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

### Debug logs

Compatibility adapters write logs to `~/.spark/logs/` by default (or custom path via env vars), rotate daily, and keep the latest 7 days:
- `codex-compat-*.log`
- `anthropic-compat-*.log`

## License

MIT
