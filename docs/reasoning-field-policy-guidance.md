# Reasoning Field Policy Guidance

This document defines how Spark should preserve, route, and degrade reasoning-related fields across compatibility gateways.

## Core Principle

The compatibility IR should preserve reasoning semantics as fully as possible. Client codecs should parse provider-specific request shapes into IR without prematurely dropping fields. Target converters should decide how to emit, degrade, or drop those fields based on the selected upstream protocol, provider, and model policy.

The intended flow is:

```text
client request
  -> client codec parses into IR
  -> gateway selects route and target policy
  -> target converter builds upstream payload from IR + policy
  -> unsupported target fields are degraded or dropped at conversion time
```

Proxy and integration layers should not contain provider/model field policy. They should pass runtime context such as upstream base URL, preferred model, API key, and long-lived caches into gateway handlers.

## Layer Responsibilities

### Proxy / Integration Layer

Responsibilities:

- Start local compatibility servers.
- Hold upstream URL, API key, preferred model, and runtime lifecycle state.
- Forward requests into gateway handlers.
- Own long-lived cache instances only as runtime state.

Non-responsibilities:

- Deciding whether `reasoning_effort`, `thinking`, or `reasoning_content` is allowed.
- Knowing provider/model allowlists.
- Rewriting protocol fields.

### Gateway Layer

Responsibilities:

- Decode client requests and select routes.
- Convert client request maps into IR.
- Select target policy from `upstreamBase`, target protocol, and target model.
- Apply target-specific compatibility steps before dispatch.
- Coordinate stateful compatibility helpers such as reasoning cache.

### Policy Layer

Responsibilities:

- Hold provider/model capability rules.
- Decide whether a target supports request controls such as `reasoning_effort`.
- Decide whether a target requires message-level reasoning echo for tool calls.

### Target Converter Layer

Responsibilities:

- Convert IR into target protocol payloads.
- Use policy to emit or drop target-specific fields.
- Keep unsupported field degradation local to target conversion.

## Reasoning Field Categories

### `reasoning_effort`

Meaning:

- Top-level generation control.
- Represents requested reasoning intensity.

IR location:

```go
Request.Generation.Reasoning.Effort
```

OpenAI Chat output shape:

```json
{
  "reasoning_effort": "high"
}
```

Policy:

- Only emit when the OpenAI Chat target policy explicitly allows it.
- Drop for generic OpenAI-compatible upstreams such as LiteLLM/GLM unless explicitly supported.

Current OpenAI Chat policy:

```text
allow reasoning_effort when:
  upstreamBase contains "api.openai.com"
  and model starts with "o", "gpt-5", or "gpt-4.1"
```

### `thinking`

Meaning:

- Top-level thinking control.
- Often includes enablement and budget.

IR location:

```go
Request.Generation.Reasoning.Enabled
Request.Generation.Reasoning.BudgetTokens
Request.Generation.Reasoning.IncludeThoughts
```

Anthropic output shape:

```json
{
  "thinking": {
    "type": "enabled",
    "budget_tokens": 1024
  }
}
```

OpenAI Chat behavior:

- Usually drop unless the target policy explicitly allows a `thinking` object.

### `reasoning_content`

Meaning:

- Message-level reasoning/history compatibility field.
- Not a top-level generation control.
- Used by some OpenAI Chat-compatible models during multi-turn tool call flows.

IR location:

```go
Message.Content[] = ContentBlock{Type: BlockReasoning}
```

OpenAI Chat output shape:

```json
{
  "role": "assistant",
  "content": "",
  "reasoning_content": "think first",
  "tool_calls": []
}
```

Policy:

- Generic OpenAI Chat targets should strip it.
- MiMo/DeepSeek-like targets may require it for assistant tool-call messages.
- Stateful tool-call flows may need `gateway/features/reasoning.ReasoningCache` to remember reasoning associated with tool call IDs and restore it on later turns.

## Field Assembly Examples

### Anthropic Messages Input

```json
{
  "model": "glm-5.1",
  "output_config": {
    "effort": "high"
  },
  "thinking": {
    "type": "enabled",
    "budget_tokens": 1024
  },
  "messages": [
    {
      "role": "assistant",
      "content": [
        { "type": "thinking", "thinking": "think first" },
        {
          "type": "tool_use",
          "id": "call_1",
          "name": "sum",
          "input": { "a": 1, "b": 2 }
        }
      ]
    }
  ]
}
```

### IR Representation

