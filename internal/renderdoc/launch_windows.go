//go:build windows

package renderdoc

import (
	"os/exec"
	"syscall"
)

func configureLaunchSysProcAttr(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
}
