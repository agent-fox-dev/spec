//go:build unix

package specrun

import (
	"os/exec"
	"syscall"
)

// isolate starts the session in a process group of its own, so that ending it
// ends every process it started.
func isolate(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error { return endGroup(cmd) }
}

// endGroup ends every process in the session's group.
func endGroup(cmd *exec.Cmd) error {
	if cmd.Process == nil {
		return nil
	}
	return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
}
