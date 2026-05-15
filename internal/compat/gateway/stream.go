package gateway

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"strings"

	"spark/internal/compat/client/codex"
	openai_chat_target "spark/internal/compat/target/openai_chat"
)

func WriteCodexResponsesStreamFromOpenAIChat(writer io.Writer, reader io.Reader, flush func()) codex.ResponsesStreamResult {
	out := codex.NewResponsesStreamWriter(writer, flush)
	scanner := bufio.NewScanner(reader)
	scanner.Buffer(make([]byte, 0, 64*1024), 2*1024*1024)

	rawJSONLines := make([]string, 0, 1)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		data := ""
		if strings.HasPrefix(line, "data:") {
			data = strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		} else if strings.HasPrefix(line, "{") {
			data = line
			rawJSONLines = append(rawJSONLines, line)
		} else {
			continue
		}
		if data == "" {
			continue
		}
		if data == "[DONE]" {
			out.MarkDone()
			break
		}

		var chunk map[string]any
		if err := json.Unmarshal([]byte(data), &chunk); err != nil {
			if result := out.WriteScanError(fmt.Errorf("malformed upstream stream chunk: %w", err)); result.HandledError {
				return result
			}
			continue
		}
		out.ObserveUpstreamChunk(chunk, data)

		hadTextDelta := false
		for _, event := range openai_chat_target.ChatStreamEvents(chunk) {
			if out.WriteEvent(event) {
				hadTextDelta = true
			}
		}
		if !hadTextDelta {
			out.WriteResponseFallback(openai_chat_target.ChatResponse(chunk))
		}
	}

	if len(rawJSONLines) == 1 {
		var full map[string]any
		if err := json.Unmarshal([]byte(rawJSONLines[0]), &full); err == nil {
			out.WriteResponseFallback(openai_chat_target.ChatResponse(full))
		}
	}

	if result := out.WriteScanError(scanner.Err()); result.HandledError {
		return result
	}
	return out.Finish()
}
