package main

import (
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/agent-fox-dev/spec/afspec"
	"github.com/agent-fox-dev/spec/envelope"
	"github.com/agent-fox-dev/spec/internal/cli"
	"github.com/agent-fox-dev/spec/internal/specpkg"
)

// statusTargets are the statuses --status moves a package to, in the order of
// its life. draft is not one: no move leads back to it.
var statusTargets = []string{
	string(afspec.PrdFrontmatterV2JsonStatusActive),
	string(afspec.PrdFrontmatterV2JsonStatusSealed),
	string(afspec.PrdFrontmatterV2JsonStatusSuperseded),
	string(afspec.PrdFrontmatterV2JsonStatusArchived),
}

// givenFlag is a string flag that remembers it was given. --status "" must be
// refused as a status that is none of the four, not taken for no --status at
// all, which would report the package where a move was asked for.
type givenFlag struct {
	value string
	given bool
}

func (f *givenFlag) String() string { return f.value }

func (f *givenFlag) Set(v string) error {
	f.value, f.given = v, true
	return nil
}

// statusCall is one --status call as the checks see it: what was asked, the
// packages as they were loaded, and what the checks found out about the move.
type statusCall struct {
	*call
	dir    string
	name   string // dir, as the call shows it
	target string // the status asked for
	spec   *afspec.Spec
	byDir  string       // the replacing package, or ""
	by     *afspec.Spec // the replacing package, loaded

	// under is whether the package is already under archive/. moveOnly is
	// whether the call only moves it: the package is already at the target
	// status, which is archived or superseded, and is not under archive/. It
	// resumes a call that was interrupted after it wrote the status.
	under, moveOnly bool

	// warnings are what the call will say when it goes ahead.
	warnings []plannedWarning
}

// A statusCheck looks at a call before anything is written, and refuses it, or
// returns nil.
type statusCheck func(*statusCall) *cli.ErrorInfo

// statusChecks are the checks of every --status call, in the order they run,
// under --dry-run too. The first one that refuses ends the call: it has no
// result, and nothing was written.
var statusChecks = []statusCheck{checkLoaded, checkMove, checkTarget, checkUnknownKeys, checkDestination}

// The library calls the form makes and the read-back that follows them are
// variables, so that a test can fail them.
var (
	transitionSpec   = (*afspec.Spec).Transition
	moveUnderArchive = afspec.MoveUnderArchive
	loadWritten      = afspec.LoadSpec
)

// executeStatus is the status form: speca <spec> --status S [--by <spec>].
//
// It resolves --by, which is a usage error where it cannot name another
// package, and then makes the checks in order.
func executeStatus(c *call, dir string) (*Result, *cli.ErrorInfo) {
	sc := &statusCall{call: c, dir: dir, name: specpkg.Display(dir, c.root), target: c.opts.status.value}
	var refusal *cli.ErrorInfo
	if sc.byDir, refusal = c.locateBy(dir); refusal != nil {
		return nil, refusal
	}
	for _, check := range statusChecks {
		if refusal := check(sc); refusal != nil {
			return nil, refusal
		}
	}
	if c.opts.dryRun {
		// A dry run is made in task 9; until then it must not write.
		return nil, &cli.ErrorInfo{Stage: "status", Category: envelope.CategoryInternal,
			Message: "--dry-run is not implemented yet for the status form"}
	}
	return sc.apply()
}

// movesUnderArchive reports whether the call ends with the package under
// archive/ and starts with it elsewhere: an archive or a supersede of a package
// that is not there yet, which a move-only call finishes.
func (sc *statusCall) movesUnderArchive() bool {
	return !sc.under && (sc.target == string(afspec.PrdFrontmatterV2JsonStatusArchived) ||
		sc.target == string(afspec.PrdFrontmatterV2JsonStatusSuperseded))
}

// holdingDir is the directory that holds the package: where archive/ is made.
func (sc *statusCall) holdingDir() string { return filepath.Dir(sc.dir) }

// checkDestination refuses a move under archive/ whose destination is taken,
// before anything is written. --force does not override it. The refusal has no
// result: nothing was written, so there is nothing to resume.
func checkDestination(sc *statusCall) *cli.ErrorInfo {
	if !sc.movesUnderArchive() {
		return nil
	}
	if _, err := afspec.ArchiveDestination(sc.dir, sc.holdingDir()); err != nil {
		return &cli.ErrorInfo{Stage: "archive", Category: envelope.CategoryDisk,
			Message: fmt.Sprintf("%s cannot be moved under archive/: %v", sc.name, err)}
	}
	return nil
}

