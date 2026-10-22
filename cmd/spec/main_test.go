package main

import (
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"os"
	"strings"
	"testing"

	"github.com/agent-fox-dev/spec/internal/cli"
)

func TestMain(m *testing.M) {
	// Unset model-vendor variables so that tests exercise the intended
	// credential paths regardless of the developer's shell environment.
	for _, v := range []string{"AF_MODEL", "AGENTKIT_MODEL", "CLAUDE_CODE_USE_VERTEX", "CLAUDE_CODE_USE_BEDROCK",
		"ANTHROPIC_VERTEX_PROJECT_ID", "ANTHROPIC_VERTEX_BASE_URL"} {
		_ = os.Unsetenv(v)
	}
	os.Exit(m.Run())
}

// TS-06-53 (unit): spec's usage says --dry-run writes no files either.
func TestTS06_53_SpecUsageStatesDryRunWritesNoFiles(t *testing.T) {
	flat := strings.Join(strings.Fields(usage), " ")
	if !strings.Contains(flat, "writes no files") {
		t.Errorf("spec's usage does not say --dry-run writes no files:\n%s", usage)
	}
}

func TestSpecAcceptsSharedDryRun(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := newApp().Main(context.Background(), []string{"--dry-run", "--total-budget", "1", "--version"},
		strings.NewReader(""), &stdout, &stderr)
	if code != cli.ExitOK {
		t.Fatalf("code = %d, stderr: %s", code, stderr.String())
	}
}

// --max-parallel defaults to the pipelined split and refuses less than one.
func TestMaxParallelFlag(t *testing.T) {
	fs := flag.NewFlagSet("spec", flag.ContinueOnError)
	newApp().Flags(fs)
	if f := fs.Lookup("max-parallel"); f == nil || f.DefValue != "3" {
		t.Fatalf("--max-parallel = %+v", f)
	}
	cleanEnv(t)
	var stdout, stderr bytes.Buffer
	code := newApp().Main(context.Background(), []string{"--max-parallel", "0", "--dir", t.TempDir(), "an idea"},
		strings.NewReader(""), &stdout, &stderr)
	if code != cli.ExitUsage || !strings.Contains(stdout.String(), "--max-parallel must be at least 1") {
		t.Errorf("code = %d\n%s", code, stdout.String())
	}
}

// siblingLine fails the test unless help carries the sentence naming the three
// commands on a line of its own, after the title line and before Usage:, with a
// blank line on each side.
func siblingLine(t *testing.T, what, help string) {
	t.Helper()
	usage := strings.Index(help, "Usage:")
	if usage < 0 {
		t.Errorf("%s: the help has no Usage: line:\n%s", what, help)
		return
	}
	before := help[:usage]
	if !strings.Contains(before, "\n\n"+cli.SiblingCommands+"\n\n") {
		t.Errorf("%s: the sentence is not on a line of its own, with a blank line on each side, before Usage::\n%s", what, before)
	}
	if title, _, _ := strings.Cut(help, "\n"); title == "" || strings.Index(before, cli.SiblingCommands) < len(title) {
		t.Errorf("%s: the title line %q does not come before the sentence:\n%s", what, title, before)
	}
}

// TS-13-3: -h and --help print the sentence between the title and Usage:.
func TestHelpCarriesTheSiblingLine_TS13_3(t *testing.T) {
	cleanEnv(t)
	for _, flag := range []string{"-h", "--help"} {
		var stdout, stderr bytes.Buffer
		code := newApp().Main(context.Background(), []string{flag}, strings.NewReader(""), &stdout, &stderr)
		if code != cli.ExitOK || stdout.Len() != 0 {
			t.Fatalf("%s: code %d, stdout %q", flag, code, stdout.String())
		}
		siblingLine(t, flag, stderr.String())
	}
}

// TS-13-4: a bare invocation exits 2 with a usage envelope, and the help on
// stderr carries the sentence.
func TestBareInvocationCarriesTheSiblingLine_TS13_4(t *testing.T) {
	cleanEnv(t)
	var stdout, stderr bytes.Buffer
	code := newApp().Main(context.Background(), nil, strings.NewReader(""), &stdout, &stderr)
	var env struct {
		Error struct {
			Category string `json:"category"`
		} `json:"error"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &env); err != nil || code != cli.ExitUsage || env.Error.Category != "usage" {
		t.Fatalf("code %d, stdout %q (%v)", code, stdout.String(), err)
	}
	siblingLine(t, "a bare invocation", stderr.String())
}
