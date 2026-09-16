package proxy

import (
	"io"

	"spark/internal/compat/proxyutil"
)

func openGeminiCompatLogFile() (io.WriteCloser, string, error) {
	return proxyutil.OpenProxyLogFile("AGENT_LAUNCH_GEMINI_COMPAT_LOG", "gemini-compat.log", "gemini-compat")
}
