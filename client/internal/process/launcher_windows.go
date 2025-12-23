//go:build windows

package process

import (
	"os/exec"
	"syscall"
)

func applyProcessAttributes(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
}
