# Compat IR Migration Plan

## 背景

当前 Spark 的兼容代理是直接协议互转：

- Codex: `Responses API` -> OpenAI `Chat Completions` -> `Responses API`
- Claude Code: Anthropic `Messages API` -> OpenAI `Chat Completions` -> Anthropic `Messages API`

这种方式短期能修 bug，但会让 `reasoning_content`、工具调用、流式聚合、usage、错误格式等字段在每条链路里重复处理。随着 OpenAI、Anthropic、Gemini 输入和不同目标协议增多，直接互转会变成 N x M 的复杂度。

目标是改成：

```text
Client Protocol -> Inbound Adapter -> Compat IR -> Policy/Middleware -> Target Adapter -> Provider
Provider -> Target Adapter -> Compat IR -> Client Adapter -> Client Protocol
```

## 参考

DeepWiki 查询 `looplj/axonhub` 没返回代码上下文。后续参考改用公开资料和源码入口：

- AxonHub README: https://github.com/looplj/axonhub
- AxonHub package docs: https://pkg.go.dev/github.com/looplj/axonhub/internal/llm
- OpenAI outbound transformer example: https://raw.githubusercontent.com/looplj/axonhub/unstable/llm/transformer/openai/outbound.go

AxonHub 的核心思路可以借鉴：Inbound Transformer 把客户端协议标准化为统一请求模型，Outbound Transformer 再把统一模型转成目标提供商协议。Spark 不建议直接大块搬代码；如果后续要搬，应先确认许可证、类型依赖和归属说明。

## 目标架构

### 1. HTTP Handler 层

保留当前集成入口：

- Codex 继续暴露 `/v1/responses`
- Claude 继续暴露 `/v1/messages`
- 未来 Gemini 可暴露 `/v1beta/models/{model}:generateContent` 或兼容路径

Handler 只做：

- HTTP method/path 检查
- body decode
- stream/non-stream 标记
- 调用统一 pipeline
- 按客户端协议写回响应

### 2. Inbound Adapter 层

把客户端请求转成 `compatir.Request`。

首批 adapters：

- `openai_chat_inbound`: OpenAI Chat Completions -> IR
- `openai_responses_inbound`: OpenAI Responses -> IR
- `anthropic_messages_inbound`: Anthropic Messages -> IR
- `gemini_generate_content_inbound`: Gemini GenerateContent -> IR

### 3. Compat IR 层

建议新建 `internal/compatir`，只放协议中立的数据结构，不放 HTTP 逻辑。

核心结构草案：

```go
type Request struct {
    Model       string
    Messages    []Message
    Tools       []Tool
    ToolChoice  ToolChoice
    Generation  GenerationConfig
    Stream      bool
    Metadata    map[string]any
    Source      Protocol
}

type Message struct {
    Role    Role
    Content []ContentBlock
}

type ContentBlock struct {
    Type       BlockType
    Text       string
    Reasoning  *ReasoningBlock
    ToolCall   *ToolCall
    ToolResult *ToolResult
    Image      *ImageBlock
    Document   *DocumentBlock
    Raw        map[string]any
}

type ReasoningBlock struct {
    Text       string
    Signature  string
    Visibility ReasoningVisibility
    ProviderFields map[string]any
}

type Response struct {
    ID           string
    Model        string
    Output       []ContentBlock
    StopReason   StopReason
    Usage        Usage
    Raw          map[string]any
}

type StreamEvent struct {
    Type       StreamEventType
    Index      int
    Delta      ContentBlock
    Usage      *Usage
    StopReason StopReason
    Raw        map[string]any
}
```

关键要求：

- `reasoning_content`、Anthropic `thinking`、Gemini thinking/signature 都进入 `ReasoningBlock`。
- 工具调用和工具结果必须保留稳定 ID。
- 不把 reasoning 混进普通 text，除非特定客户端协议明确需要这样降级。
- `Raw` 只用于临时保底，不作为长期逻辑依赖。

### 4. Policy/Middleware 层

新建 `internal/compat/policy`。

首批策略：

- `ReasoningPolicy`: 按目标提供商决定 preserve/strip/rename/require-empty。
- `ToolPolicy`: 校验 tool_call/tool_result 顺序和 ID。
- `CapabilityPolicy`: 根据目标能力降级，比如不支持 Responses、Gemini 原生工具、Anthropic native tool。
- `RetryPolicy`: 替代当前 Codex minimal/ultra-minimal 直接写死逻辑。

MiMo/DeepSeek 规则应放在 policy，不再散落在 Codex/Claude translator：

```text
if target requires reasoning echo and assistant has tool_calls:
  if IR has reasoning for that assistant/tool_call:
    output target reasoning field
  else:
    output empty reasoning field if provider requires field presence
```

### 5. Target Adapter 层

把 IR 转成目标提供商请求，再把目标响应转回 IR。

首批目标：

- `target/openai_chat`: IR <-> OpenAI Chat Completions
- `target/openai_responses`: IR <-> OpenAI Responses
- `target/anthropic`: IR <-> Anthropic Messages
- `target/gemini`: IR <-> Gemini GenerateContent

