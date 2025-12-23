//go:build !windows

package process

import "os/exec"

func applyProcessAttributes(_ *exec.Cmd) {}
