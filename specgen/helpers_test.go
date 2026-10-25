package specgen

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/agent-fox-dev/agentkit-go/core"
	"github.com/agent-fox-dev/agentkit-go/provider/faux"
	"github.com/agent-fox-dev/agentkit-go/tools"
	"github.com/agent-fox-dev/agentkit-go/workspace"

	"github.com/agent-fox-dev/spec/afspec"
	"github.com/agent-fox-dev/spec/envelope"
	"github.com/agent-fox-dev/spec/internal/agentrun"
	"github.com/agent-fox-dev/spec/internal/cli"
	"github.com/agent-fox-dev/spec/internal/issuex"
)

// The tests drive the real pipeline — the scaffolding, afspec's validation,
// the lifecycle transition, the agent loop and the submit handlers — with only
// the model replaced: by a scripted author for the pipeline's own bookkeeping,
// or by a scripted provider where what the model is sent and sends back is the
// point. The artifacts come from afspec's valid fixture, so a pipeline that
// mangles them fails here rather than in production.

// fixtureDir is afspec's valid version 2 package.
var fixtureDir = filepath.Join("..", "afspec", "testdata", "valid_spec")

const fixtureSpecID = "01"

// loadFixture reads the fixture's artifacts relabelled with the id and name
// the pipeline will assign, so the rule that ids agree everywhere is
// exercised rather than sidestepped, and returns them with its PRD body.
func loadFixture(t *testing.T, specID, specName string) (map[afspec.GenerationStep]map[string]any, string) {
	t.Helper()
	artifacts := map[afspec.GenerationStep]map[string]any{}
	for _, step := range afspec.GenerationSteps {
		raw, err := os.ReadFile(filepath.Join(fixtureDir, afspec.ArtifactFileName(step)))
		if err != nil {
			t.Fatal(err)
		}
		text := strings.ReplaceAll(string(raw), `"`+fixtureSpecID+`-`, `"`+specID+`-`)
		text = strings.ReplaceAll(text, `TS-`+fixtureSpecID+`-`, `TS-`+specID+`-`)
		text = strings.ReplaceAll(text, `"spec_id": "`+fixtureSpecID+`"`, `"spec_id": "`+specID+`"`)
		text = strings.ReplaceAll(text, `"spec_name": "test_feature"`, `"spec_name": "`+specName+`"`)
		var content map[string]any
		if err := json.Unmarshal([]byte(text), &content); err != nil {
			t.Fatalf("%s: %v", step, err)
		}
		artifacts[step] = content
	}
	prd, err := os.ReadFile(filepath.Join(fixtureDir, "prd.md"))
	if err != nil {
		t.Fatal(err)
	}
	body := string(prd)
	if i := strings.Index(body[4:], "\n---\n"); i >= 0 {
		body = strings.TrimSpace(body[4+i+5:])
	}
	return artifacts, body
}