短期 Spark 可以只让目标走 `openai_chat`，先把现有行为迁进 IR；再逐步支持 Anthropic/Gemini 目标。

### 6. Client Response Adapter 层

把 IR 响应写回客户端协议：

- Codex: IR -> Responses JSON/SSE
- Claude: IR -> Anthropic Messages JSON/SSE
- OpenAI SDK: IR -> Chat Completions JSON/SSE
- Gemini SDK: IR -> GenerateContent JSON/SSE

这里要严格保持各客户端的事件顺序：

- Codex Responses: `response.created` -> item events -> `response.completed`
- Anthropic: `message_start` -> `content_block_*` -> `message_delta` -> `message_stop`
- OpenAI Chat: `chat.completion.chunk`
- Gemini: candidate/content part delta

## 目录建议

```text
internal/compatir/
  model.go
  stream.go
  usage.go
  errors.go

internal/compat/pipeline/
  pipeline.go
  executor.go
  retry.go
  stream_aggregate.go

internal/compat/policy/
  reasoning.go
  tools.go
  capabilities.go

internal/compat/codec/openai/
  chat_inbound.go
  chat_client_writer.go
  responses_inbound.go
  responses_client_writer.go

internal/compat/codec/anthropic/
  messages_inbound.go
  messages_client_writer.go

internal/compat/codec/gemini/
  generate_content_inbound.go
  generate_content_client_writer.go

internal/compat/target/openai/
  chat_outbound.go
  chat_response.go
  chat_stream.go

internal/compat/target/anthropic/
  messages_outbound.go
  messages_response.go
  messages_stream.go

internal/compat/target/gemini/
  generate_content_outbound.go
  generate_content_response.go
  generate_content_stream.go
```

当前 `internal/integrations` 只保留 agent 启动、配置文件修改、本地代理启动。协议转换逐步搬到 `internal/compat*`。

## 分阶段计划

### Phase 0: 冻结现有行为

目标：先把当前行为固定住，防止重构时悄悄改协议。

任务：

- 给 Codex Responses 非流式/流式添加 golden fixture。
- 给 Claude Messages 非流式/流式添加 golden fixture。
- 添加 MiMo reasoning echo fixture：
  - assistant + tool_calls + reasoning_content
  - assistant + tool_calls + missing reasoning_content
  - reasoning delta + tool_call delta
- 记录现有日志字段，后续保留关键排障日志。

验收：

- `go test ./...`
- golden 文件变更必须人工确认。

### Phase 1: 引入 IR 类型，不接入生产路径

目标：新增 `internal/compatir`，不改现有代理。

任务：

- 定义 `Request`、`Message`、`ContentBlock`、`ToolCall`、`ToolResult`、`ReasoningBlock`、`Response`、`StreamEvent`、`Usage`。
- 写 `compatir` 单元测试，覆盖工具调用 ID、reasoning block、usage merge。

验收：

- 没有现有行为变更。
- `go test ./internal/compatir ./internal/integrations`

### Phase 2: OpenAI Chat target adapter

目标：先把当前共同目标 OpenAI Chat Completions IR 化。

任务：

- 实现 `target/openai.ChatOutbound`：IR -> Chat request。
- 实现 `target/openai.ChatResponse`：Chat response/stream -> IR。
- 把 `reasoning_content`、`reasoning`、tool_calls、usage 都映射进 IR。
- provider-specific reasoning 字段通过 `ReasoningPolicy` 控制。

验收：

- 单测覆盖 DeepSeek/MiMo reasoning_content。
- 单测覆盖普通 OpenAI 目标时 strip 非标准字段。

### Phase 3: Codex Responses 迁到 IR

目标：替换 `responsesToChatCompletions` 的内部实现，但保持外部行为。

任务：

- `openai_responses_inbound`: Responses request -> IR。
- `responses_client_writer`: IR response/stream -> Responses JSON/SSE。
- 当前 Codex handler 改成：

```text
decode Responses -> Responses inbound -> IR -> policy -> OpenAI Chat target -> execute -> target response -> IR -> Responses writer
```

保留旧函数名作为 wrapper，降低调用点改动。

验收：

- 现有 Codex 测试全过。
- Codex SSE golden 顺序完全一致。
- MiMo reasoning echo fixture 通过。

### Phase 4: Claude Messages 迁到 IR

目标：替换 `anthropicToChatCompletions` 和 `chatToAnthropicMessage` 的内部实现。

任务：

- `anthropic_messages_inbound`: Anthropic request -> IR。
- `anthropic_client_writer`: IR response/stream -> Anthropic JSON/SSE。
- Claude handler 改成：

```text
decode Anthropic -> Anthropic inbound -> IR -> policy -> OpenAI Chat target -> execute -> target response -> IR -> Anthropic writer
```

验收：

- 现有 Claude 测试全过。
- `thinking` round-trip 到 `ReasoningBlock`，再按目标 policy 输出。
- tool_use/tool_result ID 不变。

