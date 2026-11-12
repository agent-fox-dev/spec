//go:build unix

package specrun

import (
	"errors"
	"syscall"
)

// processAlive reports whether a process with the id runs.
func processAlive(pid int) bool {
	err := syscall.Kill(pid, 0)
	return err == nil || errors.Is(err, syscall.EPERM)
}
