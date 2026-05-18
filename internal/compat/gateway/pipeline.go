package gateway

import (
	"context"
	"fmt"
	"io"
	"net/http"

	"spark/internal/compat/client/codex"
)

type RequestTranslator interface {
	ToChat(req map[string]any) (map[string]any, error)
}

type ChatExecutor interface {
	Do(ctx context.Context, chatReq map[string]any) (*http.Response, error)
}

type ChatRequestPreparer func(chatReq map[string]any)

type StreamWriter func(writer io.Writer, reader io.Reader, flush func()) codex.ResponsesStreamResult

type NonStreamWriter func(resp map[string]any) map[string]any

type PipelineStage string

const (
	PipelineStageTranslate PipelineStage = "translate"
	PipelineStageExecute   PipelineStage = "execute"
)

type PipelineError struct {
	Stage PipelineStage
	Err   error
}

func (e PipelineError) Error() string {
	return fmt.Sprintf("%s: %v", e.Stage, e.Err)
}

func (e PipelineError) Unwrap() error { return e.Err }

func ExecuteTranslatedChat(
	ctx context.Context,
	req map[string]any,
	translator RequestTranslator,
	executor ChatExecutor,
	preparers ...ChatRequestPreparer,
) (map[string]any, *http.Response, error) {
	chatReq, err := translator.ToChat(req)
	if err != nil {
		return nil, nil, PipelineError{Stage: PipelineStageTranslate, Err: err}
	}
	for _, prepare := range preparers {
		if prepare != nil {
			prepare(chatReq)
		}
	}
	resp, err := executor.Do(ctx, chatReq)
	if err != nil {
		return nil, nil, PipelineError{Stage: PipelineStageExecute, Err: err}
	}
	return chatReq, resp, nil
}
