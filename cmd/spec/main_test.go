package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"flag"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/agent-fox-dev/agentkit-go/provider/faux"

	"github.com/agent-fox-dev/spec/afspec"
	"github.com/agent-fox-dev/spec/envelope"
	"github.com/agent-fox-dev/spec/internal/agentrun/indextest"
	"github.com/agent-fox-dev/spec/internal/cli"
	"github.com/agent-fox-dev/spec/specgen"
)

const idea = "a library that does a thing"

// -h, --help and a bare invocation print spec's help to stderr: the sentence
// naming the sibling commands between the title and Usage:, and the promise
// that --dry-run writes no files. Only the bare invocation is a usage error,
// with an envelope on stdout.
func TestHelp_NamesTheSiblingsAndSaysADryRunWritesNoFiles(t *testing.T) {
	for _, tc := range []struct {
		name string
		argv []string
		code int
	}{
		{"-h", []string{"-h"}, cli.ExitOK},
		{"--help", []string{"--help"}, cli.ExitOK},
		{"bare invocation", nil, cli.ExitUsage},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cleanEnv(t)
			var stdout, stderr bytes.Buffer
			code := newApp().Main(t.Context(), tc.argv, strings.NewReader(""), &stdout, &stderr)
			if code != tc.code {
				t.Fatalf("code %d, want %d; stdout %q", code, tc.code, stdout.String())
			}
			if tc.code == cli.ExitOK && stdout.Len() != 0 {
				t.Errorf("help wrote to stdout: %q", stdout.String())
			}
			if tc.code == cli.ExitUsage {
				env, err := envelope.DecodeSpec(stdout.Bytes())
				if err != nil || env.Error == nil || env.Error.Category != "usage" {
					t.Errorf("envelope %+v (%v)", env.Error, err)
				}
			}
			help := stderr.String()
			siblingLine(t, tc.name, help)
			if flat := strings.Join(strings.Fields(help), " "); !strings.Contains(flat, "--dry-run changes nothing: it writes no files") {
				t.Errorf("the help does not say --dry-run writes no files:\n%s", help)
			}
		})
	}
}

// Every one of spec's flags, and the shared values the shell resolved, reach
// the options both the run and --preflight are built from.
func TestFlags_ReachTheRunsOptions(t *testing.T) {
	defaults := specgen.Options{Activate: true, MaxParallel: specgen.DefaultMaxParallel, AdversarialRounds: 1}
	input := cli.Input{Kind: cli.KindText, Origin: "argument", Body: "an idea"}
	for _, tc := range []struct {
		name   string
		args   []string
		common cli.Common
		want   func(o *specgen.Options)
	}{
		{"defaults", nil, cli.Common{}, func(*specgen.Options) {}},
		{"--adversarial alone reviews one round", []string{"--adversarial"}, cli.Common{},
			func(o *specgen.Options) { o.Adversarial = true }},
		{"--adversarial-rounds sets the rounds", []string{"--adversarial", "--adversarial-rounds", "3"}, cli.Common{},
			func(o *specgen.Options) { o.Adversarial, o.AdversarialRounds = true, 3 }},
		{"every other flag",
			[]string{"--specs-dir", "specs", "--name", "my_spec", "--architecture", "--no-activate", "--comment", "--max-parallel", "2"},
			cli.Common{},
			func(o *specgen.Options) {
				o.SpecsDir, o.Name, o.Architecture, o.Activate, o.Comment, o.MaxParallel = "specs", "my_spec", true, false, true, 2
			}},
		{"the shared flags", nil, cli.Common{DryRun: true, TotalBudgetUSD: 9, RepoMapTokens: 100},
			func(o *specgen.Options) { o.DryRun, o.TotalBudgetUSD, o.RepoMapTokens = true, 9, 100 }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := &specFlags{}
			fs := flag.NewFlagSet("spec", flag.ContinueOnError)
			f.register(fs)
			if err := fs.Parse(tc.args); err != nil {
				t.Fatal(err)
			}
			common := tc.common
			got := f.specOptions(cli.Deps{Common: &common, Input: input})
			want := defaults
			want.Input = input
			tc.want(&want)
			if !reflect.DeepEqual(got, want) {
				t.Errorf("options:\n got %+v\nwant %+v", got, want)
			}
			// The flag reads back only a count that was given.
			wantRounds := ""
			if i := slices.Index(tc.args, "--adversarial-rounds"); i >= 0 {
				wantRounds = tc.args[i+1]
			}
			if got := fs.Lookup("adversarial-rounds").Value.String(); got != wantRounds {
				t.Errorf("--adversarial-rounds reads back as %q, want %q", got, wantRounds)
			}
		})
	}
}

