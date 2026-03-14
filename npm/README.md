# @ngominhbinh708/spark

[English](#english) | [中文](#中文)

---

<a name="english"></a>
## English

A unified launcher for AI coding agents with configurable OpenAI-compatible gateways.

### Installation

```bash
npm i -g @ngominhbinh708/spark
```

**How it works**: This package uses [npm alias](https://docs.npmjs.com/cli/v9/commands/npm-install#aliases) to automatically install the correct binary for your platform. No additional download scripts needed.

### Platform Support

| OS | Architecture | npm alias |
|----|--------------|-----------|
| macOS | ARM64 (M1/M2) | `@ngominhbinh708/spark-darwin-arm64` |
| macOS | x64 (Intel) | `@ngominhbinh708/spark-darwin-x64` |
| Linux | ARM64 | `@ngominhbinh708/spark-linux-arm64` |
| Linux | x64 | `@ngominhbinh708/spark-linux-x64` |
| Windows | ARM64 | `@ngominhbinh708/spark-windows-arm64` |
| Windows | x64 | `@ngominhbinh708/spark-windows-x64` |

### Usage

```bash
# Run spark
spark

# Show version
spark --version

# Show help
spark --help
```

### npm Alias Architecture

The main package defines `optionalDependencies` using npm alias syntax:

```json
{
  "optionalDependencies": {
    "@ngominhbinh708/spark-linux-x64": "npm:@ngominhbinh708/spark@0.1.6-linux-x64",
    "@ngominhbinh708/spark-darwin-arm64": "npm:@ngominhbinh708/spark@0.1.6-darwin-arm64",
    ...
  }
}
```

npm will:
1. Install the main package
2. Evaluate each optionalDependency's `os`/`cpu` constraints
3. Only install the matching platform variant

This is the same approach used by [@openai/codex](https://github.com/openai/codex).

### Troubleshooting

#### Platform package not found

```
Platform package not found: @ngominhbinh708/spark-darwin-arm64
```

Try reinstalling:

```bash
npm uninstall -g @ngominhbinh708/spark
npm install -g @ngominhbinh708/spark@latest
```

---

<a name="中文"></a>
## 中文

一个统一的 AI 编码代理启动器，支持可配置的 OpenAI 兼容网关。

### 安装

```bash
npm i -g @ngominhbinh708/spark
```

**工作原理**：此包使用 [npm alias](https://docs.npmjs.com/cli/v9/commands/npm-install#aliases) 自动安装适合你平台的二进制文件，无需额外的下载脚本。

### 支持的平台

| 操作系统 | 架构 | npm 别名 |
|---------|------|---------|
| macOS | ARM64 (M1/M2) | `@ngominhbinh708/spark-darwin-arm64` |
| macOS | x64 (Intel) | `@ngominhbinh708/spark-darwin-x64` |
| Linux | ARM64 | `@ngominhbinh708/spark-linux-arm64` |
| Linux | x64 | `@ngominhbinh708/spark-linux-x64` |
| Windows | ARM64 | `@ngominhbinh708/spark-windows-arm64` |
| Windows | x64 | `@ngominhbinh708/spark-windows-x64` |

### 使用方法

```bash
# 运行 spark
spark

# 显示版本
spark --version

# 显示帮助
spark --help
```

### 故障排除

#### 找不到平台包

```
Platform package not found: @ngominhbinh708/spark-darwin-arm64
```

尝试重新安装：

```bash
npm uninstall -g @ngominhbinh708/spark
npm install -g @ngominhbinh708/spark@latest
```

---

## Development

### Building Platform Packages

```bash
cd npm
node scripts/build-packages.js
```

This downloads binaries from GitHub Releases and creates platform packages.

### Publishing

The release workflow publishes:
1. Platform packages: `@ngominhbinh708/spark@VERSION-PLATFORM-ARCH`
2. Main package: `@ngominhbinh708/spark@VERSION`

---

## License

MIT