// apply writes what the call asks for, in order, and returns the package as it
// is now. updated_at is set, since the library does not, on every package the
// call writes.
func (sc *statusCall) apply() (*Result, *cli.ErrorInfo) {
	var changes []envelope.Change
	here := sc.dir
	superseding := sc.target == string(afspec.PrdFrontmatterV2JsonStatusSuperseded)
	if !sc.moveOnly {
		sc.spec.UpdatedAt = time.Now().UTC().Format(time.RFC3339)
		var err error
		if superseding {
			err = sc.supersede()
		} else {
			_, err = transitionSpec(sc.spec, sc.target, sc.dir)
		}
		if err != nil {
			return nil, writeFailure(sc.name, err)
		}
		changes = append(changes, envelope.Change{Kind: "status", From: sc.spec.Status, To: sc.target})
		sc.raise()
	}
	if superseding {
		if change := sc.recordSupersedes(); change != nil {
			changes = append(changes, *change)
		}
	}
	if sc.movesUnderArchive() {
		dest, err := moveUnderArchive(sc.dir, sc.holdingDir())
		if err != nil {
			return nil, &cli.ErrorInfo{Stage: "archive", Category: envelope.CategoryDisk,
				Message: fmt.Sprintf("%s was written but could not be moved under archive/: %v", sc.name, err)}
		}
		changes = append(changes, envelope.Change{Kind: "moved", From: sc.name, To: specpkg.Display(dest, sc.root)})
		sc.raise()
		here = dest
	}

	written, err := loadWritten(here)
	if err != nil {
		return nil, &cli.ErrorInfo{Stage: "load", Category: envelope.CategoryInput,
			Message: fmt.Sprintf("the package %s was written but could not be read back: %v", here, err)}
	}
	r := sc.reportOf(here, written)
	r.Changes = changes
	r.nextSet = true
	return r, nil
}

// writeFailure says how a write of the status failed. What the lifecycle or the
// intent refuses despite the checks is a refusal at stage status; anything else
// is the disk's, in the library's words.
func writeFailure(name string, err error) *cli.ErrorInfo {
	var lifecycleErr *afspec.LifecycleError
	var intentErr *afspec.IntentError
	if errors.As(err, &lifecycleErr) || errors.As(err, &intentErr) {
		return lifecycleRefusal(fmt.Sprintf("%s cannot be changed: %v", name, err), nil)
	}
	return &cli.ErrorInfo{Stage: "write", Category: envelope.CategoryDisk, Message: err.Error()}
}

// locateBy resolves --by as the input is resolved, and refuses a --by that names
// the package itself. Its refusals are usage errors that begin --by:. It returns
// "" when --by was not given.
func (c *call) locateBy(dir string) (string, *cli.ErrorInfo) {
	ref := strings.TrimSpace(c.opts.by.value)
	if !c.opts.by.given || ref == "" {
		return "", nil
	}
	byDir, err := specpkg.Locate(tool, ref, c.root, c.specsDir)
	if err != nil {
		return "", usageInfo(fmt.Errorf("--by: %w", err), nil)
	}
	if sameDir(dir, byDir) {
		return "", usageInfo(fmt.Errorf("--by: %s is the package itself: name the package that replaces it", specpkg.Display(dir, c.root)), nil)
	}
	return byDir, nil
}

// checkLoaded loads the package, and the replacing package. One the library
// refuses, or one in another format version, fails the call at stage load,
// naming --by when it is the replacing package: --force does not reach it. A
// replacing package that has the package's own spec_id is a usage error, which
// only the loaded packages can tell.
func checkLoaded(sc *statusCall) *cli.ErrorInfo {
	spec, failure := load(sc.dir)
	if failure != nil {
		return failure
	}
	if specpkg.Legacy(spec) {
		return legacyRefusal(sc.name, spec)
	}
	sc.spec = spec
	if sc.byDir == "" {
		return nil
	}
	byName := specpkg.Display(sc.byDir, sc.root)
	by, err := afspec.LoadSpec(sc.byDir)
	if err != nil {
		return &cli.ErrorInfo{Stage: "load", Category: envelope.CategoryInput,
			Message: fmt.Sprintf("the --by package %s could not be read: %v", byName, err)}
	}
	if specpkg.Legacy(by) {
		return legacyRefusal("the --by package "+byName, by)
	}
	if by.SpecID == spec.SpecID {
		return usageInfo(fmt.Errorf("--by: %s has spec_id %s, the same as %s: a package is superseded by another one",
			byName, by.SpecID, sc.name), nil)
	}
	sc.by = by
	return nil
}

