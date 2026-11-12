//go:build unix

package specrun

import (
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
)

// isSpecrun reports whether a process with the id runs and is a specrun (its
// tests run as specrun.test): an id recorded before a reboot may belong to
// another program now.
func isSpecrun(pid int) bool {
	if syscall.Kill(pid, 0) != nil {
		return false
	}
	out, err := exec.Command("ps", "-p", strconv.Itoa(pid), "-o", "comm=").Output()
	return err == nil && strings.HasPrefix(filepath.Base(strings.TrimSpace(string(out))), tool)
}
