package specgen

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"sync"

	"github.com/agent-fox-dev/spec/internal/agentrun"
	"github.com/agent-fox-dev/spec/internal/cli"
)

// DefaultMaxParallel is how many packages of a split the command generates at
// once when --max-parallel is not given.
const DefaultMaxParallel = 3

// writeSplit writes every scope of the split that has no package yet, in
// plan order.
//
// The PRD phases run one after another: a scope's PRD is written against the
// PRDs of the scopes before it, which is all of an earlier scope a later one
// builds on. A scope's package — its generation phases, the write, the
// validation — starts as soon as its PRD exists, beside the next scope's PRD
// phase, with at most Options.MaxParallel packages in flight. One or less
// keeps the old order: each scope's PRD waits for the package before it.
//
// With --adversarial, each scope's PRD is reviewed in the coordinator after
// its PRD phase, before its package is reserved, so the scope after it is
// written against the PRD the review left. A resumed split reviews only the
// scopes it writes.
//
// A package that does not validate stops nothing: it is on disk, its errors
// name the rules, and the scopes after it are not made better by being
// skipped. Any other failure stops the run starting anything new; packages
// already being generated finish, so the work they have paid for is kept and
// the plan records it. The next run on the same input resumes from the
// scopes still without a package.
func (e *runEnv) writeSplit(ctx context.Context, plan *SplitPlan, first *PRD, result *Result) (*Result, error) {
	o := e.o
	n := len(plan.Scopes)
	parallel := max(1, o.MaxParallel)
	sem := make(chan struct{}, parallel)
	var wg sync.WaitGroup

	type built struct {
		pkg *Package
		err error
	}
	builds := make([]*built, n) // written by the scope's goroutine, read after wg.Wait

	var failMu sync.Mutex
	failures := map[int]error{}
	fail := func(i int, err error) {
		failMu.Lock()
		failures[i] = err
		failMu.Unlock()
	}
	stopped := func() bool {
		failMu.Lock()
		defer failMu.Unlock()
		return len(failures) > 0
	}

	// drafts are the PRDs this run wrote, for the scopes after them; only
	// this goroutine touches the map.
	drafts := map[int]PRD{}
	specRoot := relativeTo(e.root, e.specsDir)

	for i := 0; i < n && !stopped(); i++ {
		e.mu.Lock()
		sc := plan.Scopes[i]
		e.mu.Unlock()
		if sc.Written() {
			continue
		}
		label := fmt.Sprintf("scope %d of %d (%s)", i+1, n, sc.Name)
		o.Progress.Step("write", "%s: %s", label, oneLine(sc.Scope))

		var prd PRD
		if first != nil && i == 0 {
			prd, first = *first, nil
		} else {
			// The interior boundary: this scope's PRD phase has not begun. A
			// run already over its ceiling stops here with the plan in place,
			// so the next run on the same input resumes from this scope. The
			// spend counts the phases that have finished, so packages still
			// being generated can take the run past it.
			if spent := o.Run.CostUSD(); o.TotalBudgetUSD > 0 && spent >= o.TotalBudgetUSD {
				bf := failf("budget", agentrun.CategoryBudget,
					"the run has spent $%.2f of its $%.2f total budget; the packages written so far "+
						"are on disk, re-run on the same input to continue", spent, o.TotalBudgetUSD)
				bf.TotalBudget = o.TotalBudgetUSD
				fail(i, scoped(label, bf))
				break
			}
			split := &splitContext{Plan: e.planSnapshot(plan), Index: i, SpecsDir: e.specsDir,
				SpecRoot: specRoot, Drafts: drafted(drafts, plan, e), Notes: notesOf(drafts)}
			var err error
			prd, err = e.writePRD(ctx, split, label)
			if err != nil {
				fail(i, scoped(label, err))
				break
			}
			// The plan names the package, not the model: a name the model
			// changed here would leave the next run unable to tell that this
			// scope was written.
			if prd.SpecName != sc.Name {
				o.Run.Warn(cli.WarnScopeRenamed, "low", "%s: the PRD phase named the spec %q; the planned name is used",
					label, prd.SpecName)
				prd.SpecName = sc.Name
			}
			if len(prd.RecommendedSplit) > 0 {
				o.Run.Warn(cli.WarnScopeCountMismatch, "low", "%s: the PRD phase reported this scope as %d specs' worth of work; "+
					"it is written as one, since the split was already decided",
					label, len(prd.RecommendedSplit))
			}
		}
		// The scope's PRD is reviewed here, in the coordinator, so the next
		// scope's PRD phase is shown the PRD the review left, and before the
		// number is reserved, so an interrupt leaves no reservation behind.
		var review *Review
		if o.Adversarial {
			split := &splitContext{Plan: e.planSnapshot(plan), Index: i, SpecsDir: e.specsDir,
				SpecRoot: specRoot, Drafts: drafted(drafts, plan, e), Notes: notesOf(drafts)}
			var err error
			prd, review, err = e.runRounds(ctx, prd, label, split)
			if err != nil {
				fail(i, err)
				break
			}
		}
		drafts[i] = prd
		if stopped() {
			// Another scope failed while this PRD was written.
			break
		}
		sl, err := e.reservePackage(prd, label)
		if err != nil {
			fail(i, err)
			break
		}

		sem <- struct{}{}
		wg.Add(1)
		go func(i int, prd PRD, label string, sl slot, review *Review) {
			defer wg.Done()
			defer func() { <-sem }()
			// The scope is recorded in the plan the moment its package is on
			// disk, before the read-back, the activation and the comment: a
			// run killed after the write must not leave a package the next
			// run does not know about and writes a second time.
			onWritten := func(specID, dirName string) { e.recordScope(plan, i, specID, dirName, false, label) }
			pkg, err := e.buildPackage(ctx, prd, label, sl, onWritten, review)
			builds[i] = &built{pkg: pkg, err: err}
			switch {
			case err == nil:
				e.recordScope(plan, i, pkg.SpecID, sl.dirName, false, label)
			case isInvalid(err):
				e.recordScope(plan, i, pkg.SpecID, sl.dirName, true, label)
			default:
				fail(i, err)
			}
		}(i, prd, label, sl, review)
		if parallel == 1 {
			wg.Wait()
		}
	}
	wg.Wait()

	// The packages are reported in plan order, whichever finished first.
	var invalid []string
	wrote := 0
	for _, b := range builds {
		// A scope that failed before its package was written has none to report.
		if b == nil || b.pkg == nil || b.pkg.SpecDir == "" {
			continue
		}
		if wrote == 0 {
			result.Package = *b.pkg
		} else {
			result.FollowOnSpecs = append(result.FollowOnSpecs, *b.pkg)
		}
		wrote++
		if isInvalid(b.err) {
			invalid = append(invalid, b.pkg.SpecDir)
		}
	}

	if len(failures) > 0 {
		failed := map[int]bool{}
		firstFailed := n
		for i := range failures {
			failed[i] = true
			firstFailed = min(firstFailed, i)
		}
		result.setSplit(splitReport(e.root, e.specsDir, plan, failed))
		return result, failures[firstFailed]
	}

	result.setSplit(splitReport(e.root, e.specsDir, plan, nil))
	if !o.DryRun {
		if err := plan.remove(e.specsDir); err != nil {
			o.Run.Warn(cli.WarnSplitPlanNotRemoved, "low", "the split is complete and its plan could not be removed: %v", err)
		} else {
			result.SplitPlan = ""
		}
	}
	if len(invalid) > 0 {
		return result, failf("validate", CategoryInvalid,
			"%d of %d packages do not validate (%s); each is on disk and each error names the "+
				"rule it broke", len(invalid), n, strings.Join(invalid, ", "))
	}
	o.Progress.Step("validate", "the split is complete: %d packages", n)
	return result, nil
}

