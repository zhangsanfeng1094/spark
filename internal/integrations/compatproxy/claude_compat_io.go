package compatproxy

import (
	"io"

	"spark/internal/integrations/proxyutil"
)

func openAnthropicCompatLogFile() (io.WriteCloser, string, error) {
	return proxyutil.OpenProxyLogFile("AGENT_LAUNCH_ANTHROPIC_COMPAT_LOG", "anthropic-compat.log", "anthropic-compat")
}
