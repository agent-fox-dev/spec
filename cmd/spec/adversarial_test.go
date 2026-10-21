package main

import (
	"flag"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/agent-fox-dev/agentkit-go/provider/faux"

	"github.com/agent-fox-dev/spec/internal/cli"
)

// TS-09-1 (unit): --adversarial is off by default, and on its own gives a
// round limit of 1; --adversarial-rounds N gives N.
//
// Verifies: 09-REQ-1.1
func TestTS09_1_TheAdversarialFlagsReachTheOptions(t *testing.T) {
	parse := func(args ...string) *specFlags {
		t.Helper()
		f := &specFlags{}
		fs := flag.NewFlagSet("spec", flag.ContinueOnError)
		f.register(fs)
		if err := fs.Parse(args); err != nil {
			t.Fatal(err)
		}
		return f
	}
	d := cli.Deps{Common: &cli.Common{}}

	if o := parse().specOptions(d); o.Adversarial {
		t.Errorf("--adversarial is on by default: %+v", o)
	}
	if o := parse("--adversarial").specOptions(d); !o.Adversarial || o.AdversarialRounds != 1 {
		t.Errorf("--adversarial alone: adversarial %v, rounds %d", o.Adversarial, o.AdversarialRounds)
	}
	if o := parse("--adversarial", "--adversarial-rounds", "3").specOptions(d); !o.Adversarial || o.AdversarialRounds != 3 {
		t.Errorf("--adversarial-rounds 3: adversarial %v, rounds %d", o.Adversarial, o.AdversarialRounds)
	}
}

// refusalHarness runs spec on an issue input whose forge API counts every
// request, with a scripted model that records every call, so a test can say
// that nothing was fetched and no model was called.
type refusalHarness struct {
	t       *testing.T
	fetches atomic.Int32
	model   *faux.Provider
	dir     string
}

func newRefusalHarness(t *testing.T) *refusalHarness {
	t.Helper()
	cleanEnv(t)
	withIndex(t, nil)
	h := &refusalHarness{t: t, model: faux.New(), dir: t.TempDir()}
	forge := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		h.fetches.Add(1)
		http.Error(w, "the input must not be fetched", http.StatusTeapot)
	}))
	t.Cleanup(forge.Close)
	t.Setenv("GITHUB_API_URL", forge.URL)
	t.Setenv("ANTHROPIC_API_KEY", "test-key")
	t.Cleanup(cli.SetProvider(h.model))
	return h
}

func (h *refusalHarness) run(args ...string) (int, map[string]any) {
	h.t.Helper()
	argv := append([]string{"--dir", h.dir}, args...)
	return runSpecApp(h.t, append(argv, "https://github.com/a/b/issues/1")...)
}

// assertRefusedBeforeAnything checks the usage error and that the run did
// nothing: no fetch, no model call, no phase, no file.
// The message must contain because, so a refusal for another reason (an
// undefined flag, say) does not pass for this one.
func (h *refusalHarness) assertRefusedBeforeAnything(what, because string, code int, env map[string]any) {
	h.t.Helper()
	errInfo, _ := env["error"].(map[string]any)
	if code != cli.ExitUsage || errInfo["category"] != "usage" {
		h.t.Errorf("%s: code %d, error %v", what, code, env["error"])
	}
	if msg, _ := errInfo["message"].(string); !strings.Contains(msg, because) {
		h.t.Errorf("%s: message %q does not say %q", what, msg, because)
	}
	if n := h.fetches.Load(); n != 0 {
		h.t.Errorf("%s: the input was fetched %d times", what, n)
	}
	if n := len(h.model.Requests()); n != 0 {
		h.t.Errorf("%s: the model was called %d times", what, n)
	}
	if _, ok := env["usage"]; ok {
		h.t.Errorf("%s: usage recorded phases: %v", what, env["usage"])
	}
	if _, err := os.Stat(filepath.Join(h.dir, ".specs")); err == nil {
		h.t.Errorf("%s: a spec root was written", what)
	}
}

// TS-09-2 (unit): a round count outside 1 to 5, an explicit 0 included, is a
// usage error with or without --preflight, before anything is fetched.
//
// Verifies: 09-REQ-1.2
func TestTS09_2_ARoundCountOutsideOneToFiveIsAUsageError(t *testing.T) {
	h := newRefusalHarness(t)
	for _, n := range []string{"0", "6", "-1"} {
		for _, pf := range []bool{false, true} {
			args := []string{"--adversarial", "--adversarial-rounds", n}
			if pf {
				args = append(args, "--preflight")
			}
			code, env := h.run(args...)
			h.assertRefusedBeforeAnything("--adversarial-rounds "+n, "1 to 5", code, env)
		}
	}
}

// TS-09-3 (unit): --adversarial-rounds without --adversarial is a usage
// error, with or without --preflight, before anything is fetched.
//
// Verifies: 09-REQ-1.3
func TestTS09_3_RoundsWithoutAdversarialIsAUsageError(t *testing.T) {
	h := newRefusalHarness(t)
	for _, pf := range []bool{false, true} {
		args := []string{"--adversarial-rounds", "2"}
		if pf {
			args = append(args, "--preflight")
		}
		code, env := h.run(args...)
		h.assertRefusedBeforeAnything("--adversarial-rounds alone", "without --adversarial", code, env)
	}
}

// TS-09-4 (unit): a value that is not an integer is refused by the flag
// parser as a usage error.
//
// Verifies: 09-REQ-1.4
func TestTS09_4_ANonIntegerRoundCountIsRefusedByTheParser(t *testing.T) {
	h := newRefusalHarness(t)
	for _, v := range []string{"abc", "1.5"} {
		code, env := h.run("--adversarial", "--adversarial-rounds", v)
		h.assertRefusedBeforeAnything("--adversarial-rounds "+v, "invalid value", code, env)
	}
}
