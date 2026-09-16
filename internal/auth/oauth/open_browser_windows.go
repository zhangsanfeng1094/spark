//go:build windows

package oauth

import (
	"os/exec"
)

// OpenBrowser opens rawURL in the default Windows browser.
func OpenBrowser(rawURL string) error {
	return openURL(rawURL)
}

// openURL opens rawURL in the default Windows browser.
func openURL(rawURL string) error {
	return exec.Command("rundll32", "url.dll,FileProtocolHandler", rawURL).Start()
}