### Phase 5: Gemini 输入支持

目标：支持 Gemini SDK 作为客户端输入。

任务：

- `gemini_generate_content_inbound`: Gemini contents/tools -> IR。
- `gemini_client_writer`: IR -> Gemini response。
- 映射：
  - `contents[].parts[].text` -> text block
  - `functionCall` -> tool call
  - `functionResponse` -> tool result
  - image/file data -> media block
  - thinking/signature -> reasoning block

验收：

- Gemini text-only fixture。
- Gemini functionCall/functionResponse fixture。
- Gemini multimodal fixture可先 parse，不要求所有目标都能输出。

### Phase 6: 多目标协议

目标：不再只能打 OpenAI Chat Completions。

任务：

- 加 Anthropic target adapter。
- 加 Gemini target adapter。
- profile 增加 target protocol/capabilities 配置：

```json
{
  "target_protocol": "openai_chat|openai_responses|anthropic|gemini",
  "reasoning_field": "reasoning_content|reasoning|thinking|none|auto",
  "tool_mode": "openai|anthropic|gemini|auto"
}
```

验收：

- OpenAI input -> Anthropic target。
- Anthropic input -> Gemini target。
- Gemini input -> OpenAI target。
- 不支持的能力必须明确报错或可预测降级。

### Phase 7: 删除直接互转债务

目标：清掉 N x M 直连转换。

任务：

- 标记旧 direct translator 为 deprecated。
- 移除散落的 `reasoning_content` 特判，只保留 policy。
- 更新 README 和 `docs/compat-proxy-architecture.md`。

验收：

- 新增协议只需实现 inbound/client writer 或 target adapter，不需要改其他协议。

## 流式处理策略

流式不能只靠 `[]byte -> []byte` 直转。要引入 IR stream aggregator：

```text
provider SSE chunk -> Target Stream Decoder -> IR StreamEvent -> Client Stream Writer
```

需要两个模式：

- passthrough-capable: 目标协议和客户端协议一致时尽量直通。
- normalized: 协议不一致时聚合 tool_call/reasoning/text delta，再按客户端事件顺序输出。

必须保留：

- tool call delta 的 incremental arguments。
- reasoning delta 的独立通道。
- usage final chunk。
- early EOF / malformed chunk 的日志样本。

## Reasoning 规则

统一规则放在 `policy/reasoning.go`。

输入归一：

- OpenAI/MiMo/DeepSeek: `reasoning_content` 或 `reasoning`
- Anthropic: `thinking`
- Gemini: thinking text/signature
- Responses: `reasoning.summary`

输出策略：

- MiMo/DeepSeek + assistant tool_calls: 必须输出 `reasoning_content`，没有内容也输出空字符串。
- 普通 OpenAI: 默认不输出非标准 `reasoning_content`，除非 profile 显式允许。
- Anthropic target: 输出 `thinking` block 时必须处理 signature 规则。
- Gemini target: 输出 Gemini 支持的 thinking/signature 结构；不支持时保留在 IR，不混入 text。

## 测试计划

### Unit

- 每个 inbound adapter 的 request fixture。
- 每个 target adapter 的 request fixture。
- 每个 client writer 的 response/stream fixture。
- reasoning policy matrix。
- tool call ID/order validation。

### Golden

- Codex Responses SSE。
- Claude Messages SSE。
- OpenAI Chat SSE。
- Gemini stream。

### Round Trip

这些不要求 JSON 字段完全一致，但要求语义等价：

```text
OpenAI Chat -> IR -> OpenAI Chat
Responses -> IR -> Responses
Anthropic -> IR -> Anthropic
Gemini -> IR -> Gemini
```

### Integration

- 本地 httptest provider，模拟：
  - MiMo 400 when missing reasoning_content
  - reasoning delta then tool_call delta
  - empty final usage
  - malformed SSE
  - early EOF

## 风险和处理

- 事件顺序风险：先 golden 冻结，再迁移 writer。
- reasoning 泄露风险：IR 区分 `ReasoningBlock` 和 text，默认不混入用户可见 text。
- 工具调用错配风险：`ToolPolicy` 强制 tool_result 必须有前置 tool_call。
- provider 特判膨胀风险：只允许放在 policy/capability，不允许散落在 adapters。
- 大改风险：先 wrapper 替换内部实现，不一次性改 handler。

## 推荐执行顺序

1. Phase 0 + Phase 1
2. Phase 2 OpenAI Chat target
3. Phase 3 Codex 迁移
4. Phase 4 Claude 迁移
5. Phase 5 Gemini 输入
6. Phase 6 多目标输出
7. Phase 7 清理旧路径

## Definition Of Done

- 新协议输入只需要实现 inbound adapter + client writer。
- 新目标提供商只需要实现 target adapter + capability policy。
- `reasoning_content` 类问题只在 policy 层处理。
- Codex/Claude 当前行为和测试保持兼容。
- `go test ./...` 通过。