// refusalHarness runs spec with a forge API that counts every request and a
// scripted model that records every call, so a test can say that nothing was
// fetched and no model was called.
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

// A flag value that cannot hold is a usage error, with or without --preflight,
// before anything is fetched, any model called or any file written. The
// message must name the reason, so a refusal for another one (an undefined
// flag, say) does not pass for it.
func TestUsageErrors_AreRefusedBeforeAnythingRuns(t *testing.T) {
	const issue = "https://github.com/a/b/issues/1"
	for _, tc := range []struct {
		name    string
		args    []string
		input   string
		because string
	}{
		{"--max-parallel below one", []string{"--max-parallel", "0"}, issue, "--max-parallel must be at least 1"},
		{"--name that is not a spec name", []string{"--name", "Not-A-Name"}, issue, "must match"},
		{"--adversarial-rounds without --adversarial", []string{"--adversarial-rounds", "2"}, issue, "without --adversarial"},
		{"--adversarial-rounds 0", []string{"--adversarial", "--adversarial-rounds", "0"}, issue, "1 to 5"},
		{"--adversarial-rounds 6", []string{"--adversarial", "--adversarial-rounds", "6"}, issue, "1 to 5"},
		{"--adversarial-rounds -1", []string{"--adversarial", "--adversarial-rounds", "-1"}, issue, "1 to 5"},
		{"--adversarial-rounds abc", []string{"--adversarial", "--adversarial-rounds", "abc"}, issue, "invalid value"},
		{"--adversarial-rounds 1.5", []string{"--adversarial", "--adversarial-rounds", "1.5"}, issue, "invalid value"},
		{"--comment on an input that is no issue", []string{"--comment"}, idea, "--comment posts the PRD back"},
	} {
		for _, preflight := range []bool{false, true} {
			name := tc.name
			args := tc.args
			if preflight {
				name += " with --preflight"
				args = append([]string{"--preflight"}, args...)
			}
			t.Run(name, func(t *testing.T) {
				h := newRefusalHarness(t)
				argv := append(append([]string{"--dir", h.dir}, args...), tc.input)
				code, env := runSpecRaw(t, argv...)
				errInfo, _ := env["error"].(map[string]any)
				if code != cli.ExitUsage || errInfo["category"] != "usage" {
					t.Errorf("code %d, error %v", code, env["error"])
				}
				if msg, _ := errInfo["message"].(string); !strings.Contains(msg, tc.because) {
					t.Errorf("message %q does not say %q", msg, tc.because)
				}
				if n := h.fetches.Load(); n != 0 {
					t.Errorf("the input was fetched %d times", n)
				}
				if n := len(h.model.Requests()); n != 0 {
					t.Errorf("the model was called %d times", n)
				}
				if _, ok := env["usage"]; ok {
					t.Errorf("usage recorded phases: %v", env["usage"])
				}
				if _, err := os.Stat(filepath.Join(h.dir, ".specs")); err == nil {
					t.Error("a spec root was written")
				}
			})
		}
	}
}

// --preflight through the real shell: exit 0, stage preflight, the checklist
// and the estimate of four phases in the default view, no usage, and nothing
// written: no spec root, no side effect, no artifact but the envelope's own
// report and events files.
func TestPreflight_ReportsTheChecklistAndWritesNothing(t *testing.T) {
	cleanEnv(t)
	t.Setenv("ANTHROPIC_API_KEY", "test-key")
	dir := goRepo(t)

	code, env := runSpecRaw(t, "--preflight", "--dir", dir, idea)
	if code != cli.ExitOK {
		t.Fatalf("code = %d: %v", code, env)
	}
	res, _ := env["result"].(map[string]any)
	if res["stage"] != envelope.StagePreflight {
		t.Errorf("stage = %v", res["stage"])
	}
	if list, ok := res["preflight"].([]any); !ok || len(list) == 0 {
		t.Errorf("the default view dropped result.preflight: %v", res)
	}
	if est, ok := res["estimate"].(map[string]any); !ok || est["phases"] != float64(4) {
		t.Errorf("estimate = %v", res["estimate"])
	}
	if _, ok := env["usage"]; ok {
		t.Error("no phase ran, so the envelope must carry no usage")
	}
	if list, _ := env["side_effects"].([]any); len(list) != 0 {
		t.Errorf("side_effects = %v, want none", list)
	}
	arts, _ := env["artifacts"].([]any)
	for _, a := range arts {
		if kind := a.(map[string]any)["kind"]; kind != "report_file" && kind != "events_file" {
			t.Errorf("artifact %v, want only report_file or events_file", a)
		}
	}
	if v, ok := env["untrusted_fields"].([]any); ok && len(v) != 0 {
		t.Errorf("untrusted_fields = %v, want none", v)
	}
	if _, err := os.Stat(filepath.Join(dir, ".specs")); err == nil {
		t.Error("--preflight created a spec root")
	}
}

