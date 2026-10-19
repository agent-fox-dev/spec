package main

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/agent-fox-dev/agentkit-go/provider/faux"
	"github.com/agent-fox-dev/agentkit-go/tools"
	"github.com/agent-fox-dev/agentkit-go/workspace"

	"github.com/agent-fox-dev/spec/internal/agentrun"
	"github.com/agent-fox-dev/spec/internal/agentrun/indextest"
	"github.com/agent-fox-dev/spec/internal/cli"
)

// withIndex makes the shell build idx as the run's one code-search index.
func withIndex(t *testing.T, idx tools.Index) {
	t.Helper()
	old := agentrun.NewIndex
	agentrun.NewIndex = func(*workspace.Workspace) (tools.Index, error) { return idx, nil }
	t.Cleanup(func() { agentrun.NewIndex = old })
}

func goRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	return dir
}

// The run's index is closed exactly once, whether the run succeeds (here,
// --preflight) or fails (here, at the PRD phase, which the unscripted model
// ends without a result).
func TestTheIndexIsClosedOnceOnEveryExitPath(t *testing.T) {
	for _, tc := range []struct {
		name string
		args []string
		code int
	}{
		{"success", []string{"--preflight"}, cli.ExitOK},
		{"failure", nil, cli.ExitFailed},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cleanEnv(t)
			t.Cleanup(cli.SetProvider(faux.New()))
			idx := &indextest.Index{}
			withIndex(t, idx)
			argv := append(append([]string{"--dir", goRepo(t)}, tc.args...), "a library that does a thing")
			var stdout, stderr bytes.Buffer
			code := newApp().Main(context.Background(), argv, strings.NewReader(""), &stdout, &stderr)
			if code != tc.code {
				t.Fatalf("code = %d, want %d\n%s", code, tc.code, stdout.String())
			}
			if n := idx.CloseCalls(); n != 1 {
				t.Errorf("Close was called %d times, want 1", n)
			}
			if n := idx.InvalidateCalls(); n != 0 {
				t.Errorf("spec never changes the tree, yet Invalidate was called %d times", n)
			}
		})
	}
}
