package specrun

import (
	"strings"

	"github.com/agent-fox-dev/spec/afspec"
)

// The landing checks, in the order they are made, by the words that name a
// failed one.
const (
	reasonNotOnBase   = "not on the base branch"
	reasonDirty       = "working tree not clean"
	reasonHeadStill   = "HEAD did not move"
	reasonTasksOpen   = "tasks still open"
	reasonStalled     = "stalled"
	reasonNoSession   = "the session could not start"
	reasonInterrupted = "interrupted"
)

// landed checks from the repository that the package in dir landed: the base
// branch is checked out, the working tree is clean, HEAD is not before, and
// the package has no task pending or in progress. It returns the first check
// that fails, or "".
func (r *run) landed(dir, before string) string {
	if branch, err := gitOutput(r.root, "branch", "--show-current"); err != nil || branch != r.base {
		return reasonNotOnBase
	}
	if status, err := gitOutput(r.root, "status", "--porcelain"); err != nil || strings.TrimSpace(status) != "" {
		return reasonDirty
	}
	if head, err := gitOutput(r.root, "rev-parse", "HEAD"); err != nil || head == before {
		return reasonHeadStill
	}
	spec, err := afspec.LoadSpec(dir)
	if err != nil || len(spec.OpenTasks()) > 0 {
		return reasonTasksOpen
	}
	return ""
}
