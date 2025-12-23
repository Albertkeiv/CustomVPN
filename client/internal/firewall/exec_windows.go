//go:build windows

package firewall

import (
	"os/exec"
	"syscall"
)

func applyCommandAttributes(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
}
