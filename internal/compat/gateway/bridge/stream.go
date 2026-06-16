package bridge

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"strings"

	"spark/internal/compat/client/codex"
	"spark/internal/compat/ir"
)

func WriteClientStreamFromTarget(client ClientCodec, target TargetCodec, writer io.Writer, reader io.Reader, flush func()) StreamResult {
	out := client.NewStreamWriter(writer, flush)
	scanner := bufio.NewScanner(reader)
	scanner.Buffer(make([]byte, 0, 64*1024), 2*1024*1024)

	var eventName string
	rawJSONLines := make([]string, 0, 1)
	usedRawJSONFallback := false
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		if strings.HasPrefix(line, "event:") {
			eventName = strings.TrimSpace(strings.TrimPrefix(line, "event:"))
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
			continue
		}

		var chunk map[string]any
		if err := json.Unmarshal([]byte(data), &chunk); err != nil {
			if result := out.WriteScanError(fmt.Errorf("malformed upstream stream chunk: %w", err)); result.HandledError {
				return result
			}
			continue
		}
		if target.PrepareStreamChunk != nil {
			target.PrepareStreamChunk(chunk, eventName)
		}
		out.ObserveUpstreamChunk(chunk, data)

		handledEvent := false
		for _, event := range target.StreamEvents(chunk) {
			if out.WriteEvent(event) {
				handledEvent = true
			}
		}
		if !handledEvent {
			out.WriteResponseFallback(target.ResponseInbound(chunk))
			if len(rawJSONLines) == 1 && rawJSONLines[0] == line {
				usedRawJSONFallback = true
			}
		}
	}

	if len(rawJSONLines) == 1 && !usedRawJSONFallback {
		var full map[string]any
		if err := json.Unmarshal([]byte(rawJSONLines[0]), &full); err == nil {
			out.WriteResponseFallback(target.ResponseInbound(full))
		}
	}

	if result := out.WriteScanError(scanner.Err()); result.HandledError {
		return result
	}
	return out.Finish()
}

func newCodexResponsesStreamWriter(writer io.Writer, flush func()) ClientStreamWriter {
	return codexResponsesStreamWriter{writer: codex.NewResponsesStreamWriter(writer, flush)}
}

type codexResponsesStreamWriter struct {
	writer *codex.ResponsesStreamWriter
}

func (w codexResponsesStreamWriter) ObserveUpstreamChunk(raw map[string]any, sample string) {
	w.writer.ObserveUpstreamChunk(raw, sample)
}

func (w codexResponsesStreamWriter) WriteEvent(event ir.StreamEvent) bool {
	return w.writer.WriteEvent(event)
}

func (w codexResponsesStreamWriter) WriteResponseFallback(resp ir.Response) {
	w.writer.WriteResponseFallback(resp)
}

func (w codexResponsesStreamWriter) MarkDone() {
	w.writer.MarkDone()
}

func (w codexResponsesStreamWriter) WriteScanError(err error) StreamResult {
	return streamResultFromCodex(w.writer.WriteScanError(err))
}

func (w codexResponsesStreamWriter) Finish() StreamResult {
	return streamResultFromCodex(w.writer.Finish())
}

func prepareAnthropicMessageStreamChunk(chunk map[string]any, eventName string) {
	if chunk["event"] == nil && eventName != "" {
		chunk["event"] = eventName
	}
}

func streamResultFromCodex(result codex.ResponsesStreamResult) StreamResult {
	return StreamResult{
		ChunkCount:       result.ChunkCount,
		ExtractedTextLen: result.ExtractedTextLen,
		ChunkSamples:     result.ChunkSamples,
		SawDone:          result.SawDone,
		SawContentDelta:  result.SawContentDelta,
		ReasoningLen:     result.ReasoningLen,
		ReasoningSamples: result.ReasoningSamples,
		FirstValidChunk:  result.FirstValidChunk,
		LastValidChunk:   result.LastValidChunk,
		Usage:            result.Usage,
		ResponseID:       result.ResponseID,
		Model:            result.Model,
		ScanErr:          result.ScanErr,
		HandledError:     result.HandledError,
	}
}
