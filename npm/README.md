# spark-agent-launcher

[English](#english) | [中文](#中文)

---

<a name="english"></a>
## English

A cross-platform CLI tool for launching Spark agents.

### Installation

```bash
npm i -g spark-agent-launcher
```

### Usage

```bash
# Run spark
spark

# Show help
spark --help
```

### Configuration

Spark stores configuration in `~/.spark/config.json`. You can configure:

- OpenAI API key and base URL
- Default model and profiles
- Anthropic credentials

```bash
# Configure your profile
spark
# Follow the interactive prompts to set up your API key and model
```

### Environment Variables

| Variable | Description |
|----------|-------------|
| `OPENAI_BASE_URL` | Override API base URL |
| `OPENAI_API_KEY` | Override API key |
| `ANTHROPIC_BASE_URL` | Anthropic-specific endpoint |
| `ANTHROPIC_AUTH_TOKEN` | Anthropic auth token |
| `SPARK_BINARY_URL` | Direct download URL for binary |
| `SPARK_BINARY_BASE_URL` | Base URL for GitHub releases mirror |
| `SPARK_BINARY_VERSION` | Specific version to download |

### Manual Binary Download

If npm install fails, you can manually download the binary:

1. Visit [GitHub Releases](https://github.com/zhangsanfeng1094/spark/releases)
2. Download the appropriate binary for your platform
3. Add it to your PATH

### Troubleshooting

#### `E403` error during npm publish

Use an npm **Automation** token with publish permission and bypass 2FA enabled.

#### Network errors during install

```bash
# Use a mirror
SPARK_BINARY_BASE_URL="https://your-mirror.com/releases" npm i -g spark-agent-launcher

# Or direct URL
SPARK_BINARY_URL="https://direct-url/spark-linux-amd64" npm i -g spark-agent-launcher
```

---

<a name="中文"></a>
## 中文

跨平台的 Spark Agent 启动器 CLI 工具。

### 安装

```bash
npm i -g spark-agent-launcher
```

### 使用方法

```bash
# 运行 spark
spark

# 显示帮助
spark --help
```

### 配置

Spark 将配置存储在 `~/.spark/config.json`，你可以配置：

- OpenAI API 密钥和基础 URL
- 默认模型和配置文件
- Anthropic 凭证

```bash
# 配置你的配置文件
spark
# 按照交互提示设置你的 API 密钥和模型
```

### 环境变量

| 变量 | 描述 |
|------|------|
| `OPENAI_BASE_URL` | 覆盖 API 基础 URL |
| `OPENAI_API_KEY` | 覆盖 API 密钥 |
| `ANTHROPIC_BASE_URL` | Anthropic 专用端点 |
| `ANTHROPIC_AUTH_TOKEN` | Anthropic 认证令牌 |
| `SPARK_BINARY_URL` | 二进制文件的直接下载 URL |
| `SPARK_BINARY_BASE_URL` | GitHub releases 镜像基础 URL |
| `SPARK_BINARY_VERSION` | 要下载的特定版本 |

### 手动下载二进制文件

如果 npm 安装失败，你可以手动下载二进制文件：

1. 访问 [GitHub Releases](https://github.com/zhangsanfeng1094/spark/releases)
2. 下载适合你平台的二进制文件
3. 将其添加到 PATH 中

### 故障排除

#### npm 发布时出现 `E403` 错误

使用具有发布权限并启用 2FA 绑过的 npm **Automation** 令牌。

#### 安装时网络错误

```bash
# 使用镜像
SPARK_BINARY_BASE_URL="https://your-mirror.com/releases" npm i -g spark-agent-launcher

# 或使用直接 URL
SPARK_BINARY_URL="https://direct-url/spark-linux-amd64" npm i -g spark-agent-launcher
```

---

## License

MIT