// isInvalid reports whether err is the failure of a package that was written
// and does not validate.
func isInvalid(err error) bool {
	var f *Failure
	return errors.As(err, &f) && f.Category == CategoryInvalid
}

// recordScope records scope i's package in the plan and saves it. A dry run
// keeps the record in memory, for the report, and writes nothing.
func (e *runEnv) recordScope(plan *SplitPlan, i int, specID, dirName string, invalid bool, label string) {
	e.mu.Lock()
	defer e.mu.Unlock()
	sc := &plan.Scopes[i]
	sc.SpecID, sc.Dir = specID, dirName
	if invalid {
		sc.Invalid = true
	}
	if e.o.DryRun {
		return
	}
	if err := plan.save(e.specsDir); err != nil {
		e.o.Run.Warn(cli.WarnSplitPlanUpdateFailed, "high", "the split plan could not be updated after %s: %v", label, err)
	}
}

// planSnapshot is a copy of the plan a PRD phase can read while packages are
// being recorded in the original.
func (e *runEnv) planSnapshot(plan *SplitPlan) *SplitPlan {
	e.mu.Lock()
	defer e.mu.Unlock()
	cp := *plan
	cp.Scopes = slices.Clone(plan.Scopes)
	return &cp
}

// notesOf is the codebase notes of the PRDs this run wrote, by scope index.
func notesOf(drafts map[int]PRD) map[int]string {
	out := map[int]string{}
	for i, d := range drafts {
		if d.CodebaseNotes != "" {
			out[i] = d.CodebaseNotes
		}
	}
	return out
}

// drafted is the drafts whose packages are not on disk: still being
// generated, or never written by a dry run. A later scope's PRD reads the
// others from the disk.
func drafted(drafts map[int]PRD, plan *SplitPlan, e *runEnv) map[int]PRD {
	e.mu.Lock()
	defer e.mu.Unlock()
	out := map[int]PRD{}
	for i, d := range drafts {
		if e.o.DryRun || !plan.Scopes[i].Written() {
			out[i] = d
		}
	}
	return out
}
