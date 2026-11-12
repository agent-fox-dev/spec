package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/agent-fox-dev/spec/afspec"
	"github.com/agent-fox-dev/spec/envelope"
	"github.com/agent-fox-dev/spec/internal/cli"
	"github.com/agent-fox-dev/spec/internal/gate"
	"github.com/agent-fox-dev/spec/internal/specpkg"
)

// gateLevels are the gate's two levels, in the order the help names them.
var gateLevels = []string{envelope.GateLevelTask, envelope.GateLevelFull}

// givenDuration is a duration flag that knows whether it was given, so that
// --timeout without --gate is refused rather than ignored.
type givenDuration struct {
	value time.Duration
	given bool
}

func (f *givenDuration) String() string { return f.value.String() }

func (f *givenDuration) Set(v string) error {
	d, err := time.ParseDuration(v)
	if err != nil {
		return errors.New("not a duration, such as 90s or 20m")
	}
	f.value, f.given = d, true
	return nil
}

// Get makes the flag a flag.Getter, so that --schema types it as a duration.
func (f *givenDuration) Get() any { return f.value }

// executeGate is the gate form of speca <spec> --gate task|full: the
// package's linter command, then its all_tests command, run in the repository
// whatever the package's status, or the recorded pass of the same tree reused.
// Every verdict is a successful call; only a package that cannot be read fails
// it. It writes nothing under --dir: what it keeps goes to the state directory.
func executeGate(c *call, dir string) (*Result, *cli.ErrorInfo) {
	spec, failure := load(dir)
	if failure != nil {
		return nil, failure
	}
	cmds := gateCommands(spec)
	key := gate.Key{Level: c.opts.gate.value, Linter: cmds[0].Line, AllTests: cmds[1].Line, CleanEnv: c.opts.cleanEnv}
	base := ""
	if affected := affectedTests(spec); affected != "" && key.Level == envelope.GateLevelTask {
		id, refusal := baseCommit(c)
		if refusal != nil {
			return nil, refusal
		}
		base = id
		cmds[1] = gate.Command{Name: gate.AffectedTests, Line: strings.Replace(affected, afspec.AffectedTestsBase, id, 1)}
		key.AllTests, key.AffectedTests = "", cmds[1].Line
	}

	// The tree is read before anything runs: a command may change it.
	repo := c.root
	tree, treeErr := gate.ReadTree(c.root)
	if treeErr == nil {
		repo, key.Tree, key.Dir = tree.Top, tree.ID, dirIn(tree.Top, c.root)
	}
	store, storeErr := gate.OpenStore(repo)
	switch {
	case storeErr != nil:
		c.run.Warn(cli.WarnGateNotRecorded, "low", "the state directory could not be written (%v): the gate runs, "+
			"with no log, and its result is not recorded for reuse", storeErr)
	case treeErr != nil:
		c.run.Warn(cli.WarnGateNotRecorded, "low", "there is no tree id to record the result under (%v): the gate runs, "+
			"and its result is not recorded for reuse", treeErr)
	}
	recording := storeErr == nil && treeErr == nil

	var g envelope.GateResult
	at := time.Now().UTC()
	if p, ok := lookup(store, key, recording); ok {
		g = reusedPass(p)
	} else {
		runner := gate.Runner{Dir: c.root, Timeout: c.opts.timeout.value}
		if store != nil {
			runner.LogDir = store.LogDir()
		}
		if key.Level == envelope.GateLevelFull {
			runner.Units = gate.AllTests
		}
		g = runGate(runner, cmds, c.opts.cleanEnv)
		if key.Level == envelope.GateLevelFull && g.Slowest == nil {
			g.Slowest = []envelope.GateUnit{}
		}
		if recording && g.Verdict == envelope.GateVerdictPass {
			p := gate.Pass{Key: key, Commit: tree.Commit, At: at.Format(time.RFC3339), Seconds: g.Seconds, Slowest: g.Slowest}
			if err := store.Record(p); err != nil {
				c.run.Warn(cli.WarnGateNotRecorded, "low", "the pass could not be recorded under %s (%v): the next gate of this tree runs again", store.Dir, err)
			}
		}
	}
	g.Level, g.CleanEnv, g.Base = key.Level, key.CleanEnv, base
	if key.Level != envelope.GateLevelFull {
		g.Slowest = nil
	}
	if recording && g.Verdict != envelope.GateVerdictNotRun {
		r := gate.Run{SpecID: spec.SpecID, At: at.Format(time.RFC3339), Commit: tree.Commit, Level: g.Level, CleanEnv: g.CleanEnv,
			Verdict: g.Verdict, Seconds: g.Seconds, Reused: g.Reused, Slowest: g.Slowest}
		if err := store.AppendRun(r); err != nil {
			c.run.Warn(cli.WarnGateNotRecorded, "low", "the run could not be added to the run log under %s (%v): the gate report will not count it", store.Dir, err)
		}
	}
	return &Result{AdminResult: envelope.AdminResult{
		SpecDir: specpkg.Display(dir, c.root), SpecID: spec.SpecID, SpecName: spec.SpecName, Status: spec.Status, Gate: &g,
	}}, nil
}

