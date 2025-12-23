//go:build !windows

package app

import "os/exec"

func applyCommandAttributes(_ *exec.Cmd) {}
