package integrations

import "io"

func openCompatLogFile() (io.WriteCloser, string, error) {
	return openProxyLogFile("AGENT_LAUNCH_COMPAT_LOG", "codex-compat.log", "compat")
}
