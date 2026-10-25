package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/agent-fox-dev/agentkit-go/provider/faux"
	"github.com/agent-fox-dev/agentkit-go/tools"
	"github.com/agent-fox-dev/agentkit-go/workspace"

	"github.com/agent-fox-dev/spec/internal/agentrun"
	"github.com/agent-fox-dev/spec/internal/issuex"
)

// newApp is an App whose Exec records the Deps it was given and, unless exec
// replaces it, succeeds with a small result.
func newApp(t *testing.T, exec func(context.Context, Deps) (int, any, *ErrorInfo)) (*App, *Deps) {
	t.Helper()
	var seen Deps
	app := &App{
		Name:    "tool",
		Version: "test",
		Usage:   "usage\n",
		Exec: func(ctx context.Context, d Deps) (int, any, *ErrorInfo) {
			seen = d
			if exec != nil {
				return exec(ctx, d)
			}
			return ExitOK, map[string]string{"stage": "done"}, nil
		},
	}
	return app, &seen
}

// refusingApp is an App whose Exec fails the test if it runs.
func refusingApp(t *testing.T) *App {
	t.Helper()
	app, _ := newApp(t, func(context.Context, Deps) (int, any, *ErrorInfo) {
		t.Error("Exec ran")
		return ExitOK, nil, nil
	})
	return app
}

// runApp runs app's Main and decodes the one JSON object it printed.
func runApp(t *testing.T, app *App, argv []string, stdin string) (Envelope, int, string) {
	t.Helper()
	var stdout, stderr bytes.Buffer
	code := app.Main(context.Background(), argv, strings.NewReader(stdin), &stdout, &stderr)
	var env Envelope
	if err := json.Unmarshal(stdout.Bytes(), &env); err != nil {
		t.Fatalf("stdout is not one JSON object (%v):\n%s", err, stdout.String())
	}
	return env, code, stderr.String()
}

// withoutIndex makes the index builder fail, so a test neither builds a real
// index nor depends on one.
func withoutIndex(t *testing.T) {
	t.Helper()
	old := agentrun.NewIndex
	agentrun.NewIndex = func(*workspace.Workspace) (tools.Index, error) { return nil, errors.New("no index in tests") }
	t.Cleanup(func() { agentrun.NewIndex = old })
}

// scripted makes the shell use a scripted model in place of Anthropic's, and
// build no code-search index.
func scripted(t *testing.T) {
	t.Helper()
	t.Cleanup(SetProvider(faux.New()))
	withoutIndex(t)
}

// noNetwork replaces the default HTTP transport with one that refuses every
// request, and returns the count of requests made.
func noNetwork(t *testing.T) *atomic.Int32 {
	t.Helper()
	old := http.DefaultTransport
	t.Cleanup(func() { http.DefaultTransport = old })
	var requests atomic.Int32
	http.DefaultTransport = testRoundTripper(func(*http.Request) (*http.Response, error) {
		requests.Add(1)
		return nil, errors.New("no network in this test")
	})
	return &requests
}

// testRoundTripper is an http.RoundTripper made of a function.
type testRoundTripper func(*http.Request) (*http.Response, error)

func (f testRoundTripper) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

// warningsOf are the warnings with the given code, in order.
func warningsOf(ws []Warning, code WarnCode) []Warning {
	var out []Warning
	for _, w := range ws {
		if w.Code == code {
			out = append(out, w)
		}
	}
	return out
}

// unwritableOutput returns a path whose parent cannot be created, because an
// ancestor is a regular file (this holds when running as root too).
func unwritableOutput(t *testing.T) string {
	t.Helper()
	blocker := filepath.Join(t.TempDir(), "file")
	if err := os.WriteFile(blocker, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	return filepath.Join(blocker, "sub", "out.json")
}

// mockForgeClient answers ReadIssue from readIssueFn, else with readErr, else
// with thread.
type mockForgeClient struct {
	issuex.NoOpClient
	authenticated bool
	readErr       error
	thread        issuex.IssueThread
	readIssueFn   func(ctx context.Context, ref issuex.IssueRef) (issuex.IssueThread, error)
}

func (m *mockForgeClient) Authenticated() bool { return m.authenticated }

func (m *mockForgeClient) ReadIssue(ctx context.Context, ref issuex.IssueRef) (issuex.IssueThread, error) {
	if m.readIssueFn != nil {
		return m.readIssueFn(ctx, ref)
	}
	if m.readErr != nil {
		return issuex.IssueThread{}, m.readErr
	}
	return m.thread, nil
}