```go
Request{
  Model: "glm-5.1",
  Generation: GenerationConfig{
    Reasoning: ReasoningConfig{
      Effort: ReasoningEffortHigh,
      Enabled: &trueValue,
      BudgetTokens: &budget,
    },
  },
  Messages: []Message{
    {
      Role: RoleAssistant,
      Content: []ContentBlock{
        Reasoning("think first"),
        ToolCall(...),
      },
    },
  },
}
```

### OpenAI Chat Target: Official Reasoning Model

For an official OpenAI target that supports `reasoning_effort`, the payload may include:

```json
{
  "model": "gpt-5",
  "messages": [
    {
      "role": "assistant",
      "content": "",
      "tool_calls": []
    }
  ],
  "reasoning_effort": "high"
}
```

### OpenAI Chat Target: Generic LiteLLM/GLM

For a generic OpenAI-compatible target, unsupported top-level controls are dropped:

```json
{
  "model": "glm-5.1",
  "messages": [
    {
      "role": "assistant",
      "content": "",
      "tool_calls": []
    }
  ]
}
```

### OpenAI Chat Target: MiMo/DeepSeek Tool Call Echo

For a target that requires message-level reasoning echo:

```json
{
  "model": "mimo-v2.5-pro",
  "messages": [
    {
      "role": "assistant",
      "content": "",
      "reasoning_content": "think first",
      "tool_calls": []
    }
  ]
}
```

This does not imply `reasoning_effort` should be emitted. Top-level controls and message-level reasoning compatibility are separate decisions.

## Policy Injection Pattern

Preferred pattern:

```go
handler := gateway.NewAnthropicMessagesToOpenAIChatHandler(gateway.AnthropicMessagesOptions{
  PreferredModel: p.preferredModel,
  UpstreamBase:   p.upstreamBase,
  ReasoningCache: &p.reasoningCache,
})
```

Then inside the gateway handler:

```go
targetModel := incomingModel
if preferredModel != "" {
  targetModel = preferredModel
}

reasoningPolicy := policy.OpenAIChatReasoningPolicy(upstreamBase, targetModel)

chatReq := openai_chat.ChatOutbound{
  Reasoning: reasoningPolicy,
}.BuildRequest(irReq)
```

Stateful message-level compatibility should also be coordinated inside the gateway handler:

```go
adapter := reasoning.ChatReasoningAdapter{
  UpstreamBase: upstreamBase,
  Cache:        reasoningCache,
}
adapter.ApplyToChatRequest(chatReq)
```

Proxy owns the cache lifetime, but gateway owns the decision to apply it.

## Current Implementation Review

### Aligned

- IR has separate request controls and message reasoning:
  - `GenerationConfig.Reasoning`
  - `ContentBlock{Type: BlockReasoning}`
- Client codecs preserve reasoning controls:
  - OpenAI Responses `reasoning.effort`
  - Anthropic `thinking` and `output_config.effort`
  - Gemini `thinkingConfig`
- OpenAI Chat target emits top-level reasoning controls through `policy.ReasoningPolicy`.
- OpenAI Chat target emits message-level reasoning through `policy.ReasoningPolicy.ChatReasoningContent`.
- Codex `reasoning_effort` policy selection is in gateway code via the handler returned by `NewCodexResponsesHandler`.
- Claude/Anthropic Messages now selects OpenAI Chat reasoning policy in the handler returned by `NewAnthropicMessagesToOpenAIChatHandler` from `UpstreamBase` and target model.
- `reasoning_content` apply/remember orchestration is now inside gateway handlers:
  - The Anthropic Messages handler owns `gateway/features/reasoning.ChatReasoningAdapter` construction from `UpstreamBase` and `ReasoningCache`.
  - The Codex Responses handler applies `gateway/features/reasoning.ChatReasoningAdapter` when routing Codex Responses to OpenAI Chat.
- Proxy wiring passes runtime context/cache only; it no longer injects reasoning callbacks.
- Reasoning echo target detection now delegates to `policy.RequiresOpenAIChatReasoningEcho`.

### Differences From Desired Boundary

1. Some tests still use broad `PreserveReasoningContent` bridge paths.

This can be acceptable for pure adapter unit tests, but production gateway paths should always pass a target policy when targeting OpenAI Chat.

2. Compatibility cache is gateway-typed but proxy-owned.

This is acceptable for lifecycle ownership. The current handler API now exposes cache/context fields instead of reasoning callback hooks.

## Recommended Cleanup Plan

1. Keep proxy layer limited to `UpstreamBase`, `PreferredModel`, executor functions, logging, and cache lifetime.
2. Add regression tests for:
   - LiteLLM/GLM drops `reasoning_effort`.
   - MiMo/DeepSeek keeps or restores `reasoning_content` for assistant tool-call messages.
   - Generic OpenAI-compatible target strips message-level `reasoning_content`.
