package main

import (
	"errors"
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
	g := runner.Run(gateCommands(spec))
	g.Level = level
	if level == envelope.GateLevelFull {
		g.Slowest = []envelope.GateUnit{}
	}
	return &Result{AdminResult: envelope.AdminResult{
		SpecDir: specpkg.Display(dir, c.root), SpecID: spec.SpecID, SpecName: spec.SpecName, Status: spec.Status, Gate: &g,
	}}, nil
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