// An input that does not split is one phase, so --total-budget lowers its
// ceiling like --budget, below spec's own default of $5. --preflight reports
// the per-phase ceiling the Runner resolved, so the fold is observed where it
// takes effect.
func TestTotalBudget_LowersTheOnePhasesCeiling(t *testing.T) {
	cleanEnv(t)
	t.Setenv("ANTHROPIC_API_KEY", "test-key")
	dir := goRepo(t)
	for _, tc := range []struct {
		name string
		args []string
		want float64
	}{
		{"--budget above --total-budget", []string{"--budget", "10", "--total-budget", "4"}, 4},
		{"--total-budget above --budget", []string{"--budget", "4", "--total-budget", "10"}, 4},
		{"--total-budget alone, below the default", []string{"--total-budget", "1"}, 1},
		{"neither given: the default stands", nil, 5},
	} {
		t.Run(tc.name, func(t *testing.T) {
			argv := append(append([]string{"--preflight", "--dir", dir}, tc.args...), idea)
			code, env, stderr := runSpec(t, argv...)
			if code != cli.ExitOK || env.Result == nil || env.Result.Estimate == nil {
				t.Fatalf("code = %d: %+v\n%s", code, env.Error, stderr)
			}
			if got := env.Result.Estimate.MaxBudgetPerPhaseUSD; got != tc.want {
				t.Errorf("estimate.max_budget_per_phase_usd = %v, want %v", got, tc.want)
			}
		})
	}
}

// For every failure before the first model call, adding --preflight changes
// neither the error nor the exit code, and a refused --preflight reports no
// checklist or estimate: both are built from the same options and mapped onto
// the envelope the same way.
func TestPreflight_FailsAsTheRunDoesBeforeTheFirstModelCall(t *testing.T) {
	// ambiguousPlans leaves two unfinished splits that both match the input,
	// which the run refuses at its preflight stage.
	ambiguousPlans := func(t *testing.T, dir string) {
		sum := sha256.Sum256([]byte(idea))
		root := filepath.Join(dir, ".specs")
		if err := os.MkdirAll(root, 0o755); err != nil {
			t.Fatal(err)
		}
		for _, name := range []string{"one", "two"} {
			plan := map[string]any{
				"input":      map[string]any{"kind": "text", "origin": "argument", "sha256": hex.EncodeToString(sum[:])},
				"created_at": "2025-01-01T00:00:00Z",
				"updated_at": "2025-01-01T00:00:00Z",
				"scopes":     []map[string]any{{"name": name, "scope": "s"}, {"name": name + "_b", "scope": "t"}},
			}
			raw, _ := json.Marshal(plan)
			if err := os.WriteFile(filepath.Join(root, name+".split.json"), raw, 0o644); err != nil {
				t.Fatal(err)
			}
		}
	}

	for _, tc := range []struct {
		name  string
		key   string
		setup func(*testing.T, string)
		argv  []string
	}{
		{"unresolvable model", "test-key", nil, []string{"--model", "no-such-tier/at-all", idea}},
		{"missing model credential", "", nil, []string{idea}},
		{"ambiguous split plan", "test-key", ambiguousPlans, []string{idea}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cleanEnv(t)
			t.Setenv("ANTHROPIC_API_KEY", tc.key)
			dir := goRepo(t)
			if tc.setup != nil {
				tc.setup(t, dir)
			}
			base := append([]string{"--dir", dir}, tc.argv...)

			codeA, envA := runSpecRaw(t, base...)
			codeB, envB := runSpecRaw(t, append([]string{"--preflight"}, base...)...)
			if codeA == cli.ExitOK {
				t.Fatalf("the scenario does not fail: %v", envA)
			}
			if codeA != codeB {
				t.Errorf("exit code %d without --preflight, %d with", codeA, codeB)
			}
			a, _ := json.Marshal(envA["error"])
			b, _ := json.Marshal(envB["error"])
			if len(a) == 0 || string(a) == "null" {
				t.Fatalf("no error object: %v", envA)
			}
			if !bytes.Equal(a, b) {
				t.Errorf("error objects differ:\n%s\n%s", a, b)
			}
			if envA["status"] != envB["status"] {
				t.Errorf("status = %v vs %v", envA["status"], envB["status"])
			}
			if res, ok := envB["result"].(map[string]any); ok {
				for _, k := range []string{"preflight", "estimate"} {
					if _, has := res[k]; has {
						t.Errorf("a refused --preflight run carries %q: %v", k, res)
					}
				}
			}
		})
	}
}

