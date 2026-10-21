package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/agent-fox-dev/agentkit-go/core"
	"github.com/agent-fox-dev/agentkit-go/provider/faux"

	"github.com/agent-fox-dev/spec/afspec"
	"github.com/agent-fox-dev/spec/envelope"
	"github.com/agent-fox-dev/spec/internal/cli"
)

// fixture is afspec's valid package: its PRD body and its three artifacts,
// with the artifacts' $schema field left for the submit handlers to write.
func fixture(t *testing.T) (prdBody string, artifacts map[afspec.GenerationStep]string) {
	t.Helper()
	root := filepath.Join("..", "..", "afspec", "testdata", "valid_spec")
	raw, err := os.ReadFile(filepath.Join(root, "prd.md"))
	if err != nil {
		t.Fatal(err)
	}
	body := string(raw)
	if i := strings.Index(body[4:], "\n---\n"); i >= 0 {
		body = strings.TrimSpace(body[4+i+5:])
	}
	artifacts = map[afspec.GenerationStep]string{}
	for _, step := range afspec.GenerationSteps {
		b, err := os.ReadFile(filepath.Join(root, afspec.ArtifactFileName(step)))
		if err != nil {
			t.Fatal(err)
		}
		artifacts[step] = string(b)
	}
	return body, artifacts
}

func submit(id, tool string, args any) faux.Turn {
	raw, err := json.Marshal(args)
	if err != nil {
		panic(err)
	}
	return faux.Turn{Blocks: []core.ContentBlock{faux.FauxToolCall(id, tool, string(raw))}, StopReason: core.StopReasonToolUse}
}