// legacyRefusal is the refusal of a package in a format version other than 2.
// A write through the version 2 types, of its status or of its supersedes
// list, would replace its tasks and tests with what the types read of them,
// which is nothing.
func legacyRefusal(who string, spec *afspec.Spec) *cli.ErrorInfo {
	return &cli.ErrorInfo{Stage: "load", Category: envelope.CategoryInput,
		Message: fmt.Sprintf("%s is format version %d, and speca changes the state of format version 2 packages only: "+
			"a write would discard its tasks and tests, which the version 2 types do not read; convert it to version %d first "+
			"(format v2 §13 maps each v1 construct to its v2 form)", who, spec.SchemaVersion, afspec.SchemaVersion)}
}

// underArchive reports whether dir is a package under archive/: the directory
// that holds it is named archive. No spec root is involved, so a package that
// was put in some other archive/ by hand counts too.
func underArchive(dir string) bool {
	return filepath.Base(filepath.Dir(dir)) == "archive"
}

// checkMove classifies the call as a move-only call, or checks that the
// lifecycle allows the move. --force does not override a refusal.
func checkMove(sc *statusCall) *cli.ErrorInfo {
	status := sc.spec.Status
	sc.under = underArchive(sc.dir)
	sc.moveOnly = sc.target == status && !sc.under &&
		(status == string(afspec.PrdFrontmatterV2JsonStatusArchived) || status == string(afspec.PrdFrontmatterV2JsonStatusSuperseded))
	if sc.moveOnly || afspec.ValidTransition(status, sc.target) {
		return nil
	}
	refusal := &cli.ErrorInfo{Stage: "status", Category: envelope.CategoryLifecycle, Message: refusalMessage(sc.name, status, sc.target)}
	if allowed := allowedStatus(status); len(allowed) > 0 {
		refusal.FixHint = &cli.FixHint{Flag: "--status", Valid: allowed}
	}
	return refusal
}

// moveVerbs say a move to a status as an instruction.
var moveVerbs = map[string]string{
	string(afspec.PrdFrontmatterV2JsonStatusActive):     "activate",
	string(afspec.PrdFrontmatterV2JsonStatusSealed):     "seal",
	string(afspec.PrdFrontmatterV2JsonStatusSuperseded): "supersede",
	string(afspec.PrdFrontmatterV2JsonStatusArchived):   "archive",
}

// shortestMoves is a shortest chain of moves the lifecycle allows from one
// status to another, by breadth-first search over ValidTransition: the statuses
// it passes through and the target, not the one it starts from. It is nil when
// there is none.
func shortestMoves(from, to string) []string {
	previous := map[string]string{from: ""}
	queue := []string{from}
	for len(queue) > 0 {
		at := queue[0]
		queue = queue[1:]
		for _, next := range lifecycle {
			if _, seen := previous[next]; seen || !afspec.ValidTransition(at, next) {
				continue
			}
			previous[next] = at
			if next == to {
				var chain []string
				for s := to; s != from; s = previous[s] {
					chain = append([]string{s}, chain...)
				}
				return chain
			}
			queue = append(queue, next)
		}
	}
	return nil
}

// refusalMessage says why the lifecycle does not allow a package named name to
// move from current to target: that it is there already, or the shortest chain
// of allowed moves that does lead there ("seal it first, then archive it"), or
// that none does.
func refusalMessage(name, current, target string) string {
	if current == target {
		return fmt.Sprintf("%s is already %s, so there is no move to make", name, current)
	}
	chain := shortestMoves(current, target)
	if chain == nil {
		return fmt.Sprintf("%s is %s and cannot move to %s: no sequence of allowed moves leads from %s to %s",
			name, current, target, current, target)
	}
	steps := make([]string, len(chain))
	for i, status := range chain {
		switch {
		case i == len(chain)-1 && status == string(afspec.PrdFrontmatterV2JsonStatusSuperseded):
			steps[i] = fmt.Sprintf("then %s it, with --by naming the package that replaces it", moveVerbs[status])
		case i == len(chain)-1:
			steps[i] = fmt.Sprintf("then %s it", moveVerbs[status])
		case i == 0:
			steps[i] = fmt.Sprintf("%s it first (`speca %s --status %s`)", moveVerbs[status], name, status)
		default:
			steps[i] = fmt.Sprintf("then %s it (`speca %s --status %s`)", moveVerbs[status], name, status)
		}
	}
	return fmt.Sprintf("%s is %s and cannot move to %s directly; the shortest way is %s: %s",
		name, current, target, strings.Join(append([]string{current}, chain...), " → "), strings.Join(steps, ", "))
}
