# Spark 接入 Bifrost Core 替代自研兼容层实施方案

> **文档状态**：Completed  
> **目标**：全面使用 `github.com/maximhq/bifrost/core` 替代 Spark 现有的 `internal/compat/target` 与自研上游网关转发逻辑。  
> **适用对象**：Coding Agent、后端研发工程师  

---

## 1. 架构演进与替换范围 (Before vs After)

### 1.1 现状与痛点 (Before)
- Spark 早期在 `internal/compat/` 下自研了针对 OpenAI Chat、OpenAI Responses、Anthropic Messages 等目标上游的编解码器与 SSE 流式扫描器。
- 维护 20+ 家不同 Provider 的认证方式（AWS IAM 签名、Google OAuth、Azure MSAL 等）、连接池、重试与流式规范化负担沉重。

### 1.2 目标架构 (After)
将 Spark 的定位聚焦在 **“Coding Agent 入口适配与 Profile 编排”**，将 **“所有上游多模型协议转换、HTTP 发送与流式归一化”** 委托给 **Bifrost Core**：

```text
               ┌───────────────────────────────────────────────────────────┐
               │                  Spark Ingress (入口层)                   │
               │                                                           │
Codex CLI ────►│  • POST /v1/responses ──┐                                │
Claude CLI ───►│  • POST /v1/messages  ──┼─► 转为 schemas.BifrostChatRequest│
One / Grok ───►│  • POST /v1/chat/compl ─┘                                │
               └─────────────────────────────┬─────────────────────────────┘
                                             │ Go 内存函数调用
                                             ▼
               ┌───────────────────────────────────────────────────────────┐
               │                Bifrost Core (核心驱动引擎)                │
               │                                                           │
               │  • Provider 协议映射 (OpenAI, Claude, Gemini, Bedrock...) │
               │  • 认证签名 (AWS SigV4, Google ADC, Azure MSAL...)       │
               │  • 连接池、超时重试、自动 Fallback                        │
               │  • 统一流式通道 (chan *schemas.BifrostStreamChunk)        │
               └─────────────────────────────┬─────────────────────────────┘
                                             │
                                             ▼
                                     各家 LLM 上游接口
```

---

## 2. 详细替换矩阵

| 现有模块 (Spark Homegrown) | 迁移后状态 | 替代方案 (Bifrost Core) |
|---|---|---|
| `internal/compat/target/openai_chat` | **已删除** | 使用 `schemas.OpenAI` 原生 Provider 驱动 |
| `internal/compat/target/openai_responses` | **已删除** | 使用 Bifrost Responses 转换通道 |
| `internal/compat/target/anthropic_messages`| **已删除** | 使用 `schemas.Anthropic` 原生 Provider 驱动 |
| `internal/compat/gateway/codex_responses_forward.go` | **已替换** | 接入 `ingress/codex`：输入转 `BifrostChatRequest`，输出流写 Codex SSE |
| `internal/compat/gateway/anthropic_messages_handler.go`| **已替换** | 接入 `ingress/claude`：输入转 `BifrostChatRequest`，输出流写 Anthropic SSE |
| `internal/compat/gateway/bridge/stream.go` | **已删除** | 使用 Bifrost `ChatCompletionStreamRequest` 统一通道 |
| 自研 Usage 记录与日志 | **已重构接入** | 流/非流结束时从 `Usage` 回调写入 Spark SQLite，失败记录错误日志 |

---

## 3. 分阶段实施任务清单

### 阶段一：依赖引入与引擎单例初始化
**目标**：在 Spark 中引入 `github.com/maximhq/bifrost/core` 并完成引擎生命周期管理。

- [x] **任务 1.1**：在 `go.mod` 中引入 `github.com/maximhq/bifrost/core`。
- [x] **任务 1.2**：创建 `internal/compat/engine/bifrost.go`，封装引擎单例初始化与生命周期。
- [x] **任务 1.3**：实现 Spark Profile 到 Bifrost Provider 配置的映射函数。

---

### 阶段二：Codex Ingress 适配器实现 (`/v1/responses`)
**目标**：支持 Codex CLI 发送的 `POST /v1/responses` 经由 Bifrost 路由至任意上游 Provider。

- [x] **任务 2.1**：创建 `internal/compat/ingress/codex/request.go`：
  - 将 Codex 的 `POST /v1/responses` 请求体（包含 `instructions`、`input`、`tools`、`reasoning` 配置）转换为 `schemas.BifrostChatRequest`。
- [x] **任务 2.2**：创建 `internal/compat/ingress/codex/stream.go`：
  - 读取 Bifrost `chan *schemas.BifrostStreamChunk`；
  - 将统一的 chunk 转换为 Codex 期望的 `response.output_item.added`、`response.output_text.delta`、`response.function_call_arguments.delta`、`response.completed` 等 SSE 事件并 Flush。
