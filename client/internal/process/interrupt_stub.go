//go:build !windows

package process

import (
	"os"
	"os/exec"
)

func sendInterrupt(cmd *exec.Cmd) error {
	if cmd == nil || cmd.Process == nil {
		return nil
	}
	return cmd.Process.Signal(os.Interrupt)
}
