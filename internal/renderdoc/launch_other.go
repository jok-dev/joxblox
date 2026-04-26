//go:build !windows

package renderdoc

import "os/exec"

func configureLaunchSysProcAttr(cmd *exec.Cmd) {
	// no-op on non-Windows
	_ = cmd
}