// copyFixture copies the fixture package into dir as it is on disk.
func copyFixture(t *testing.T, dir string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, f := range []string{"prd.md", "requirements.json", "test_spec.json", "tasks.json"} {
		b, err := os.ReadFile(filepath.Join(fixtureDir, f))
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, f), b, 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

// acceptedArtifacts runs steps through the submit handler's checks into a
// partial spec, as the generation phases before the one under test would.
func acceptedArtifacts(t *testing.T, artifacts map[afspec.GenerationStep]map[string]any,
	steps ...afspec.GenerationStep) *afspec.PartialSpec {
	t.Helper()
	partial := &afspec.PartialSpec{SpecID: "01", SpecName: "test_feature"}
	for _, step := range steps {
		if err := validateArtifactContent(artifacts[step], step, partial); err != nil {
			t.Fatal(err)
		}
	}
	return partial
}

// scriptedAuthor stands in for the model phases of the author interface and
// records every request it is sent.
type scriptedAuthor struct {
	t         *testing.T
	prd       PRD
	artifacts map[afspec.GenerationStep]map[string]any
	// primary is the "NN_name" the artifacts above were relabelled for. A
	// package with another id or name — a later scope of a split — gets the
	// fixture relabelled for it on demand.
	primary string
	arch    string

	prdErr      error
	artifactErr map[afspec.GenerationStep]error
	// skipStepValidation submits an artifact without the per-step check,
	// standing in for a violation only whole-package validation can see.
	skipStepValidation bool
	// breakScope names the split scope whose tasks artifact is submitted
	// with one test unowned, so that package does not validate.
	breakScope string

	// followOn scripts the PRD phase for one scope of a split, by scope
	// name. A scope with no script gets the fixture PRD under the planned
	// name; one in followOnErr fails instead.
	followOn    map[string]PRD
	followOnErr map[string]error

	// shapes bends the fixture for the packages it names, by spec name.
	shapes map[string]packageShape

	// prdCost is the spend every PRD phase reports.
	prdCost float64
	// onGenerate, when set, is called at the start of every generation
	// phase, to observe what the pipeline has done before it.
	onGenerate func(req artifactRequest)

	steps        []afspec.GenerationStep
	prdRequests  []prdRequest
	artifactReqs []artifactRequest
	archReqs     []architectureRequest
}

func (a *scriptedAuthor) WritePRD(_ context.Context, req prdRequest) (PRD, agentrun.Result, error) {
	a.prdRequests = append(a.prdRequests, req)
	res := agentrun.Result{Name: "prd", Turns: 3, ToolCalls: map[string]int{"read_file": 2, ToolSubmitPRD: 1}}
	res.Usage.CostUSD = a.prdCost
	if req.Split == nil {
		return a.prd, res, a.prdErr
	}
	name := req.Split.Scope().Name
	if err := a.followOnErr[name]; err != nil {
		return PRD{}, res, err
	}
	if prd, ok := a.followOn[name]; ok {
		return prd, res, nil
	}
	prd := a.prd
	prd.SpecName, prd.Title, prd.RecommendedSplit = name, "Scope "+name, nil
	return prd, res, nil
}

func (a *scriptedAuthor) GenerateArtifact(_ context.Context, req artifactRequest) (map[string]any, agentrun.Result, error) {
	a.steps = append(a.steps, req.Step)
	a.artifactReqs = append(a.artifactReqs, req)
	if a.onGenerate != nil {
		a.onGenerate(req)
	}
	res := agentrun.Result{Name: "generate:" + string(req.Step), Turns: 2,
		ToolCalls: map[string]int{"read_file": 3, ArtifactToolName(req.Step): 1}}
	if err := a.artifactErr[req.Step]; err != nil {
		return nil, res, err
	}
	content := a.artifacts[req.Step]
	if req.SpecID+"_"+req.SpecName != a.primary {
		fixtures, _ := loadFixture(a.t, req.SpecID, req.SpecName)
		content = fixtures[req.Step]
	}
	if s, ok := a.shapes[req.SpecName]; ok {
		s.apply(req.Step, content)
	}
	skip := a.skipStepValidation
	if a.breakScope != "" && req.SpecName == a.breakScope && req.Step == afspec.StepTasks {
		dropOneOwnedTest(content)
		skip = true
	}
	// The pipeline validates through the same handler the tool uses, so the
	// scripted author goes through it too rather than around it.
	if skip {
		decoded, err := afspec.DecodeArtifact(req.Step, content)
		if err != nil {
			return nil, res, err
		}
		switch v := decoded.(type) {
		case *afspec.RequirementsV2Json:
			req.Partial.Requirements = v
		case *afspec.TestSpecV2Json:
			req.Partial.TestSpec = v
		case *afspec.TasksV2Json:
			req.Partial.Tasks = v
		}
		return content, res, nil
	}
	if err := validateArtifactContent(content, req.Step, req.Partial); err != nil {
		return nil, res, err
	}
	return content, res, nil
}

func (a *scriptedAuthor) WriteArchitecture(_ context.Context, req architectureRequest) (string, agentrun.Result, error) {
	a.archReqs = append(a.archReqs, req)
	if a.arch == "" {
		return "", agentrun.Result{Name: "architecture"}, errors.New("no architecture scripted")
	}
	return a.arch, agentrun.Result{Name: "architecture", Turns: 2}, nil
}

// dropOneOwnedTest breaks the rule that every test is owned by a task: task
// 1 owns the first tests, and dropping one leaves that test owned by nothing.
func dropOneOwnedTest(tasksContent map[string]any) {
	tasks := tasksContent["tasks"].([]any)
	first := tasks[0].(map[string]any)
	tests := first["tests"].([]any)
	first["tests"] = tests[:len(tests)-1]
}

func newAuthor(t *testing.T, specID, specName string) *scriptedAuthor {
	t.Helper()
	artifacts, body := loadFixture(t, specID, specName)
	return &scriptedAuthor{
		t: t,
		prd: PRD{
			SpecName: specName,
			Title:    "Test Feature",
			Body:     body,
			OpenQuestions: []OpenQuestion{{
				Question: "Should the loader follow symlinks?",
				Decision: "Yes, matching the rest of the project.",
				Why:      "No existing code settles it either way.",
			}},
		},
		artifacts:   artifacts,
		primary:     specID + "_" + specName,
		artifactErr: map[afspec.GenerationStep]error{},
		followOn:    map[string]PRD{},
		followOnErr: map[string]error{},
	}
}

func threeScopes() []SplitScope {
	return []SplitScope{
		{Name: "widget_core", Scope: "The widget model and its loader."},
		{Name: "widget_github", Scope: "The GitHub-backed widget store."},
		{Name: "widget_adopt", Scope: "Switching the tools over to the new store."},
	}
}

// splitAuthor answers the first PRD phase with a split into threeScopes.
func splitAuthor(t *testing.T) *scriptedAuthor {
	t.Helper()
	a := newAuthor(t, "01", "widget_core")
	a.prd.RecommendedSplit = threeScopes()
	return a
}

// packageShape is how a test bends the fixture for one package.
type packageShape struct {
	// dependsOn is the spec id its tasks declare a dependency on, or "".
	dependsOn string
	// actor, when set, is the actor of every step of its execution paths.
	actor string
}

func (s packageShape) apply(step afspec.GenerationStep, content map[string]any) {
	switch step {
	case afspec.StepRequirements:
		if s.actor == "" {
			return
		}
		for _, p := range content["execution_paths"].([]any) {
			for _, st := range p.(map[string]any)["steps"].([]any) {
				st.(map[string]any)["actor"] = s.actor
			}
		}
	case afspec.StepTasks:
		if s.dependsOn != "" {
			content["dependencies"] = []any{map[string]any{"spec": s.dependsOn, "reason": "builds on it"}}
		}
	}
}

// concurrentAuthor makes a scriptedAuthor safe to call from the goroutines a
// pipelined split runs: calls to it are serialised, and the hooks run
// outside the lock, so a hook may block to force one phase to overlap
// another.
type concurrentAuthor struct {
	mu    sync.Mutex
	inner *scriptedAuthor

	beforePRD      func(req prdRequest)
	afterPRD       func(req prdRequest)
	beforeGenerate func(req artifactRequest)
	afterGenerate  func(req artifactRequest)
	// failGenerate fails every generation phase of the named spec.
	failGenerate map[string]error

	events []string
}

func (a *concurrentAuthor) log(e string) {
	a.mu.Lock()
	a.events = append(a.events, e)
	a.mu.Unlock()
}

func (a *concurrentAuthor) WritePRD(ctx context.Context, req prdRequest) (PRD, agentrun.Result, error) {
	name := "first"
	if req.Split != nil {
		name = req.Split.Scope().Name
	}
	a.log("prd " + name)
	if a.beforePRD != nil {
		a.beforePRD(req)
	}
	a.mu.Lock()
	prd, res, err := a.inner.WritePRD(ctx, req)
	a.mu.Unlock()
	if a.afterPRD != nil {
		a.afterPRD(req)
	}
	return prd, res, err
}

func (a *concurrentAuthor) GenerateArtifact(ctx context.Context, req artifactRequest) (map[string]any, agentrun.Result, error) {
	a.log("gen " + req.SpecName + " " + string(req.Step))
	if a.beforeGenerate != nil {
		a.beforeGenerate(req)
	}
	if err := a.failGenerate[req.SpecName]; err != nil {
		return nil, agentrun.Result{Name: "generate:" + string(req.Step)}, err
	}
	a.mu.Lock()
	content, res, err := a.inner.GenerateArtifact(ctx, req)
	a.mu.Unlock()
	if a.afterGenerate != nil {
		a.afterGenerate(req)
	}
	return content, res, err
}

func (a *concurrentAuthor) WriteArchitecture(ctx context.Context, req architectureRequest) (string, agentrun.Result, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.inner.WriteArchitecture(ctx, req)
}

func (a *concurrentAuthor) indexOf(event string) int {
	a.mu.Lock()
	defer a.mu.Unlock()
	return slices.Index(a.events, event)
}

// wait blocks until ch is closed. The deadline is a deadlock guard, not a
// performance bound: an implementation that never overlaps the phases would
// otherwise hang the test instead of failing it.
func wait(t *testing.T, ch <-chan struct{}, what string) {
	t.Helper()
	select {
	case <-ch:
	case <-time.After(20 * time.Second):
		t.Errorf("gave up waiting for %s: the phases did not overlap", what)
	}
}

// reviewOutcome is what one scripted review does: fail, or raise findings of
// these severities (none for an empty review).
type reviewOutcome struct {
	severities []string
	err        error
	// cost is what the phase reports spending.
	cost float64
	// mark, when set, is written into every finding's flaw and evidence.
	mark string
}

// revisionOutcome is what one scripted revision does: fail, or accept every
// finding but the ones listed in reject.
type revisionOutcome struct {
	reject []int
	err    error
	cost   float64
	// edit, when set, makes the revised PRD from the one under revision in
	// place of appending a line to its body.
	edit func(PRD) PRD
}

// fakeModel is the model a scripted review phase reports it ran on.
var fakeModel = agentrun.Model{Spec: "fake", Row: core.Model{ID: "fake-model"}, Effort: core.EffortHigh}

// scriptedReviewer stands in for the review phases, one outcome per call,
// and records every request it was sent.
type scriptedReviewer struct {
	t         *testing.T
	reviews   []reviewOutcome
	revisions []revisionOutcome
	// onReview and onRevise, when set, run at the start of each call.
	onReview func(req reviewRequest)
	onRevise func(req revisionRequest)

	mu         sync.Mutex
	reviewReqs []reviewRequest
	reviseReqs []revisionRequest
}

func (s *scriptedReviewer) Review(_ context.Context, req reviewRequest) ([]Finding, agentrun.Result, error) {
	s.mu.Lock()
	i := len(s.reviewReqs)
	s.reviewReqs = append(s.reviewReqs, req)
	s.mu.Unlock()
	if s.onReview != nil {
		s.onReview(req)
	}
	if i >= len(s.reviews) {
		s.t.Fatalf("review %d was not scripted", i+1)
	}
	out := s.reviews[i]
	res := agentrun.Result{Name: agentrun.PhasePRDReview, Turns: 2, Model: fakeModel}
	res.Usage.CostUSD = out.cost
	if out.err != nil {
		return nil, res, out.err
	}
	findings := make([]Finding, len(out.severities))
	for n, sev := range out.severities {
		findings[n] = Finding{ID: fmt.Sprintf("R%d-F%d", req.Round, n+1), Severity: sev,
			Section: "Goals", Flaw: fmt.Sprintf("flaw %d.%d%s", req.Round, n+1, out.mark), Evidence: "e" + out.mark, Remedy: "r"}
	}
	return findings, res, nil
}

func (s *scriptedReviewer) Revise(_ context.Context, req revisionRequest) (PRD, []Disposition, agentrun.Result, error) {
	s.mu.Lock()
	i := len(s.reviseReqs)
	s.reviseReqs = append(s.reviseReqs, req)
	s.mu.Unlock()
	if s.onRevise != nil {
		s.onRevise(req)
	}
	if i >= len(s.revisions) {
		s.t.Fatalf("revision %d was not scripted", i+1)
	}
	out := s.revisions[i]
	res := agentrun.Result{Name: agentrun.PhasePRDRevision, Turns: 3, Model: fakeModel}
	res.Usage.CostUSD = out.cost
	if out.err != nil {
		return PRD{}, nil, res, out.err
	}
	var ds []Disposition
	for n, f := range req.Findings {
		d := Disposition{ID: f.ID, Disposition: envelope.DispositionAccepted}
		if slices.Contains(out.reject, n) {
			d = Disposition{ID: f.ID, Disposition: envelope.DispositionRejected, Reason: "REASON-" + f.ID}
		}
		ds = append(ds, d)
	}
	revised := req.PRD
	revised.Body = fmt.Sprintf("%s\nrevised in round %d\n", req.PRD.Body, req.Round)
	if out.edit != nil {
		revised = out.edit(req.PRD)
	}
	return revised, ds, res, nil
}

func (s *scriptedReviewer) counts() (reviews, revisions int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.reviewReqs), len(s.reviseReqs)
}