- [x] **任务 2.3**：创建 `internal/compat/ingress/codex/handler.go`，实现标准 `http.Handler`。

---

### 阶段三：Claude Ingress 适配器实现 (`/v1/messages`)
**目标**：支持 Claude Code 发送的 `POST /v1/messages` 经由 Bifrost 路由至任意上游 Provider。

- [x] **任务 3.1**：创建 `internal/compat/ingress/claude/request.go`：
  - 将 Anthropic Messages 请求体（包含 `system`、`messages` content blocks、`tools`、`thinking`）转换为 `schemas.BifrostChatRequest`。
- [x] **任务 3.2**：创建 `internal/compat/ingress/claude/stream.go`：
  - 监听 `chan *schemas.BifrostStreamChunk`；
  - 格式化为 Anthropic Messages 规范的 `message_start`、`content_block_start`、`content_block_delta`（含 `text_delta`、`thinking_delta`、`input_json_delta`）、`message_delta`、`message_stop`。
- [x] **任务 3.3**：创建 `internal/compat/ingress/claude/handler.go`。

---

### 阶段四：Token Usage 监控与 Hook 拦截接入
**目标**：无缝对接 Spark 现有的 SQLite Usage 监控与 TUI 统计。

- [x] **任务 4.1**：在调用 Bifrost 时注入统一的上下文 `BifrostContext`：
  ```go
  bCtx := schemas.NewBifrostContext(r.Context(), schemas.NoDeadline)
  ```
- [x] **任务 4.2**：在非流式与流式结束时，提取 `resp.Usage` / `chunk.Usage`，调用 `spark/internal/usage.AppendDefault(...)` 写入本地数据库，失败捕获打日志。
- [x] **任务 4.3**：利用 Bifrost 的 `PreLLMHook` / `PostLLMHook` 机制实现轻量请求日志与错误捕获。

---

### 阶段五：清理与废弃旧代码
**目标**：彻底剥离旧的自研 Target 协议层，降低维护包袱。

- [x] **任务 5.1**：删除废弃目录：
  - `internal/compat/target/`
  - `internal/compat/gateway/`
  - `internal/compat/client/`
  - `internal/compat/ir/`
  - `internal/compat/policy/`
  - `internal/compat/httpjson/`
  - `internal/compat/logutil/`
- [x] **任务 5.2**：简化 `internal/compat/proxy/`，直接挂载新的 Ingress Handlers。
- [x] **任务 5.3**：更新 `cmd/spark`、`internal/app/` 与 `internal/integrations/` 中的代理/daemon 启动逻辑。

---

## 4. 核心代码设计规范

### 4.1 Ingress 执行流示例 (以 Codex 为例)

```go
func (h *CodexHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
    // 1. 解码 Codex /v1/responses JSON
    rawReq, err := decodeJSON(r.Body)
    if err != nil {
        http.Error(w, "invalid json", http.StatusBadRequest)
        return
    }

    // 2. 转为统一的 BifrostChatRequest
    bReq := codex.ToBifrostRequest(rawReq, h.profile)
    bCtx := schemas.NewBifrostContext(r.Context(), schemas.NoDeadline)

    // 3. 判断是否流式
    if bReq.Stream {
        streamChan, bErr := h.engine.Client().ChatCompletionStreamRequest(bCtx, bReq)
        if bErr != nil {
            writeCodexError(w, bErr.StatusCode, bErr.Message)
            return
        }

        // 4. 将 Bifrost Chunk 转换并写入下游 SSE
        codex.ForwardStream(w, streamChan)
        return
    }

    // 5. 非流式调用
    resp, bErr := h.engine.Client().ChatCompletionRequest(bCtx, bReq)
    if bErr != nil {
        writeCodexError(w, bErr.StatusCode, bErr.Message)
        return
    }

    codex.WriteResponse(w, resp)
}
```

---

## 5. 验收标准与测试验证

1. **Codex 兼容性测试**：
   - 运行 `spark launch codex --model gpt-4o`，验证普通问答、代码多轮对话可用；
   - 验证 Tool Call（如只读文件、运行测试）能够正常触发并返回执行结果；
   - 验证思考模型（如 o1 / o3-mini / deepseek-reasoner）思考链正常折叠与展示。
2. **Claude Code 兼容性测试**：
   - 运行 `spark launch claude --model claude-3-5-sonnet`，验证 Messages 流式协议正常；
   - 验证 thinking block 能够正常解析与流式传递。
3. **多 Provider 直连验证**：
   - Profile 分别配置 OpenAI 官方、DeepSeek、Anthropic 官方、Google Gemini、AWS Bedrock 等端点，确保全链路畅通。
4. **Usage 记录验证**：
   - 请求完成后运行 `spark usage`，确保输入/输出 Token 准确入库。
