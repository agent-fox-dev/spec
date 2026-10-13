package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
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

// testRoundTripper is an http.RoundTripper made of a function.
type testRoundTripper func(*http.Request) (*http.Response, error)

func (f testRoundTripper) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