// reviewedNames are the spec names of the PRDs the reviewer was sent, in order.
func (s *scriptedReviewer) reviewedNames() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []string
	for _, r := range s.reviewReqs {
		out = append(out, r.PRD.SpecName)
	}
	return out
}

// validPRDArgs is a submit_prd payload the tool accepts.
func validPRDArgs() map[string]any {
	return map[string]any{
		"spec_name": "widget_counter",
		"title":     "Widget Counter",
		"body": "# Widget Counter\n\n## Intent\n\nCount widgets once per request.\n\n" +
			"## Goals\n\n- Count accurately.\n\n## Non-goals\n\n- Reporting.\n",
	}
}

// roundFindings are well-formed findings with these ids.
func roundFindings(ids ...string) []Finding {
	out := make([]Finding, len(ids))
	for i, id := range ids {
		out[i] = Finding{ID: id, Severity: "major", Section: "Goals", Flaw: "f " + id, Evidence: "e", Remedy: "r"}
	}
	return out
}

// accept and reject are dispositions as the reviser submits them.
func accept(id string) map[string]string {
	return map[string]string{"id": id, "disposition": "accepted", "reason": ""}
}

func reject(id, why string) map[string]string {
	return map[string]string{"id": id, "disposition": "rejected", "reason": why}
}

