package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
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
// whatever the package's status. Every verdict is a successful call; only a
// package that cannot be read fails it. It writes nothing under --dir.
func executeGate(c *call, dir string) (*Result, *cli.ErrorInfo) {
	spec, failure := load(dir)
	if failure != nil {
		return nil, failure
	}
	level := c.opts.gate.value
	runner := gate.Runner{Dir: c.root, Timeout: c.opts.timeout.value}
	if logs, err := logDir(c.root); err == nil {
		runner.LogDir = logs
	}
	var g envelope.GateResult
	if home, remove, err := cleanHome(c.opts.cleanEnv); err != nil {
		g = envelope.GateResult{Verdict: envelope.GateVerdictNotRun, Commands: []envelope.GateCommand{},
			Reason: fmt.Sprintf("the clean environment could not be made: %v", err)}
	} else {
		defer remove()
		if home != "" {
			runner.Env = gate.CleanEnv(os.Environ(), home)
		}
		g = runner.Run(gateCommands(spec))
	}
	g.Level, g.CleanEnv = level, c.opts.cleanEnv
	if level == envelope.GateLevelFull {
		g.Slowest = []envelope.GateUnit{}
	}
	return &Result{AdminResult: envelope.AdminResult{
		SpecDir: specpkg.Display(dir, c.root), SpecID: spec.SpecID, SpecName: spec.SpecName, Status: spec.Status, Gate: &g,
	}}, nil
}

// cleanHome makes the empty HOME of a clean gate, and the function that removes
// it when the gate ends; without --clean-env it makes nothing.
func cleanHome(clean bool) (string, func(), error) {
	if !clean {
		return "", func() {}, nil
	}
	return gate.NewHome()
}

// logDir is the directory, under the state directory, that the commands' logs
// of the repository at root are written to, created if need be.
func logDir(root string) (string, error) {
	state, err := gate.StateDir()
	if err != nil {
		return "", err
	}
	dir := filepath.Join(gate.RepoDir(state, root), "logs")
	return dir, os.MkdirAll(dir, 0o700)
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
