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

	"github.com/agent-fox-dev/agentkit-go/core"
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
	if strings.Join(r.Artifacts, ",") != "prd.md,requirements.json,test_spec.json,tasks.json,spec.md" {
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
	finding := map[string]any{"severity": "major", "rubric": "prd", "section": "Requirements", "flaw": "No error path for a missing package.",
		"evidence": "the PRD says nothing about it", "remedy": "say what happens", "reopens": ""}
	turns := []faux.Turn{
		submit("p1", "submit_prd", map[string]any{"spec_name": "test_feature", "title": "Test Feature", "body": body}),
		submit("r1", "submit_prd_review", map[string]any{"findings": []any{finding}}),
		submit("v1", "submit_prd_revision", map[string]any{"body": revised,
			"dispositions": []any{map[string]any{"id": "R1-F1", "disposition": "fixed", "reason": "", "counter_evidence": "",
				"question": "", "options": []string{}, "decision": ""}}}),
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
		r.Review.StopReason != envelope.ReviewStopRoundsExhausted || r.Review.Rounds[0].Findings[0].ID != "R1-F1" ||
		r.Review.Rounds[0].Findings[0].Disposition != envelope.DispositionFixed {
		t.Errorf("review = %+v", r.Review)
	}
	spec, err := afspec.LoadSpec(filepath.Join(dir, r.SpecDir))
	if err != nil {
		t.Fatal(err)
	}
	if v := spec.Validate(); !v.Valid || !strings.Contains(spec.PRDBody, "REVISED: a missing spec package") {
		t.Errorf("on disk: valid %v (%+v), revised body %v", v.Valid, v.Errors, strings.Contains(spec.PRDBody, "REVISED"))
	}
	if !strings.Contains(env.Summary, "review: 2 rounds, 1 finding, 1 fixed") {
		t.Errorf("summary = %q", env.Summary)
	}
}

// A review that the real Runner stops at its --budget leaves a high warning
// naming the scope, the round, the phase and the budget, and the package is
// generated from the PRD phase's PRD, with exit 0.
func TestAdversarial_AReviewStoppedByTheBudgetKeepsThePackage(t *testing.T) {
	cleanEnv(t)
	withIndex(t, nil)
	body, artifacts := fixture(t)
	expensive := submit("r1", "list_files", map[string]any{})
	expensive.Usage = core.Usage{InputTokens: 2_000_000, OutputTokens: 10}
	turns := []faux.Turn{
		submit("p1", "submit_prd", map[string]any{"spec_name": "test_feature", "title": "Test Feature", "body": body}),
		expensive,
	}
	turns = append(turns, generation(t, artifacts)...)
	t.Cleanup(cli.SetProvider(faux.New(turns...)))

	dir := goRepo(t)
	code, env, stderr := runSpec(t, "--dir", dir, "--repo-map-tokens", "0", "--detail", "full",
		"--budget", "1", "--adversarial", "build the test feature")
	if code != cli.ExitOK || env.Result == nil {
		t.Fatalf("code = %d: %+v\n%s", code, env.Error, stderr)
	}
	var w *envelope.Warning
	for i := range env.Warnings {
		if env.Warnings[i].Code == envelope.WarnPRDReviewIncomplete {
			w = &env.Warnings[i]
		}
	}
	if w == nil || w.Severity != "high" {
		t.Fatalf("warnings = %+v", env.Warnings)
	}
	for _, want := range []string{"test_feature", "round 1", "prd_review", "budget"} {
		if !strings.Contains(w.Message, want) {
			t.Errorf("the warning does not name %q: %s", want, w.Message)
		}
	}
	if env.Result.Review == nil || env.Result.Review.StopReason != envelope.ReviewStopIncomplete {
		t.Errorf("review = %+v", env.Result.Review)
	}
	spec, err := afspec.LoadSpec(filepath.Join(dir, env.Result.SpecDir))
	if err != nil || strings.TrimSpace(spec.PRDBody) != strings.TrimSpace(body) || !spec.Validate().Valid {
		t.Errorf("the package is not the PRD phase's (%v)", err)
	}
}

// promptText is everything a request sent the model: its system prompt and
// the text of its messages.
func promptText(req core.Request) string {
	var b strings.Builder
	add := func(blocks []core.ContentBlock) {
		for _, block := range blocks {
			if t, ok := block.(core.TextBlock); ok {
				b.WriteString(t.Text)
			}
		}
	}
	add(req.System)
	for _, m := range req.Messages {
		if um, ok := m.(core.UserMessage); ok {
			add(um.Content)
		}
	}
	return b.String()
}

// runAgainstSteering runs spec end to end on the scripted model, in a
// repository whose spec root holds the steering file, and returns the result,
// the written prd.md and every prompt the model was sent.
func runAgainstSteering(t *testing.T, steering string) (envelope.SpecEnvelope, string, []string) {
	t.Helper()
	cleanEnv(t)
	withIndex(t, nil)
	body, artifacts := fixture(t)
	turns := append([]faux.Turn{submit("p1", "submit_prd", map[string]any{
		"spec_name": "test_feature", "title": "Test Feature", "body": body})}, generation(t, artifacts)...)
	model := faux.New(turns...)
	t.Cleanup(cli.SetProvider(model))

	dir := goRepo(t)
	if err := os.MkdirAll(filepath.Join(dir, ".specs"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, ".specs", "steering.md"), []byte(steering), 0o644); err != nil {
		t.Fatal(err)
	}
	code, env, stderr := runSpec(t, "--dir", dir, "--repo-map-tokens", "0", "--detail", "full", "build the test feature")
	if code != cli.ExitOK || env.Result == nil || !env.Result.Validation.Valid {
		t.Fatalf("code %d: %+v\n%s", code, env.Error, stderr)
	}
	prd, err := os.ReadFile(filepath.Join(dir, env.Result.SpecDir, "prd.md"))
	if err != nil {
		t.Fatal(err)
	}
	var prompts []string
	for _, req := range model.Requests() {
		prompts = append(prompts, promptText(req))
	}
	return env, string(prd), prompts
}

// A run against a versioned steering records the version in the package it
// writes, names it in every prompt, and sends none of the record.
func TestSpec_RecordsTheSteeringVersionItWasGeneratedAgainst(t *testing.T) {
	const steering = "---\nversion: 1.1.0\namendments:\n" +
		"  - version: 1.1.0\n    date: 2031-07-19\n    change: AMENDMENT-ONE added a directive.\n" +
		"  - version: 1.0.0\n    date: 2030-02-03\n    change: AMENDMENT-TWO was the baseline.\n" +
		"---\n# Steering\n\nDIRECTIVE: use make check.\n"
	env, prd, prompts := runAgainstSteering(t, steering)
	if !strings.Contains(prd, `steering_version: "1.1.0"`) {
		t.Errorf("prd.md does not record the steering version:\n%s", prd)
	}
	for _, w := range env.Result.Validation.Warnings {
		if w.Check == "steering_version" {
			t.Errorf("the run's verdict carries %+v; the versions are equal", w)
		}
	}
	for i, p := range prompts {
		if !strings.Contains(p, "Steering version 1.1.0") || !strings.Contains(p, "DIRECTIVE: use make check.") {
			t.Errorf("prompt %d lacks the version line or the directive", i)
		}
		for _, s := range []string{"2031-07-19", "2030-02-03", "AMENDMENT-ONE", "AMENDMENT-TWO"} {
			if strings.Contains(p, s) {
				t.Errorf("prompt %d carries the record: %q", i, s)
			}
		}
	}
}

// A run against a malformed record warns once, gives every phase the
// directives with no version, and writes no version.
func TestSpec_WarnsOnceAboutAMalformedSteeringAndWritesNoVersion(t *testing.T) {
	const steering = "---\nversion: 2.0.0\namendments:\n" +
		"  - version: 2.0.0\n    date: 2026-10-25\n    change: B.\n" +
		"  - version: 2.1.0\n    date: 2026-10-15\n    change: A mistyped version.\n" +
		"---\n# Steering\n\nDIRECTIVE: use make check.\n"
	env, prd, prompts := runAgainstSteering(t, steering)
	var malformed []envelope.Warning
	for _, w := range env.Warnings {
		if w.Code == envelope.WarnSteeringMalformed {
			malformed = append(malformed, w)
		}
	}
	if len(malformed) != 1 || malformed[0].Severity != "high" || malformed[0].Stage != "preflight" ||
		!strings.Contains(malformed[0].Message, "strictly decrease") {
		t.Errorf("warnings %+v; want one high steering_malformed at preflight naming the broken rule", env.Warnings)
	}
	if strings.Contains(prd, "steering_version") {
		t.Errorf("prd.md records a steering version:\n%s", prd)
	}
	for i, p := range prompts {
		if !strings.Contains(p, "DIRECTIVE: use make check.") || strings.Contains(p, "Steering version") {
			t.Errorf("prompt %d: want the directive and no version line", i)
		}
	}
}

// reviewFinding is one finding as the reviewer submits it.
func reviewFinding(severity, rubric, section, flaw, evidence string) map[string]any {
	return map[string]any{"severity": severity, "rubric": rubric, "section": section, "flaw": flaw,
		"evidence": evidence, "remedy": "say what happens", "reopens": ""}
}

// revisionAnswer is one answer as the reviser submits it.
func revisionAnswer(id, disposition string, fields map[string]any) map[string]any {
	a := map[string]any{"id": id, "disposition": disposition, "reason": "", "counter_evidence": "",
		"question": "", "options": []string{}, "decision": ""}
	for k, v := range fields {
		a[k] = v
	}
	return a
}

// adversarialRun runs spec end to end on the scripted model with the review,
// and returns the result and the model.
func adversarialRun(t *testing.T, dir string, rounds string, review ...faux.Turn) (envelope.SpecEnvelope, *faux.Provider) {
	t.Helper()
	body, artifacts := fixture(t)
	turns := append([]faux.Turn{submit("p1", "submit_prd", map[string]any{"spec_name": "test_feature", "title": "Test Feature", "body": body})}, review...)
	model := faux.New(append(turns, generation(t, artifacts)...)...)
	t.Cleanup(cli.SetProvider(model))
	code, env, stderr := runSpec(t, "--dir", dir, "--repo-map-tokens", "0", "--detail", "full", "--adversarial", "--adversarial-rounds", rounds,
		"build the test feature")
	if code != cli.ExitOK || env.Result == nil || !env.Result.Validation.Valid {
		t.Fatalf("code = %d: %+v\n%s", code, env.Error, stderr)
	}
	return env, model
}

// A major finding fixed, a minor one fixed, then nothing: the second and third
// rounds are clean and the rounds converge, with no revision for the third.
func TestAdversarial_TwoConsecutiveCleanRoundsConverge(t *testing.T) {
	cleanEnv(t)
	withIndex(t, nil)
	body, _ := fixture(t)
	env, _ := adversarialRun(t, goRepo(t), "3",
		submit("r1", "submit_prd_review", map[string]any{"findings": []any{
			reviewFinding("major", "prd", "Requirements", "No error path for a missing package.", "the PRD says nothing about it")}}),
		submit("v1", "submit_prd_revision", map[string]any{"body": body + "\nREVISED-1: a missing package is reported.\n",
			"dispositions": []any{revisionAnswer("R1-F1", "fixed", nil)}}),
		submit("r2", "submit_prd_review", map[string]any{"findings": []any{
			reviewFinding("minor", "prd", "Goals", "A goal is worded loosely.", "the PRD says load quickly")}}),
		submit("v2", "submit_prd_revision", map[string]any{"body": body + "\nREVISED-1: a missing package is reported.\nREVISED-2\n",
			"dispositions": []any{revisionAnswer("R2-F1", "fixed", nil)}}),
		submit("r3", "submit_prd_review", map[string]any{"findings": []any{}}),
	)
	r := env.Result.Review
	if r == nil || r.StopReason != envelope.ReviewStopConverged || r.RoundsRun != 3 {
		t.Fatalf("review = %+v", r)
	}
	if got := []bool{r.Rounds[0].Clean, r.Rounds[1].Clean, r.Rounds[2].Clean}; !slices.Equal(got, []bool{false, true, true}) {
		t.Errorf("clean = %v; want false, true, true", got)
	}
	if got := strings.Join(phaseNames(env), ","); got !=
		"prd,prd_review,prd_revision,prd_review,prd_revision,prd_review,generate:requirements,generate:test_spec,generate:tasks" {
		t.Errorf("phases = %v; want no revision for the third round", got)
	}
	if !strings.Contains(env.Summary, "review: 3 rounds, 2 findings, 2 fixed") {
		t.Errorf("summary = %q", env.Summary)
	}
}

// An escalated finding is decided provisionally in the PRD, and its question
// reaches the caller.
func TestAdversarial_AnEscalationReachesTheCaller(t *testing.T) {
	cleanEnv(t)
	withIndex(t, nil)
	body, _ := fixture(t)
	const decision = "A missing package is skipped with a warning."
	revised := body + "\n## Design Decisions\n\n9. Provisional: " + decision + "\n"
	dir := goRepo(t)
	env, _ := adversarialRun(t, dir, "1",
		submit("r1", "submit_prd_review", map[string]any{"findings": []any{
			reviewFinding("major", "prd", "Requirements", "What a missing package does is undecided.", "the PRD says nothing about it")}}),
		submit("v1", "submit_prd_revision", map[string]any{"body": revised, "dispositions": []any{revisionAnswer("R1-F1", "escalated", map[string]any{
			"question": "Should a missing package fail the run?",
			"options":  []string{"Fail the run.", "Skip it with a warning.", "Ask the caller."},
			"decision": decision, "reason": "the input does not say which a caller wants"})}}),
	)
	f := env.Result.Review.Rounds[0].Findings[0]
	if f.Disposition != envelope.DispositionEscalated || f.Question == "" || len(f.Options) != 3 || f.Decision != decision || f.Reason == "" {
		t.Errorf("finding = %+v", f)
	}
	found := false
	for _, q := range env.Result.OpenQuestions {
		found = found || (q.Question == "Should a missing package fail the run?" && q.Decision == decision && q.Why != "")
	}
	if !found {
		t.Errorf("open questions %+v lack the escalation", env.Result.OpenQuestions)
	}
	prd, err := os.ReadFile(filepath.Join(dir, env.Result.SpecDir, "prd.md"))
	if err != nil || !strings.Contains(string(prd), "Provisional: "+decision) {
		t.Errorf("the written prd.md lacks the provisional decision (%v)", err)
	}
}

// A PRD that contradicts a steering directive gets a steering finding, which
// the reviser may not decline: the refusal reaches the model as a tool error,
// and the escalation it submits instead is recorded and handed on.
func TestAdversarial_ASteeringFindingCannotBeDeclined(t *testing.T) {
	cleanEnv(t)
	withIndex(t, nil)
	body, _ := fixture(t)
	const directive = "Every command writes exactly one JSON object to stdout."
	dir := goRepo(t)
	if err := os.MkdirAll(filepath.Join(dir, ".specs"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, ".specs", "steering.md"), []byte("# Steering\n\n- "+directive+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	const decision = "The loader writes its report as one JSON object."
	env, model := adversarialRun(t, dir, "1",
		submit("r1", "submit_prd_review", map[string]any{"findings": []any{reviewFinding("major", "steering", "Requirements",
			"The PRD asks for a text report on stdout.", `"`+directive+`" against "print a table of the packages"`)}}),
		submit("v1", "submit_prd_revision", map[string]any{"body": body, "dispositions": []any{
			revisionAnswer("R1-F1", "declined", map[string]any{"reason": "out of scope"})}}),
		submit("v2", "submit_prd_revision", map[string]any{"body": body + "\n## Design Decisions\n\n9. Provisional: " + decision + "\n",
			"dispositions": []any{revisionAnswer("R1-F1", "escalated", map[string]any{
				"question": "Should the loader print a table, which the steering forbids?",
				"options":  []string{"Write JSON only.", "Amend the steering."},
				"decision": decision, "reason": "the input asks for a table"})}}),
	)
	refused := false
	for _, req := range model.Requests() {
		b, _ := json.Marshal(req.Messages)
		refused = refused || strings.Contains(string(b), "cannot be declined")
	}
	if !refused {
		t.Error("the declined steering finding was not turned back to the model as a tool error")
	}
	f := env.Result.Review.Rounds[0].Findings[0]
	if f.Rubric != envelope.RubricSteering || f.Disposition != envelope.DispositionEscalated {
		t.Errorf("finding = %+v; want a steering finding, escalated", f)
	}
	if !strings.Contains(env.Summary, "review: 1 round, 1 finding (1 steering), 0 fixed") {
		t.Errorf("summary = %q", env.Summary)
	}
	found := false
	for _, q := range env.Result.OpenQuestions {
		found = found || strings.Contains(q.Question, "which the steering forbids")
	}
	if !found {
		t.Errorf("open questions %+v lack the escalation", env.Result.OpenQuestions)
	}
}