// The whole command against a scripted model: the real shell, the real
// pipeline, the real agent loop and submit handlers. The model writes the
// PRD, reads one file, then submits the three artifacts; spec writes,
// validates and activates the package and reports it.
func TestSpecWritesAValidPackageEndToEnd(t *testing.T) {
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
	for i, step := range afspec.GenerationSteps {
		var content map[string]any
		if err := json.Unmarshal([]byte(artifacts[step]), &content); err != nil {
			t.Fatal(err)
		}
		turns = append(turns, submit("g"+string(rune('1'+i)), "submit_"+string(step), content))
	}
	model := faux.New(turns...)
	t.Cleanup(cli.SetProvider(model))

	dir := goRepo(t)
	var stdout, stderr bytes.Buffer
	code := newApp().Main(context.Background(), []string{"--dir", dir, "--repo-map-tokens", "0", "--detail", "full",
		"--phase-model", "test_spec=claude-haiku-4-5:low", "build the test feature"},
		strings.NewReader(""), &stdout, &stderr)
	// Read the output the way a program would: with the public decoder.
	env, err := envelope.DecodeSpec(stdout.Bytes())
	if err != nil {
		t.Fatalf("stdout is not one envelope: %v\n%s", err, stdout.String())
	}
	if code != cli.ExitOK || !env.OK || env.Result == nil {
		t.Fatalf("code = %d\n%s\n%s", code, stdout.String(), stderr.String())
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
	var phases []string
	for _, p := range env.Usage.Phases {
		phases = append(phases, p.Name)
	}
	if strings.Join(phases, ",") != "prd,generate:requirements,generate:test_spec,generate:tasks" {
		t.Errorf("phases = %v", phases)
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

// --dry-run runs the same phases and writes nothing.
func TestSpecDryRunWritesNothing(t *testing.T) {
	cleanEnv(t)
	withIndex(t, nil)
	body, artifacts := fixture(t)
	turns := []faux.Turn{submit("p", "submit_prd", map[string]any{"spec_name": "test_feature", "title": "Test Feature", "body": body})}
	for _, step := range afspec.GenerationSteps {
		var content map[string]any
		_ = json.Unmarshal([]byte(artifacts[step]), &content)
		turns = append(turns, submit("g"+string(step), "submit_"+string(step), content))
	}
	t.Cleanup(cli.SetProvider(faux.New(turns...)))

	dir := goRepo(t)
	var stdout bytes.Buffer
	code := newApp().Main(context.Background(), []string{"--dir", dir, "--dry-run", "--repo-map-tokens", "0", "build the test feature"},
		strings.NewReader(""), &stdout, &bytes.Buffer{})
	if code != cli.ExitOK {
		t.Fatalf("code = %d\n%s", code, stdout.String())
	}
	if _, err := os.Stat(filepath.Join(dir, ".specs")); !os.IsNotExist(err) {
		t.Errorf("--dry-run created the spec root: %v", err)
	}
	var env struct {
		Artifacts []struct {
			DryRun bool `json:"dry_run"`
		} `json:"artifacts"`
		Next []any `json:"next"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &env); err != nil {
		t.Fatal(err)
	}
	if len(env.Artifacts) != 1 || !env.Artifacts[0].DryRun || len(env.Next) != 0 {
		t.Errorf("artifacts = %+v, next = %+v", env.Artifacts, env.Next)
	}
}

// relabel is the fixture's artifact for another package: its ids carry the
// package's spec id and name.
func relabel(text, specID, specName string) map[string]any {
	text = strings.ReplaceAll(text, `"01-`, `"`+specID+`-`)
	text = strings.ReplaceAll(text, `TS-01-`, `TS-`+specID+`-`)
	text = strings.ReplaceAll(text, `"spec_id": "01"`, `"spec_id": "`+specID+`"`)
	text = strings.ReplaceAll(text, `"spec_name": "test_feature"`, `"spec_name": "`+specName+`"`)
	var content map[string]any
	if err := json.Unmarshal([]byte(text), &content); err != nil {
		panic(err)
	}
	return content
}

// generation is the three generation phases of one package, scripted from
// the fixture.
func generation(artifacts map[afspec.GenerationStep]string, specID, specName string) []faux.Turn {
	var turns []faux.Turn
	for _, step := range afspec.GenerationSteps {
		turns = append(turns, submit("g"+specID+string(step), "submit_"+string(step), relabel(artifacts[step], specID, specName)))
	}
	return turns
}

func finding(severity, flaw string) map[string]any {
	return map[string]any{"severity": severity, "section": "Requirements", "flaw": flaw,
		"evidence": "the PRD says nothing about it", "remedy": "say what happens", "reopens": ""}
}

// runSpec runs the real command and decodes its envelope.
func runSpec(t *testing.T, argv ...string) (int, envelope.SpecEnvelope, string) {
	t.Helper()
	var stdout, stderr bytes.Buffer
	code := newApp().Main(context.Background(), argv, strings.NewReader(""), &stdout, &stderr)
	env, err := envelope.DecodeSpec(stdout.Bytes())
	if err != nil {
		t.Fatalf("stdout is not one envelope: %v\n%s\n%s", err, stdout.String(), stderr.String())
	}
	return code, env, stderr.String()
}

func phaseNames(env envelope.SpecEnvelope) []string {
	var out []string
	if env.Usage != nil {
		for _, p := range env.Usage.Phases {
			out = append(out, p.Name)
		}
	}
	return out
}

// TS-09-65 (smoke): an undivided input is reviewed, revised, reviewed clean
// and generated from the revised PRD, through the real command, agent loop,
// tools and validation.
//
// Verifies: 09-PATH-1
func TestTS09_65_AnInputIsReviewedRevisedAndGeneratedFromTheRevision(t *testing.T) {
	cleanEnv(t)
	withIndex(t, nil)
	body, artifacts := fixture(t)
	revised := body + "\n\nREVISED: a missing spec package is reported by name.\n"
	turns := []faux.Turn{
		submit("p1", "submit_prd", map[string]any{"spec_name": "test_feature", "title": "Test Feature", "body": body}),
		submit("r1", "submit_prd_review", map[string]any{"findings": []any{finding("major", "No error path for a missing package.")}}),
		submit("v1", "submit_prd_revision", map[string]any{"body": revised,
			"dispositions": []any{map[string]any{"id": "R1-F1", "disposition": "accepted", "reason": ""}}}),
		submit("r2", "submit_prd_review", map[string]any{"findings": []any{}}),
	}
	turns = append(turns, generation(artifacts, "01", "test_feature")...)
	t.Cleanup(cli.SetProvider(faux.New(turns...)))

	dir := goRepo(t)
	code, env, stderr := runSpec(t, "--dir", dir, "--repo-map-tokens", "0", "--detail", "full",
		"--adversarial", "--adversarial-rounds", "2", "build the test feature")
	if code != cli.ExitOK || env.Result == nil {
		t.Fatalf("code = %d: %+v\n%s", code, env.Error, stderr)
	}
	if got := phaseNames(env); strings.Join(got, ",") !=
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

// TS-09-66 (smoke): each scope of a split is reviewed in the coordinator
// before its package starts, and every package carries its review.
//
// Verifies: 09-PATH-2, 09-REQ-8.2
func TestTS09_66_EveryScopeOfASplitIsReviewed(t *testing.T) {
	cleanEnv(t)
	withIndex(t, nil)
	body, artifacts := fixture(t)
	revised := body + "\n\nREVISED-SCOPE-1: the loader reports a missing package.\n"
	scopes := []map[string]any{
		{"name": "test_feature", "scope": "The loader."},
		{"name": "widget_store", "scope": "The store."},
		{"name": "widget_adopt", "scope": "The switch-over."},
	}
	// One package at a time, so the scripted turns are taken in this order.
	turns := []faux.Turn{
		submit("p1", "submit_prd", map[string]any{"spec_name": "test_feature", "title": "Test Feature", "body": body,
			"recommended_split": scopes, "codebase_notes": "ORIGINAL-NOTES"}),
		submit("r1", "submit_prd_review", map[string]any{"findings": []any{finding("minor", "Unclear wording.")}}),
		submit("v1", "submit_prd_revision", map[string]any{"body": revised, "codebase_notes": "REVISED-NOTES",
			"dispositions": []any{map[string]any{"id": "R1-F1", "disposition": "accepted", "reason": ""}}}),
	}
	turns = append(turns, generation(artifacts, "01", "test_feature")...)
	for i, name := range []string{"widget_store", "widget_adopt"} {
		id := fmt.Sprintf("0%d", i+2)
		turns = append(turns,
			submit("p"+id, "submit_prd", map[string]any{"spec_name": name, "title": "Scope " + name, "body": body}),
			submit("r"+id, "submit_prd_review", map[string]any{"findings": []any{}}))
		turns = append(turns, generation(artifacts, id, name)...)
	}
	model := faux.New(turns...)
	t.Cleanup(cli.SetProvider(model))

	dir := goRepo(t)
	code, env, stderr := runSpec(t, "--dir", dir, "--repo-map-tokens", "0", "--detail", "full",
		"--max-parallel", "1", "--adversarial", "a loader, a store and the switch-over")
	if code != cli.ExitOK || env.Result == nil {
		t.Fatalf("code = %d: %+v\n%s", code, env.Error, stderr)
	}
	r := env.Result
	pkgs := append([]envelope.Package{r.Package}, r.FollowOnSpecs...)
	if len(pkgs) != 3 {
		t.Fatalf("%d packages", len(pkgs))
	}
	for _, p := range pkgs {
		if p.Review == nil || p.Review.RoundsRun != 1 {
			t.Errorf("%s: review %+v", p.SpecName, p.Review)
		}
	}
	if !strings.Contains(env.Summary, "review: 3 rounds, 1 finding, 1 accepted") {
		t.Errorf("summary = %q", env.Summary)
	}
	// Scope 1's review came after the plan was recorded: its prompt shows the
	// split, which only a recorded plan supplies.
	reqs := model.Requests()
	var scope1Review, scope2PRD string
	for _, req := range reqs {
		text := userText(req)
		switch {
		case scope1Review == "" && strings.Contains(text, "Review the PRD below"):
			scope1Review = text
		case scope2PRD == "" && strings.Contains(text, "Write the PRD for scope 2"):
			scope2PRD = text
		}
	}
	if !strings.Contains(scope1Review, "This PRD covers scope 1 only") {
		t.Error("scope 1's review was not told its split")
	}
	if !strings.Contains(scope2PRD, "REVISED-SCOPE-1") || !strings.Contains(scope2PRD, "REVISED-NOTES") ||
		strings.Contains(scope2PRD, "ORIGINAL-NOTES") {
		t.Errorf("scope 2's PRD phase was not shown scope 1's revised PRD and notes:\n%s", scope2PRD)
	}
	for i, p := range pkgs {
		spec, err := afspec.LoadSpec(filepath.Join(dir, p.SpecDir))
		if err != nil || !spec.Validate().Valid {
			t.Errorf("package %d does not validate (%v)", i+1, err)
		}
		if i == 0 && !strings.Contains(spec.PRDBody, "REVISED-SCOPE-1") {
			t.Error("scope 1's prd.md is not its revised PRD")
		}
	}
}

// userText is the text of a request's first user message.
func userText(req core.Request) string {
	for _, m := range req.Messages {
		if um, ok := m.(core.UserMessage); ok {
			var b strings.Builder
			for _, blk := range um.Content {
				if tb, ok := blk.(core.TextBlock); ok {
					b.WriteString(tb.Text)
				}
			}
			return b.String()
		}
	}
	return ""
}

// TS-09-67 (smoke): a review that stops at its --budget leaves a high
// warning and a package generated from the PRD phase's PRD, with exit 0.
//
// Verifies: 09-PATH-3, 09-REQ-7.1
func TestTS09_67_AReviewOverBudgetKeepsThePackage(t *testing.T) {
	cleanEnv(t)
	withIndex(t, nil)
	body, artifacts := fixture(t)
	expensive := submit("r1", "list_files", map[string]any{})
	expensive.Usage = core.Usage{InputTokens: 2_000_000, OutputTokens: 10}
	turns := []faux.Turn{
		submit("p1", "submit_prd", map[string]any{"spec_name": "test_feature", "title": "Test Feature", "body": body}),
		expensive,
	}
	turns = append(turns, generation(artifacts, "01", "test_feature")...)
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
	if err != nil || normalize(spec.PRDBody) != normalize(body) || !spec.Validate().Valid {
		t.Errorf("the package is not the PRD phase's (%v)", err)
	}
}

func normalize(s string) string { return strings.TrimSpace(s) }

// TS-09-68 (smoke): --preflight counts the review phases and names their
// models, and calls no model.
//
// Verifies: 09-PATH-4, 09-REQ-10.3
func TestTS09_68_PreflightCountsTheReview(t *testing.T) {
	cleanEnv(t)
	withIndex(t, nil)
	t.Setenv("ANTHROPIC_API_KEY", "test-key")
	model := faux.New()
	t.Cleanup(cli.SetProvider(model))
	code, env, stderr := runSpec(t, "--dir", goRepo(t), "--preflight", "--adversarial", "--adversarial-rounds", "3",
		"build the test feature")
	if code != cli.ExitOK || env.Result == nil || env.Result.Estimate == nil {
		t.Fatalf("code = %d: %+v\n%s", code, env.Error, stderr)
	}
	if len(model.Requests()) != 0 {
		t.Errorf("preflight called the model %d times", len(model.Requests()))
	}
	est := env.Result.Estimate
	if est.Phases != 1+3+2*3 || est.MaxTotalUSD != float64(est.Phases)*est.MaxBudgetPerPhaseUSD {
		t.Errorf("estimate = %+v", est)
	}
	var detail string
	for _, c := range env.Result.Preflight {
		if c.Check == "phase_models" {
			detail = c.Detail
		}
	}
	if !strings.HasPrefix(detail, "prd claude-sonnet-5-5/high; prd_review claude-sonnet-5-5/high; prd_revision claude-sonnet-5-5/high; requirements ") {
		t.Errorf("phase_models = %q", detail)
	}
}
