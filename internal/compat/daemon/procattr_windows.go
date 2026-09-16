//go:build windows

package daemon

import (
	"os/exec"
)

func setDaemonProcAttr(cmd *exec.Cmd) {
	// On Windows, Setsid is not supported in syscall.SysProcAttr.
}
