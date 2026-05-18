package integrations

import "spark/internal/integrations/proxyutil"

func appendLaunchRouteLog(line string) string {
	return proxyutil.AppendLaunchRouteLog(line)
}

func mustJSONForLog(v any) string {
	return proxyutil.MustJSONForLog(v)
}
