package main

import (
	"bytes"
	"context"
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
