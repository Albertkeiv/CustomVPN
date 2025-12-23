//go:build windows

package routes

import (
	"os/exec"
	"syscall"
)

func applyRouteCommandAttributes(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
}