// The run's code-search index is closed exactly once, whether the run
// succeeds (here, --preflight) or fails (here, at the PRD phase, which the
// unscripted model ends without a result), and never invalidated: spec does
// not change the tree it searches.
func TestTheRunsIndexIsClosedOnceAndNeverInvalidated(t *testing.T) {
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
			argv := append(append([]string{"--dir", goRepo(t)}, tc.args...), idea)
			code, env := runSpecRaw(t, argv...)
			if code != tc.code {
				t.Fatalf("code = %d, want %d\n%v", code, tc.code, env)
			}
			if n := idx.CloseCalls(); n != 1 {
				t.Errorf("Close was called %d times, want 1", n)
			}
			if n := idx.InvalidateCalls(); n != 0 {
				t.Errorf("Invalidate was called %d times, want 0", n)
			}
		})
	}
}

// The whole command against a scripted model: the real shell, the real
// pipeline, the real agent loop and submit handlers. The model writes the
// PRD, reads one file, then submits the three artifacts; spec writes,
// validates and activates the package and reports it.
func TestSpec_WritesAValidPackageFromAnIdea(t *testing.T) {
	cleanEnv(t)
	withIndex(t, nil)
	body, artifacts := fixture(t)
	turns := []faux.Turn{
		submit("p1", "list_files", map[string]any{}),
		submit("p2", "submit_prd", map[string]any{
			"spec_name": "test_feature", "title": "Test Feature", "body": body,
			"open_questions": []map[string]string{{"question": "Which store?", "decision": "The file store.", "why_unsure": "Not stated."}},
		}),
	}
	turns = append(turns, generation(t, artifacts)...)
	model := faux.New(turns...)
	t.Cleanup(cli.SetProvider(model))

	dir := goRepo(t)
	code, env, stderr := runSpec(t, "--dir", dir, "--repo-map-tokens", "0", "--detail", "full",
		"--phase-model", "test_spec=claude-haiku-4-5:low", "build the test feature")
	if code != cli.ExitOK || !env.OK || env.Result == nil {
		t.Fatalf("code = %d: %+v\n%s", code, env.Error, stderr)
	}
	r := env.Result
	if r.SpecID != "01" || r.SpecDir != filepath.Join(".specs", "01_test_feature") || r.Status != "active" || !r.Validation.Valid {
		t.Errorf("result = %+v", r)
	}
	if strings.Join(r.Artifacts, ",") != "prd.md,requirements.json,test_spec.json,tasks.json" {
		t.Errorf("artifacts = %v", r.Artifacts)
	}
	if r.Requirements == 0 || r.Tests == 0 || len(r.OpenQuestions) != 1 {
		t.Errorf("counts = %+v", r)
	}
	if len(env.Artifacts) != 1 || env.Artifacts[0].Kind != envelope.ArtifactSpecPackage || !env.Artifacts[0].Valid {
		t.Errorf("envelope artifacts = %+v", env.Artifacts)
	}
	if len(env.Next) != 1 || env.Next[0].Tool != "impl" || env.Next[0].Input != r.SpecDir {
		t.Errorf("next = %+v", env.Next)
	}
	if got := strings.Join(phaseNames(env), ","); got != "prd,generate:requirements,generate:test_spec,generate:tasks" {
		t.Errorf("phases = %v", got)
	}
	// STANDARD runs the PRD and the requirements at high effort and the rest
	// at medium; --phase-model moved the test spec to another model.
	var models []string
	for _, p := range env.Usage.Phases {
		models = append(models, p.Model+"/"+p.Effort)
	}
	if got := strings.Join(models, ","); got != "claude-sonnet-5-5/high,claude-sonnet-5-5/high,claude-haiku-4-5/low,claude-sonnet-5-5/medium" {
		t.Errorf("phase models = %s", got)
	}
	if env.Usage.Turns != len(turns) {
		t.Errorf("turns = %d, want %d", env.Usage.Turns, len(turns))
	}

	// The package on disk is the one reported, and it validates.
	spec, err := afspec.LoadSpec(filepath.Join(dir, r.SpecDir))
	if err != nil {
		t.Fatal(err)
	}
	if v := spec.Validate(); !v.Valid || spec.Status != "active" {
		t.Errorf("on disk: status %s, valid %v: %+v", spec.Status, v.Valid, v.Errors)
	}
	if !strings.Contains(spec.PRDBody, "## Intent") {
		t.Error("the PRD body was not written")
	}

	// Each phase was its own conversation: every generation phase started
	// from one user message, not the PRD phase's transcript.
	for i, req := range model.Requests() {
		if i == 0 || i == 2 {
			if n := len(req.Messages); n != 1 {
				t.Errorf("request %d starts a phase with %d messages, want 1", i, n)
			}
		}
	}
}

