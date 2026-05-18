package compatproxy

import (
	"io"

	"spark/internal/integrations/proxyutil"
)

func openCompatLogFile() (io.WriteCloser, string, error) {
	return proxyutil.OpenProxyLogFile("AGENT_LAUNCH_COMPAT_LOG", "codex-compat.log", "compat")
}
