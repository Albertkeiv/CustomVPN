//go:build windows

package process

import (
	"os/exec"

	"golang.org/x/sys/windows"
)

func sendInterrupt(cmd *exec.Cmd) error {
	if cmd == nil || cmd.Process == nil {
		return nil
	}
	return windows.GenerateConsoleCtrlEvent(windows.CTRL_BREAK_EVENT, uint32(cmd.Process.Pid))
}
