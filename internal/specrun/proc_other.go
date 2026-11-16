//go:build !unix

package specrun

import "os/exec"

// isolate leaves the session as it is: without process groups, ending it ends
// its own process only (Linux and macOS are the targets).
func isolate(*exec.Cmd) {}

// detach leaves the command as it is.
func detach(*exec.Cmd) {}

// endGroup ends the session's own process.
func endGroup(cmd *exec.Cmd) error {
	if cmd.Process == nil {
		return nil
	}
	return cmd.Process.Kill()
}
