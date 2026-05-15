package ir

type StreamEvent struct {
	Type       StreamEventType
	Index      int
	Delta      ContentBlock
	Usage      *Usage
	StopReason StopReason
	Raw        map[string]any
}

type StreamEventType string

const (
	StreamEventResponseStart StreamEventType = "response_start"
	StreamEventContentDelta  StreamEventType = "content_delta"
	StreamEventContentDone   StreamEventType = "content_done"
	StreamEventUsage         StreamEventType = "usage"
	StreamEventResponseDone  StreamEventType = "response_done"
)
