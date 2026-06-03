package bridge

import (
	"time"

	"spark/internal/usage"
)

func ClientResponseFromTargetResponse(client ClientCodec, target TargetCodec, resp map[string]any) map[string]any {
	irResp := target.ResponseInbound(resp)
	usage.RecordIR(irResp.Usage, irResp.Model, false, time.Now().UTC())
	return client.ResponseOutbound(irResp)
}
