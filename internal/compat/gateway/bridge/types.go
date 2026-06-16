package bridge

import (
	"io"

	"spark/internal/compat/gateway/core"
	"spark/internal/compat/ir"
)

type StreamResult struct {
	ChunkCount       int
	ExtractedTextLen int
	ChunkSamples     []string
	SawDone          bool
	SawContentDelta  bool
	ReasoningLen     int
	ReasoningSamples []string
	FirstValidChunk  string
	LastValidChunk   string
	Usage            map[string]any
	ResponseID       string
	Model            string
	ScanErr          error
	HandledError     bool
}

type StreamWriter func(writer io.Writer, reader io.Reader, flush func()) StreamResult

type NonStreamWriter func(resp map[string]any) map[string]any

type ClientStreamWriter interface {
	ObserveUpstreamChunk(raw map[string]any, sample string)
	WriteEvent(event ir.StreamEvent) bool
	WriteResponseFallback(resp ir.Response)
	MarkDone()
	WriteScanError(err error) StreamResult
	Finish() StreamResult
}

type ClientCodec struct {
	Protocol         core.ClientProtocol
	RequestInbound   func(map[string]any) ir.Request
	ResponseOutbound func(ir.Response) map[string]any
	NewStreamWriter  func(io.Writer, func()) ClientStreamWriter
}

type TargetCodec struct {
	Protocol           core.TargetProtocol
	RequestOutbound    func(ir.Request) map[string]any
	ResponseInbound    func(map[string]any) ir.Response
	StreamEvents       func(map[string]any) []ir.StreamEvent
	PrepareStreamChunk func(map[string]any, string)
}
