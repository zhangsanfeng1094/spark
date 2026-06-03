# Compatibility Gateway Architecture

This gateway is a protocol conversion service. A client sends one protocol,
Spark converts it into an internal representation, sends it to the selected
upstream API type, then converts the upstream result back to the client
protocol.

## Core Flow

For Codex using an OpenAI Chat Completions upstream:

```text
Codex Responses request
  -> client/codex request input
  -> compat/ir Request
  -> target/openai_chat request output
  -> configured upstream endpoint
  -> target/openai_chat stream or response input
  -> compat/ir StreamEvent or Response
  -> client/codex stream or response output
  -> Codex Responses SSE or JSON
```

Streaming is the primary path. Non-streaming uses the same IR, but converts a
complete upstream response into a complete client response.

## Codex Trace Examples

Stream mode:

```text
POST /v1/responses {"model":"mimo-v2.5-pro","input":"hi","stream":true}
  -> client/codex.ResponsesInbound
  -> ir.Request{Source: codex_responses, Stream: true}
  -> target/openai_chat.ChatOutbound
  -> POST upstream /chat/completions {"model":"mimo-v2.5-pro","messages":[...],"stream":true}
  <- data: {"choices":[{"delta":{"reasoning_content":"think "}}]}
  <- data: {"choices":[{"delta":{"content":"Hi"}}]}
  -> target/openai_chat.ChatStreamEvents
  -> client/codex emits response.created, reasoning delta, text delta, response.completed
```

Non-stream mode:

```text
POST /v1/responses {"model":"gpt-4.1","input":"hi","stream":false}
  -> client/codex.ResponsesInbound
  -> ir.Request{Source: codex_responses, Stream: false}
  -> target/openai_chat.ChatOutbound
  -> POST upstream /chat/completions {"model":"gpt-4.1","messages":[...],"stream":false}
  <- {"choices":[{"message":{"role":"assistant","content":"Hi"}}],"usage":{...}}
  -> target/openai_chat.ChatResponse
  -> client/codex.ResponsesClientResponse
  <- {"object":"response","output":[{"type":"message","content":[{"type":"output_text","text":"Hi"}]}]}
```

## Directory Model

```text
internal/compat/
  ir/
    model.go
    reasoning.go
    stream.go
    usage.go
    Internal request, response, stream, content, tool, usage, and reasoning
    types. IR expresses gateway semantics, not provider field names.

  client/
    codex/
      request_in.go
      response_out.go
      stream_out.go
      Codex/OpenAI Responses client protocol adapters.

    anthropic_messages/
      messages_inbound.go
      messages_client_writer.go
      messages_stream_writer.go
      Anthropic Messages caller protocol adapters.

    gemini_generate_content/
      generate_content_inbound.go
      generate_content_client_writer.go
      Gemini GenerateContent caller protocol adapters.

  target/
    openai_chat/
      request_out.go
      response_in.go
      stream_in.go
      extractors.go
      OpenAI Chat Completions upstream target adapters.

  gateway/
    core/
      route.go
      pipeline.go
      Route keys, route normalization, unsupported-route errors, and the
      translated-chat execution pipeline.
    features/
      reasoning/
        reasoning_cache.go
        Tool-call reasoning echo cache and request adapter.
    bridge/
      registry.go
      types.go
      translators.go
      Route selection plus client/target codec composition. This layer binds
      client/* adapters to target/* adapters through IR; it should not grow
      one full mapper for every client-target pair.
    codex_responses_handler.go
    anthropic_messages_handler.go
    HTTP routing, concrete route registration, stream/non-stream forwarding,
    and orchestration only. No protocol field mapping.
```

## Naming Rules

- `client/*`: protocol used by the caller, such as Codex Responses.
- `target/*`: API type used by the configured upstream endpoint.
- `ir/*`: provider-neutral internal representation.
- `gateway/*`: HTTP and routing orchestration.
- `*_in.go`: external protocol to IR.
- `*_out.go`: IR to external protocol.

## Responsibilities

Client adapters:
- Parse caller request JSON into `ir.Request`.
- Write `ir.Response` or `ir.StreamEvent` back in the caller protocol.
- Do not know the upstream endpoint.

Target adapters:
- Build upstream request JSON from `ir.Request`.
- Parse upstream non-stream JSON into `ir.Response`.
- Parse upstream stream chunks into `ir.StreamEvent`.
- Own target-specific fallback extraction helpers used by retry and diagnostics.
- Do not know which client called the gateway.

Gateway:
- Detect or receive the client protocol.
- Select the target protocol from profile/API type.
- Use protocol bridge selections from `gateway/bridge`.
- Execute the upstream request.
- For stream, convert upstream chunks to IR events and immediately write client
  events.
- For non-stream, convert upstream JSON to IR response and then client JSON.

Gateway bridge:
- Compose `client/*` inbound adapters with `target/*` outbound adapters.
- Compose target response adapters with client response writers for non-stream
  responses.
- Compose target stream parsers with client stream writers for route streams.
- Implement `gateway/core.RequestTranslator`.
- Do not import the gateway root package or own HTTP behavior.
- Do not model conversion as `N clients * M targets` pairwise field mappers.

IR:
- Owns common concepts: messages, content blocks, tools, tool results,
  generation controls, reasoning controls, usage, stop reason, and stream
  events.
- `Raw` fields are fallback storage only. Core behavior must use typed IR
  fields.

## Reasoning Controls

Request-side reasoning controls belong in `ir.GenerationConfig.Reasoning`.
Provider-specific fields map into this structure:

- OpenAI Responses `reasoning.effort` -> `Reasoning.Effort`
- Anthropic `thinking.type` -> `Reasoning.Enabled`
- Anthropic `thinking.budget_tokens` -> `Reasoning.BudgetTokens`
- Anthropic `output_config.effort` -> `Reasoning.Effort`
- Gemini `generationConfig.thinkingConfig.includeThoughts` -> `Reasoning.IncludeThoughts`
- Gemini `generationConfig.thinkingConfig.thinkingBudget` -> `Reasoning.BudgetTokens`

Target adapters decide what can be written to the selected upstream. Unsupported
controls are dropped or degraded by target policy, not by client adapters.