// scopeTwoOfThree is a split context for the second of three scopes, the
// first scope's PRD and notes held as this run's draft.
func scopeTwoOfThree() *splitContext {
	plan := &SplitPlan{Scopes: []PlanScope{
		{Name: "widget_store", Scope: "STORE-SCOPE the store"},
		{Name: "widget_cache", Scope: "CACHE-SCOPE the cache"},
		{Name: "widget_evict", Scope: "EVICT-SCOPE the eviction"},
	}}
	return &splitContext{Plan: plan, Index: 1, SpecRoot: ".specs",
		Drafts: map[int]PRD{0: {SpecName: "widget_store",
			Body: "## Intent\n\nDELIVERS-MARK a store.\n\n## Goals\n\n- store\n"}},
		Notes: map[int]string{0: "EARLIER-NOTES-MARK"},
	}
}

// cleanReviews is n reviews that find nothing.
func cleanReviews(n int) []reviewOutcome { return make([]reviewOutcome, n) }

// phaseErr is a phase failure of the given category, as the Runner reports
// one.
func phaseErr(phase, category string) error {
	return &agentrun.Error{Phase: phase, Detail: "stopped: " + category, Cat: category}
}

// failingReviewer fails the test if any review phase runs.
type failingReviewer struct{ t *testing.T }

