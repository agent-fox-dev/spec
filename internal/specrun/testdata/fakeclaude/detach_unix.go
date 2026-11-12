//go:build unix

package main

import (
	"os/exec"
	"syscall"
)

// detach puts the child in a process group of its own when asked, as a
// program that calls setsid would be.
func detach(cmd *exec.Cmd, apart bool) {
	if apart {
		cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	}
}
