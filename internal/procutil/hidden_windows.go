//go:build windows

package procutil

import (
	"os/exec"
	"syscall"
)

// createNoWindow corresponds to the Windows CREATE_NO_WINDOW process creation flag.
const createNoWindow = 0x08000000

// HideWindow prevents a console window from being allocated for cmd when the
// parent is a GUI-subsystem binary (built with -H windowsgui). Without this,
// each subprocess pops a cmd.exe window.
func HideWindow(cmd *exec.Cmd) {
	if cmd == nil {
		return
	}
	if cmd.SysProcAttr == nil {
		cmd.SysProcAttr = &syscall.SysProcAttr{}
	}
	cmd.SysProcAttr.HideWindow = true
	cmd.SysProcAttr.CreationFlags |= createNoWindow
}
