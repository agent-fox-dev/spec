package main

import (
	"bytes"
	"context"
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
