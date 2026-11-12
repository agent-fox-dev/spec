package main

import (
	"errors"
	"fmt"
	"path/filepath"

	"github.com/agent-fox-dev/spec/afspec"
	"github.com/agent-fox-dev/spec/envelope"
	"github.com/agent-fox-dev/spec/internal/cli"
	"github.com/agent-fox-dev/spec/internal/specpkg"
)

// nextSpec is the form of speca --next: the package to implement now, which
// afspec.ChooseNext chooses (the same choice specrun makes). A package under
// archive/ is history: a dependency on one is finished. A cycle among the
// packages fails the call, since no order can satisfy it.
func nextSpec(c *call) (*Result, *cli.ErrorInfo) {
	choice, err := afspec.ChooseNext(c.specsDir, func(p string) string { return specpkg.Display(p, c.root) })
	var rootErr *afspec.SpecRootError
	var cycle *afspec.CycleError
	switch {
	case errors.As(err, &rootErr):
		return nil, &cli.ErrorInfo{Stage: "load", Category: "input",
			Message: fmt.Sprintf("the spec root %s could not be read: %v", specpkg.Display(c.specsDir, c.root), rootErr.Err),
			FixHint: &cli.FixHint{Flag: "--specs-dir", Env: []string{afspec.SpecDirEnv}}}
	case errors.As(err, &cycle):
		return nil, &cli.ErrorInfo{Stage: "graph", Category: "input", Message: cycle.Error()}
	case err != nil:
		return nil, &cli.ErrorInfo{Stage: "load", Category: "input", Message: err.Error()}
	}
	if choice.ArchiveErr != nil {
		c.warnUnreadable("the archive", filepath.Join(c.specsDir, "archive"), choice.ArchiveErr,
			"the packages under archive/ are not read, and a dependency on one of them is not finished")
	}

	r := &Result{nextForm: true}
	if n := choice.Next; n != nil {
		info := packageInfo(n.Dir, n.Spec, false)
		r.NextSpec = &info
	}
	r.Waiting = []envelope.WaitingInfo{}
	for _, w := range choice.Waiting {
		r.Waiting = append(r.Waiting, envelope.WaitingInfo{SpecDir: w.Dir, SpecID: w.SpecID, WaitsOn: w.WaitsOn, Reason: w.Reason})
	}
	allDone := choice.AllDone
	r.AllDone = &allDone
	return r, nil
}

// nextSummary says what --next found: the package to implement, or that
// everything is done, or that nothing can start.
func (r *Result) nextSummary() string {
	switch {
	case r.NextSpec != nil:
		s := fmt.Sprintf("%s: next is %s", tool, r.NextSpec.SpecDir)
		if n := len(r.Waiting); n > 0 {
			s += fmt.Sprintf("; %d more waiting", n)
		}
		return s
	case r.AllDone != nil && *r.AllDone:
		return tool + ": every package is done; nothing is left to implement"
	default:
		return fmt.Sprintf("%s: no package can start; %d waiting", tool, len(r.Waiting))
	}
}

// nextNext suggests the report on the package to implement, which names the
// task to start with.
func (r *Result) nextNext() []cli.Next {
	if r.NextSpec == nil {
		return nil
	}
	return []cli.Next{{Tool: tool, Input: r.NextSpec.SpecDir, Flags: []string{},
		Why: fmt.Sprintf("%s is the next package to implement; its report names the task to start with", r.NextSpec.SpecDir)}}
}
