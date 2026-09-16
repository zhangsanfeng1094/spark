//go:build !windows

package oauth

import (
	"fmt"
	"os/exec"
	"runtime"
)

// OpenBrowser opens rawURL in the system default browser.
func OpenBrowser(rawURL string) error {
	return openURL(rawURL)
}

// openURL opens rawURL in the system default browser.
func openURL(rawURL string) error {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("open", rawURL)
	case "linux":
		cmd = exec.Command("xdg-open", rawURL)
	default:
		return fmt.Errorf("auth: cannot open browser on %s", runtime.GOOS)
	}
	return cmd.Start()
}