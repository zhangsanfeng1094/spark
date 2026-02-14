package integrations

import (
	"context"
	"fmt"
	"net/http"
)

type pipelineStage string

const (
	pipelineStageTranslate pipelineStage = "translate"
	pipelineStageExecute   pipelineStage = "execute"
)

type pipelineError struct {
	stage pipelineStage
	err   error
}

func (e pipelineError) Error() string {
	return fmt.Sprintf("%s: %v", e.stage, e.err)
}

func (e pipelineError) Unwrap() error { return e.err }

func executeTranslatedChat(
	ctx context.Context,
	req map[string]any,
	translator RequestTranslator,
	executor ChatExecutor,
) (map[string]any, *http.Response, error) {
	chatReq, err := translator.ToChat(req)
	if err != nil {
		return nil, nil, pipelineError{stage: pipelineStageTranslate, err: err}
	}
	resp, err := executor.Do(ctx, chatReq)
	if err != nil {
		return nil, nil, pipelineError{stage: pipelineStageExecute, err: err}
	}
	return chatReq, resp, nil
}
