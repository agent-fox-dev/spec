//go:build unix

package gate

import (
	"os/exec"
	"syscall"
)

// isolate starts the command in a process group of its own, and makes ending
// it end the group: a test runner's children die with it at the timeout.
func isolate(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error { return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL) }
}
