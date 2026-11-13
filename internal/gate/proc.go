package gate

import "os/exec"

// Isolate starts cmd in a process group of its own, where the platform has
// them, and makes its cancellation end the whole group: the gate's handling,
// for another runner of the package's commands (internal/evidence).
func Isolate(cmd *exec.Cmd) { isolate(cmd) }

// EndGroup ends every process still in cmd's group once cmd has ended, so
// that nothing it started outlives the run.
func EndGroup(cmd *exec.Cmd) { endGroup(cmd) }