// With --adversarial the PRD is reviewed, revised, reviewed clean, and the
// package generated from the revision, through the real command, agent loop,
// review tools and validation.
func TestAdversarial_GeneratesThePackageFromTheRevisedPRD(t *testing.T) {
	cleanEnv(t)
	withIndex(t, nil)
	body, artifacts := fixture(t)
	revised := body + "\n\nREVISED: a missing spec package is reported by name.\n"
	finding := map[string]any{"severity": "major", "section": "Requirements", "flaw": "No error path for a missing package.",
		"evidence": "the PRD says nothing about it", "remedy": "say what happens", "reopens": ""}
	turns := []faux.Turn{
		submit("p1", "submit_prd", map[string]any{"spec_name": "test_feature", "title": "Test Feature", "body": body}),
		submit("r1", "submit_prd_review", map[string]any{"findings": []any{finding}}),
		submit("v1", "submit_prd_revision", map[string]any{"body": revised,
			"dispositions": []any{map[string]any{"id": "R1-F1", "disposition": "accepted", "reason": ""}}}),
		submit("r2", "submit_prd_review", map[string]any{"findings": []any{}}),
	}
	turns = append(turns, generation(t, artifacts)...)
	t.Cleanup(cli.SetProvider(faux.New(turns...)))

	dir := goRepo(t)
	code, env, stderr := runSpec(t, "--dir", dir, "--repo-map-tokens", "0", "--detail", "full",
		"--adversarial", "--adversarial-rounds", "2", "build the test feature")
	if code != cli.ExitOK || env.Result == nil {
		t.Fatalf("code = %d: %+v\n%s", code, env.Error, stderr)
	}
	if got := strings.Join(phaseNames(env), ","); got !=
		"prd,prd_review,prd_revision,prd_review,generate:requirements,generate:test_spec,generate:tasks" {
		t.Errorf("phases = %v", got)
	}
	r := env.Result
	if r.Review == nil || r.Review.RoundsRun != 2 || r.Review.RoundsAllowed != 2 ||
		r.Review.StopReason != envelope.ReviewStopNoFindings || r.Review.Rounds[0].Findings[0].ID != "R1-F1" ||
		r.Review.Rounds[0].Findings[0].Disposition != envelope.DispositionAccepted {
		t.Errorf("review = %+v", r.Review)
	}
	spec, err := afspec.LoadSpec(filepath.Join(dir, r.SpecDir))
	if err != nil {
		t.Fatal(err)
	}
	if v := spec.Validate(); !v.Valid || !strings.Contains(spec.PRDBody, "REVISED: a missing spec package") {
		t.Errorf("on disk: valid %v (%+v), revised body %v", v.Valid, v.Errors, strings.Contains(spec.PRDBody, "REVISED"))
	}
	if !strings.Contains(env.Summary, "review: 2 rounds, 1 finding, 1 accepted") {
		t.Errorf("summary = %q", env.Summary)
	}
}
