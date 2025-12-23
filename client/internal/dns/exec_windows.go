//go:build windows

package dns

import (
	"os/exec"
	"syscall"
)

func applyCommandAttributes(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
}
