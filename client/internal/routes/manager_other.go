//go:build !windows

package routes

import "os/exec"

func applyRouteCommandAttributes(_ *exec.Cmd) {}
