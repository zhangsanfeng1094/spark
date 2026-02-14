package integrations

import "io"

func openAnthropicCompatLogFile() (io.WriteCloser, string, error) {
	return openProxyLogFile("AGENT_LAUNCH_ANTHROPIC_COMPAT_LOG", "anthropic-compat.log", "anthropic-compat")
}