// dirIn is dir as a path under the work tree's top level, "." for the top
// itself: two directories of one repository run different gates.
func dirIn(top, dir string) string {
	if real, err := filepath.EvalSymlinks(dir); err == nil {
		dir = real
	}
	if realTop, err := filepath.EvalSymlinks(top); err == nil {
		top = realTop
	}
	rel, err := filepath.Rel(top, dir)
	if err != nil {
		return dir
	}
	return filepath.ToSlash(rel)
}

// lookup is the pass recorded under key, when the gate can be recorded at all.
func lookup(store *gate.Store, key gate.Key, recording bool) (gate.Pass, bool) {
	if !recording {
		return gate.Pass{}, false
	}
	return store.Lookup(key)
}

// reusedPass is the verdict of a gate answered from the recorded pass p: it
// ran nothing, so it has no command and no seconds of its own.
func reusedPass(p gate.Pass) envelope.GateResult {
	slowest := p.Slowest
	if slowest == nil {
		slowest = []envelope.GateUnit{}
	}
	return envelope.GateResult{Verdict: envelope.GateVerdictPass, Commands: []envelope.GateCommand{}, Slowest: slowest,
		Reused: &envelope.GateReuse{Commit: p.Commit, At: p.At, Seconds: p.Seconds}}
}

// runGate runs the commands, in a clean environment when clean is true: an
// empty HOME made for this gate, removed when it ends whatever the verdict.
func runGate(runner gate.Runner, cmds []gate.Command, clean bool) envelope.GateResult {
	if clean {
		home, remove, err := gate.NewHome()
		if err != nil {
			return envelope.GateResult{Verdict: envelope.GateVerdictNotRun, Commands: []envelope.GateCommand{},
				Reason: fmt.Sprintf("the clean environment could not be made: %v", err)}
		}
		defer remove()
		runner.Env = gate.CleanEnv(os.Environ(), home)
	}
	return runner.Run(cmds)
}

// affectedTests is the package's affected_tests command, "" when it names
// none.
func affectedTests(spec *afspec.Spec) string {
	if spec.Tasks == nil || spec.Tasks.TestCommands.AffectedTests == nil {
		return ""
	}
	return *spec.Tasks.TestCommands.AffectedTests
}

// baseCommit is the full id of the commit --base names, HEAD when it is not
// given, in the git work tree of --dir; anything else is a usage error, made
// before a command runs.
func baseCommit(c *call) (string, *cli.ErrorInfo) {
	top, err := gate.Top(c.root)
	if err != nil {
		return "", usageInfo(fmt.Errorf("the package has affected_tests, which runs from a base commit, and %v", err), nil)
	}
	rev := "HEAD"
	if c.opts.base.given {
		rev = c.opts.base.value
	}
	id, err := gate.Commit(top, rev)
	if err != nil {
		return "", usageInfo(fmt.Errorf("--base %v", err), nil)
	}
	return id, nil
}

// gateCommands are the package's linter and all_tests commands, in the order
// the gate runs them; a command the package does not name is empty.
func gateCommands(spec *afspec.Spec) []gate.Command {
	var tc afspec.TestCommands
	if spec.Tasks != nil {
		tc = spec.Tasks.TestCommands
	}
	return []gate.Command{{Name: gate.Linter, Line: tc.Linter}, {Name: gate.AllTests, Line: tc.AllTests}}
}
