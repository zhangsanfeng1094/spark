package core

import (
	"context"
	"fmt"
	"net/http"
)

type RequestTranslator interface {
	Translate(req map[string]any) (map[string]any, error)
}

type Executor interface {
	Do(ctx context.Context, req map[string]any) (*http.Response, error)
}

type RequestPreparer func(req map[string]any)

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

func ExecuteTranslated(
	ctx context.Context,
	req map[string]any,
	translator RequestTranslator,
	executor Executor,
	preparers ...RequestPreparer,
) (map[string]any, *http.Response, error) {
	upstreamReq, err := translator.Translate(req)
	if err != nil {
		return nil, nil, PipelineError{Stage: PipelineStageTranslate, Err: err}
	}
	for _, prepare := range preparers {
		if prepare != nil {
			prepare(upstreamReq)
		}
	}
	resp, err := executor.Do(ctx, upstreamReq)
	if err != nil {
		return nil, nil, PipelineError{Stage: PipelineStageExecute, Err: err}
	}
	return upstreamReq, resp, nil
}