func (f failingReviewer) Review(context.Context, reviewRequest) ([]Finding, agentrun.Result, error) {
	f.t.Fatal("a review ran")
	return nil, agentrun.Result{}, nil
}

func (f failingReviewer) Revise(context.Context, revisionRequest) (PRD, []Disposition, agentrun.Result, error) {
	f.t.Fatal("a revision ran")
	return PRD{}, nil, agentrun.Result{}, nil
}

// newWorkspace is an empty Go module.
func newWorkspace(t *testing.T) *workspace.Workspace {
	t.Helper()
	dir := t.TempDir()
	writeFiles(t, dir, map[string]string{"go.mod": "module example.com/x\n\ngo 1.26\n"})
	ws, err := workspace.Open(dir, workspace.Options{})
	if err != nil {
		t.Fatal(err)
	}
	return ws
}

// writeFiles writes each file, by slash path under root, creating its
// directories.
func writeFiles(t *testing.T, root string, files map[string]string) {
	t.Helper()
	for name, body := range files {
		p := filepath.Join(root, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

func newOptions(ws *workspace.Workspace, a author) Options {
	return Options{
		Input: cli.Input{
			Kind: cli.KindText, Origin: "argument",
			Body: "a library that loads and validates spec packages",
		},
		Workspace: ws,
		Activate:  true,
		Run:       cli.NewRun("spec", "test"),
		Progress:  cli.NewProgress(io.Discard, "spec", false, true, false),
		author:    a,
	}
}

// fileOptions is newOptions on a file input, which a split resumes by origin.
func fileOptions(ws *workspace.Workspace, a author) Options {
	o := newOptions(ws, a)
	o.Input = cli.Input{Kind: cli.KindFile, Origin: "docs/drafts/widgets.md",
		Body: "widgets: a model, a store, and the switch-over"}
	return o
}

// adversarialOptions is newOptions with the review on and rv as its model
// half.
func adversarialOptions(t *testing.T, a author, rv reviewer, rounds int) Options {
	t.Helper()
	o := newOptions(newWorkspace(t), a)
	o.Adversarial, o.AdversarialRounds, o.reviewer = true, rounds, rv
	return o
}

var testIssue = issuex.IssueRef{Repo: issuex.Repo{Owner: "acme", Name: "widgets", Host: "github.com"}, Number: 42}

// commentOn sets o up to post its PRD to testIssue through forge.
func commentOn(o *Options, forge issuex.Client) {
	issue := testIssue
	o.Input = cli.Input{Kind: cli.KindIssue, Origin: issue.URL(), Issue: &issue, Body: o.Input.Body}
	o.Comment, o.Forge = true, forge
}

// mockForge is a forge client that records the comments it is asked to post.
type mockForge struct {
	issuex.NoOpClient
	authenticated bool
	commentURL    string
	commentErr    error
	// hook, when set, runs before each comment is posted.
	hook func()

	commentCalls int
	lastBody     string
	lastRef      issuex.IssueRef
}

func (m *mockForge) Authenticated() bool { return m.authenticated }

func (m *mockForge) AddComment(_ context.Context, ref issuex.IssueRef, body string) (string, error) {
	if m.hook != nil {
		m.hook()
	}
	m.commentCalls++
	m.lastRef, m.lastBody = ref, body
	if m.commentErr != nil {
		return "", m.commentErr
	}
	return m.commentURL, nil
}

// fauxRunner is a Runner on the faux provider, scripted with turns.
func fauxRunner(t *testing.T, ws *workspace.Workspace, turns ...faux.Turn) (*agentrun.Runner, *faux.Provider) {
	t.Helper()
	p := faux.New(turns...)
	return indexedRunner(t, ws, p, nil), p
}

// indexedRunner is a Runner on p whose Config carries idx, as the shell builds
// it from the index it hands the tool's Options.
func indexedRunner(t *testing.T, ws *workspace.Workspace, p *faux.Provider, idx tools.Index) *agentrun.Runner {
	t.Helper()
	r, err := agentrun.NewRunner(agentrun.Config{
		Model:     agentrun.Model{Spec: "faux", Row: *faux.Model()},
		Provider:  p,
		Workspace: ws,
		Bounds:    agentrun.Bounds{MaxTurns: 8, MaxBudgetUSD: 1},
		Index:     idx,
	})
	if err != nil {
		t.Fatal(err)
	}
	return r
}

// toolCall is a model turn that calls the tool name with args.
func toolCall(id, name string, args any) faux.Turn {
	raw, err := json.Marshal(args)
	if err != nil {
		panic(err)
	}
	return faux.Turn{
		Blocks:     []core.ContentBlock{faux.FauxToolCall(id, name, string(raw))},
		StopReason: core.StopReasonToolUse,
	}
}

// transcript renders a request's messages, so a test can assert on what the
// model was actually shown.
func transcript(req core.Request) string {
	var b strings.Builder
	for _, m := range req.Messages {
		switch v := m.(type) {
		case core.UserMessage:
			b.WriteString(v.Content.Text())
		case core.AssistantMessage:
			b.WriteString(v.Content.Text())
		case core.ToolResultMessage:
			b.WriteString(v.Content.Text())
		}
		b.WriteString("\n")
	}
	return b.String()
}

func blocksText(blocks []core.ContentBlock) string {
	var b strings.Builder
	for _, block := range blocks {
		if t, ok := block.(core.TextBlock); ok {
			b.WriteString(t.Text)
		}
	}
	return b.String()
}

// requestUserText is the text of a request's first user message: the phase's
// user prompt as it reached the wire.
func requestUserText(req core.Request) string {
	for _, m := range req.Messages {
		if um, ok := m.(core.UserMessage); ok {
			return blocksText(um.Content)
		}
	}
	return ""
}

// prefixText is the text of a request's cached prefix.
func prefixText(req core.Request) string {
	var b strings.Builder
	for _, m := range req.Prefix {
		if um, ok := m.(core.UserMessage); ok {
			b.WriteString(blocksText(um.Content))
		}
	}
	return b.String()
}

// toolNames are the names of the tools a request offered the model, sorted.
func toolNames(req core.Request) []string {
	var out []string
	for _, w := range req.Tools {
		out = append(out, w.Name)
	}
	slices.Sort(out)
	return out
}

// firstRequest is the first request that reached the model.
func firstRequest(t *testing.T, p *faux.Provider) core.Request {
	t.Helper()
	reqs := p.Requests()
	if len(reqs) == 0 {
		t.Fatal("no request reached the model")
	}
	return reqs[0]
}

func deepCopy(t *testing.T, m map[string]any) map[string]any {
	t.Helper()
	raw, err := json.Marshal(m)
	if err != nil {
		t.Fatal(err)
	}
	var out map[string]any
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatal(err)
	}
	return out
}

func mustJSON(t *testing.T, v any) json.RawMessage {
	t.Helper()
	raw, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

// dirNames are the entries of dir, sorted, or none when it does not exist.
func dirNames(t *testing.T, dir string) []string {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		t.Fatal(err)
	}
	var out []string
	for _, e := range entries {
		out = append(out, e.Name())
	}
	slices.Sort(out)
	return out
}

// specDirs are the entries of the workspace's spec root.
func specDirs(t *testing.T, ws *workspace.Workspace) string {
	t.Helper()
	return strings.Join(dirNames(t, filepath.Join(ws.Root(), ".specs")), ",")
}

// dirHash fingerprints the files of dir.
func dirHash(t *testing.T, dir string) string {
	t.Helper()
	h := sha256.New()
	for _, name := range dirNames(t, dir) {
		b, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			t.Fatal(err)
		}
		fmt.Fprintf(h, "%s\n%s\n", name, b)
	}
	return fmt.Sprintf("%x", h.Sum(nil))
}

func exists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

// warning is the first warning of the run with code, and whether there is one.
func warning(run *cli.Run, code cli.WarnCode) (cli.Warning, bool) {
	for _, w := range run.Warnings() {
		if w.Code == code {
			return w, true
		}
	}
	return cli.Warning{}, false
}

func warningText(run *cli.Run) string {
	return strings.Join(cli.WarningMessages(run.Warnings()), "\n")
}

// scopeStates are the statuses of a split's scopes, comma-separated.
func scopeStates(r *Result) string {
	var out []string
	for _, s := range r.Split {
		out = append(out, s.Status)
	}
	return strings.Join(out, ",")
}

// packages are every package a result reports, the first one first.
func packages(r *Result) []Package {
	return append([]Package{r.Package}, r.FollowOnSpecs...)
}

func checksOf(items []ValidationItem) []string {
	var out []string
	for _, it := range items {
		out = append(out, it.Check)
	}
	return out
}

func asFailure(t *testing.T, err error) *Failure {
	t.Helper()
	var f *Failure
	if !errors.As(err, &f) {
		t.Fatalf("err = %v (%T), want a *Failure", err, err)
	}
	return f
}

// preflightRunner is a Runner on p with 10 turns and $2 a phase: building one
// makes no call. With a tier it resolves that tier's model per phase.
func preflightRunner(t *testing.T, p *faux.Provider, tier string) *agentrun.Runner {
	t.Helper()
	cfg := agentrun.Config{
		Model:    agentrun.Model{Spec: "test", Row: core.Model{ID: "test-model"}},
		Provider: p,
		Bounds:   agentrun.Bounds{MaxTurns: 10, MaxBudgetUSD: 2.0},
	}
	if tier != "" {
		plan, err := agentrun.ResolvePlan(tier, "", false, nil)
		if err != nil {
			t.Fatal(err)
		}
		cfg.Model, cfg.PhaseModels = plan.Default, plan.Phases
	}
	r, err := agentrun.NewRunner(cfg)
	if err != nil {
		t.Fatal(err)
	}
	return r
}

// preflightOptions is newOptions with a real Runner. RunPreflight never runs
// a phase, so the scripted author is never asked for one.
func preflightOptions(t *testing.T) Options {
	t.Helper()
	o := newOptions(newWorkspace(t), newAuthor(t, "01", "test_feature"))
	o.Runner = preflightRunner(t, faux.New(), "")
	return o
}

func findCheck(list []cli.PreflightCheck, name string) (cli.PreflightCheck, bool) {
	for _, c := range list {
		if c.Check == name {
			return c, true
		}
	}
	return cli.PreflightCheck{}, false
}

// prdArgs is a submit_prd payload carrying the fixture's PRD body, which the
// package's validation accepts, and a split into the scopes named.
func prdArgs(t *testing.T, name string, split ...string) map[string]any {
	t.Helper()
	_, body := loadFixture(t, "01", name)
	args := map[string]any{"spec_name": name, "title": "Test Feature", "body": body}
	if len(split) > 0 {
		var scopes []map[string]any
		for _, s := range split {
			scopes = append(scopes, map[string]any{"name": s, "scope": "Covers " + s + "."})
		}
		args["recommended_split"] = scopes
	}
	return args
}

// generationTurns script the three generation phases of one package.
func generationTurns(t *testing.T, id, name string) []faux.Turn {
	t.Helper()
	artifacts, _ := loadFixture(t, id, name)
	var turns []faux.Turn
	for _, step := range afspec.GenerationSteps {
		turns = append(turns, toolCall("g-"+id+"-"+string(step), ArtifactToolName(step), artifacts[step]))
	}
	return turns
}

const sampleMap = "```\n./\n  main.go  func Main L3\n```\n"

// mapSection is the repository map block of a prompt: the heading, the
// opening sentence and the fenced map, or "" when the prompt has none.
func mapSection(prompt string) string {
	i := strings.Index(prompt, "## Repository map")
	if i < 0 {
		return ""
	}
	open := strings.Index(prompt[i:], "```\n")
	if open < 0 {
		return prompt[i:]
	}
	body := i + open + len("```\n")
	closing := strings.Index(prompt[body:], "```")
	if closing < 0 {
		return prompt[i:]
	}
	return prompt[i : body+closing+len("```")]
}
