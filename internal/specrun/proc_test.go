//go:build unix

package specrun

import (
	"errors"
	"syscall"
)

// processAlive reports whether a process with the id runs.
func processAlive(pid int) bool { return !errors.Is(syscall.Kill(pid, 0), syscall.ESRCH) }
