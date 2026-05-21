package proxy

import (
	"io"

	"spark/internal/compat/proxyutil"
)

func openCompatLogFile() (io.WriteCloser, string, error) {
	return proxyutil.OpenProxyLogFile("AGENT_LAUNCH_COMPAT_LOG", "codex-compat.log", "compat")
}
