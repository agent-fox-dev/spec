//go:build !unix

package gate

import "os/exec"

// isolate leaves the command as it is: without process groups, the timeout
// ends the command's own process only (Linux and macOS are the targets).
func isolate(*exec.Cmd) {}

// endGroup does nothing without process groups.
func endGroup(*exec.Cmd) {}
