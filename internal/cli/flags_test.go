package cli

import (
	"flag"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/agent-fox-dev/spec/internal/agentrun"
)

// parseCommon registers the shared flags and parses argv the way Main does.
func parseCommon(t *testing.T, argv ...string) (*Common, string, error) {
	t.Helper()
	var c Common
	fs := flag.NewFlagSet("t", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	c.Register(fs)
	input, err := SplitArgs(fs, argv)
	return &c, input, err
}

// The one positional argument may sit before, after or between the flags; "-"
// is stdin, everything after "--" is positional, and two inputs are refused.
func TestSplitArgs(t *testing.T) {
	for _, tc := range []struct {
		name  string
		argv  []string
		input string
		model string
	}{
		{"flags first", []string{"--model", "SIMPLE", "idea"}, "idea", "SIMPLE"},
		{"flags last", []string{"idea", "--model=SIMPLE"}, "idea", "SIMPLE"},
		{"a boolean flag takes no value", []string{"--quiet", "idea"}, "idea", ""},
		{"stdin", []string{"-", "--model", "SIMPLE"}, "-", "SIMPLE"},
		{"after --", []string{"--model", "SIMPLE", "--", "-not-a-flag"}, "-not-a-flag", "SIMPLE"},
		{"none", nil, "", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c, input, err := parseCommon(t, tc.argv...)
			if err != nil || input != tc.input || c.Model != tc.model {
				t.Errorf("input = %q, model = %q, err = %v", input, c.Model, err)
			}
		})
	}
	if _, _, err := parseCommon(t, "one", "two"); err == nil || !strings.Contains(err.Error(), "expected one input, got 2") {
		t.Errorf("two inputs: err = %v", err)
	}
}

// Validate refuses the values that cannot hold, naming each, and accepts the
// defaults and every input kind.
func TestCommon_Validate(t *testing.T) {
	for _, kind := range []string{"", "file", "text", "issue", "stdin"} {
		t.Run("input kind "+kind, func(t *testing.T) {
			c, _, err := parseCommon(t, "--input-kind", kind, "x")
			if err != nil || c.InputKind != kind || c.Validate() != nil {
				t.Errorf("--input-kind %q: %v, %v", kind, err, c.Validate())
			}
		})
	}
	for _, tc := range []struct {
		name    string
		c       Common
		mention string
	}{
		{"detail", Common{Detail: "brief"}, "--detail"},
		{"input kind", Common{InputKind: "bogus"}, "--input-kind"},
		{"budget", Common{Budget: -1}, "--budget cannot be negative"},
		{"total budget", Common{TotalBudgetUSD: -1}, "--total-budget cannot be negative"},
		{"max turns", Common{MaxTurns: -1}, "--max-turns cannot be negative"},
		{"max tokens", Common{MaxTokens: -1}, "--max-tokens cannot be negative"},
		{"timeout", Common{Timeout: -time.Second}, "--phase-timeout cannot be negative"},
		{"repo map", Common{RepoMapTokens: -1}, "--repo-map-tokens cannot be negative"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if err := tc.c.Validate(); err == nil || !strings.Contains(err.Error(), tc.mention) {
				t.Errorf("err = %v, want one naming %q", err, tc.mention)
			}
		})
	}
}

// --output is optional, resolves against the working directory and not
// --dir, and cannot be stdin or a directory.
func TestCommon_ResolveOutput(t *testing.T) {
	c, _, err := parseCommon(t, "x")
	if err != nil {
		t.Fatal(err)
	}
	if got, err := c.ResolveOutput(); err != nil || got != "" {
		t.Errorf("by default = %q, %v; want none", got, err)
	}

	cwd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	got, err := (&Common{Dir: dir, Output: filepath.Join("sub", "out.json")}).ResolveOutput()
	if want := filepath.Join(cwd, "sub", "out.json"); err != nil || got != want {
		t.Errorf("resolved = %q, %v; want %q, not under --dir", got, err, want)
	}

	for name, out := range map[string]string{"stdin": "-", "a directory": t.TempDir()} {
		t.Run(name, func(t *testing.T) {
			if _, err := (&Common{Output: out}).ResolveOutput(); err == nil {
				t.Error("accepted")
			}
		})
	}
}

// The flags override the tool's defaults, and a single-phase run takes the
// lower of --budget and --total-budget as its ceiling.
func TestCommon_Bounds(t *testing.T) {
	defaults := agentrun.Bounds{MaxTurns: 10, MaxBudgetUSD: 2}
	for _, tc := range []struct {
		name   string
		c      Common
		single bool
		want   agentrun.Bounds
	}{
		{"defaults", Common{}, false, defaults},
		{"flags", Common{MaxTurns: 3, MaxTokens: 100, Budget: 7, Timeout: time.Minute}, false,
			agentrun.Bounds{MaxTurns: 3, MaxTokens: 100, MaxBudgetUSD: 7, Timeout: time.Minute}},
		{"total under budget, single phase", Common{Budget: 7, TotalBudgetUSD: 4}, true, agentrun.Bounds{MaxTurns: 10, MaxBudgetUSD: 4}},
		{"total over budget, single phase", Common{TotalBudgetUSD: 9}, true, defaults},
		{"total under budget, several phases", Common{Budget: 7, TotalBudgetUSD: 4}, false, agentrun.Bounds{MaxTurns: 10, MaxBudgetUSD: 7}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.c.Bounds(defaults, tc.single); got != tc.want {
				t.Errorf("Bounds = %+v, want %+v", got, tc.want)
			}
		})
	}
}

// The model is --model, then $AF_MODEL, then $AGENTKIT_MODEL, then the
// default tier.
func TestCommon_ModelSpec(t *testing.T) {
	for _, tc := range []struct {
		name, flag, env, fallback, want string
	}{
		{"default", "", "", "", DefaultModel},
		{"fallback variable", "", "", "SIMPLE", "SIMPLE"},
		{"variable", "", "ADVANCED", "SIMPLE", "ADVANCED"},
		{"flag", "claude-opus-5-5", "ADVANCED", "SIMPLE", "claude-opus-5-5"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv(ModelEnv, tc.env)
			t.Setenv(ModelEnvFallback, tc.fallback)
			if got := (&Common{Model: tc.flag}).ModelSpec(); got != tc.want {
				t.Errorf("ModelSpec = %q, want %q", got, tc.want)
			}
		})
	}
}

// --dir must be a directory that exists.
func TestCommon_Workspace(t *testing.T) {
	dir := t.TempDir()
	if ws, err := (&Common{Dir: dir}).Workspace(); err != nil || ws == nil {
		t.Errorf("a directory: %v", err)
	}
	file := filepath.Join(dir, "f")
	if err := os.WriteFile(file, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	for name, d := range map[string]string{"missing": filepath.Join(dir, "missing"), "a file": file} {
		t.Run(name, func(t *testing.T) {
			if _, err := (&Common{Dir: d}).Workspace(); err == nil || !strings.Contains(err.Error(), "--dir") {
				t.Errorf("err = %v", err)
			}
		})
	}
}

// The --phase-model help names every phase a model can be chosen for, the
// review phases included.
func TestRegister_PhaseModelHelpNamesEveryPhase(t *testing.T) {
	fs := flag.NewFlagSet("tool", flag.ContinueOnError)
	(&Common{}).Register(fs)
	help := fs.Lookup("phase-model").Usage
	if !strings.Contains(help, strings.Join(agentrun.Phases, ", ")) {
		t.Errorf("--phase-model help = %q", help)
	}
}
