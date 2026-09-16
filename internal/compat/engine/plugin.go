package engine

import (
	"github.com/maximhq/bifrost/core/schemas"
)

// SparkLLMPlugin implements schemas.LLMPlugin for logging and request inspection.
type SparkLLMPlugin struct {
	logf func(format string, args ...any)
}

func NewSparkLLMPlugin(logf func(format string, args ...any)) *SparkLLMPlugin {
	return &SparkLLMPlugin{
		logf: logf,
	}
}

func (p *SparkLLMPlugin) GetName() string {
	return "spark-llm-plugin"
}

func (p *SparkLLMPlugin) Cleanup() error {
	return nil
}

func (p *SparkLLMPlugin) PreRequestHook(ctx *schemas.BifrostContext, req *schemas.BifrostRequest) error {
	if p.logf != nil && req != nil {
		provider, model, _ := req.GetRequestFields()
		p.logf("[Bifrost PreRequestHook] type=%v provider=%v model=%v", req.RequestType, provider, model)
	}
	return nil
}

func (p *SparkLLMPlugin) PreLLMHook(ctx *schemas.BifrostContext, req *schemas.BifrostRequest) (*schemas.BifrostRequest, *schemas.LLMPluginShortCircuit, error) {
	return req, nil, nil
}

func (p *SparkLLMPlugin) PostLLMHook(ctx *schemas.BifrostContext, resp *schemas.BifrostResponse, bifrostErr *schemas.BifrostError) (*schemas.BifrostResponse, *schemas.BifrostError, error) {
	if p.logf != nil {
		if bifrostErr != nil {
			p.logf("[Bifrost PostLLMHook] error: %s", bifrostErr.GetErrorString())
		} else if resp != nil && resp.ChatResponse != nil && resp.ChatResponse.Usage != nil {
			p.logf("[Bifrost PostLLMHook] usage: total_tokens=%d", resp.ChatResponse.Usage.TotalTokens)
		}
	}
	return resp, bifrostErr, nil
}
