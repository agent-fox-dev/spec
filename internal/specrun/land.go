package specrun

import (
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
	// A session that ended in an error and left the step unfinished stops
	// with one of these, then a colon and the first line of what it said.
	reasonSessionError = "the session ended with an error"
	reasonAPIError     = "the session ended with an API error"
)

// landed checks from the repository that the package in dir landed: the base
// branch is checked out, the working tree is clean, HEAD is not before, and
// the package has no task pending or in progress. It returns the first check
// that fails, or "".
func (r *run) landed(dir, before string) string {
	w, err := worktreeOf(r.root)
	switch {
	case err != nil || w.branch != r.base:
		return reasonNotOnBase
	case !w.clean:
		return reasonDirty
	case w.head == "" || w.head == before:
		return reasonHeadStill
	}
	spec, err := afspec.LoadSpec(dir)
	if err != nil || len(spec.OpenTasks()) > 0 {
		return reasonTasksOpen
	}
	return ""
}
