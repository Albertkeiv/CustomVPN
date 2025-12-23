//go:build windows

package app

import (
	"os/exec"
	"syscall"
)

func applyCommandAttributes(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
}
