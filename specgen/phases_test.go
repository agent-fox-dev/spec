package specgen

import (
	"context"
	"slices"
	"strings"
	"testing"

	"github.com/agent-fox-dev/agentkit-go/provider/faux"
	"github.com/agent-fox-dev/agentkit-go/workspace"

	"github.com/agent-fox-dev/spec/afspec"
	"github.com/agent-fox-dev/spec/internal/agentrun"
	"github.com/agent-fox-dev/spec/internal/agentrun/indextest"
	"github.com/agent-fox-dev/spec/internal/project"
)

// phaseCall runs one phase of a against whatever its provider was scripted
// with, and returns the phase's error.
type phaseCall func(ctx context.Context, a *agentAuthor, ws *workspace.Workspace) error

var everyPhase = []struct {
	name  string
	tools []string
	run   phaseCall
}{
	{"prd", []string{ToolSubmitPRD}, func(ctx context.Context, a *agentAuthor, ws *workspace.Workspace) error {
		_, _, err := a.WritePRD(ctx, prdRequest{Root: ws.Root()})
		return err
	}},
	{"requirements", []string{ArtifactToolName(afspec.StepRequirements), PatchToolName(afspec.StepRequirements)},
		func(ctx context.Context, a *agentAuthor, ws *workspace.Workspace) error {
			_, _, err := a.GenerateArtifact(ctx, artifactRequest{Step: afspec.StepRequirements, Root: ws.Root(),
				Partial: &afspec.PartialSpec{}})
			return err
		}},
	{"architecture", []string{ToolSubmitArchitecture}, func(ctx context.Context, a *agentAuthor, ws *workspace.Workspace) error {
		_, _, err := a.WriteArchitecture(ctx, architectureRequest{Root: ws.Root(), Partial: &afspec.PartialSpec{}})
		return err
	}},
	{"prd_review", []string{ToolSubmitPRDReview}, func(ctx context.Context, a *agentAuthor, ws *workspace.Workspace) error {
		_, _, err := a.Review(ctx, reviewRequest{Root: ws.Root(), Round: 1, Limit: 1})
		return err
	}},
	{"prd_revision", []string{ToolSubmitPRDRevision}, func(ctx context.Context, a *agentAuthor, ws *workspace.Workspace) error {
		_, _, _, err := a.Revise(ctx, revisionRequest{reviewRequest: reviewRequest{Root: ws.Root(), Round: 1, Limit: 1},
			Findings: roundFindings("R1-F1")})
		return err
	}},
}

// Every phase is offered its own tools, the read-only file tools and, with an
// index, code_search; and a cap on one response that leaves room to think. A
// phase's reply is one tool call carrying the whole document, and at the
// highest efforts the model thinks most of it through first: a run whose PRD
// phase spent a 32768 cap on one turn of thinking never reached submit_prd.
func TestPhases_AreOfferedTheirToolsAndRoomToThink(t *testing.T) {
	const floor = 64000
	ws := newWorkspace(t)
	for _, phase := range everyPhase {
		t.Run(phase.name, func(t *testing.T) {
			p := faux.New()
			a := &agentAuthor{runner: indexedRunner(t, ws, p, &indextest.Index{}), ws: ws, codeSearch: true}
			_ = phase.run(context.Background(), a, ws)
			req := firstRequest(t, p)
			if req.MaxTokens == nil || *req.MaxTokens < floor {
				t.Errorf("max_tokens = %v, want at least %d", req.MaxTokens, floor)
			}
			want := append(slices.Clone(phase.tools), agentrun.ReadOnlyFileTools...)
			want = append(want, "code_search")
			slices.Sort(want)
			if got := toolNames(req); !slices.Equal(got, want) {
				t.Errorf("tools = %v, want %v", got, want)
			}
		})
	}
}

// A phase that ends without an accepted submission is a no-result error that
// names the phase and its tool; a generation phase also says what its
// rejected submissions were stuck on.
func TestPhases_EndingWithoutASubmissionIsNoResult(t *testing.T) {
	ws := newWorkspace(t)
	for _, phase := range everyPhase {
		t.Run(phase.name, func(t *testing.T) {
			runner, _ := fauxRunner(t, ws, faux.Text("looks fine to me"))
			err := phase.run(context.Background(), &agentAuthor{runner: runner, ws: ws}, ws)
			if agentrun.CategoryOf(err) != agentrun.CategoryNoResult || !strings.Contains(err.Error(), phase.tools[0]) {
				t.Errorf("err = %v; want no_result naming %s", err, phase.tools[0])
			}
		})
	}
	t.Run("after a rejected artifact", func(t *testing.T) {
		artifacts, _ := loadFixture(t, "01", "test_feature")
		broken := deepCopy(t, artifacts[afspec.StepTestSpec])
		broken["tests"].([]any)[0].(map[string]any)["verifies"] = []any{"01-REQ-99.1"}
		runner, _ := fauxRunner(t, ws, toolCall("c1", ArtifactToolName(afspec.StepTestSpec), broken), faux.Text("I give up"))
		a := &agentAuthor{runner: runner, ws: ws}
		_, _, err := a.GenerateArtifact(context.Background(), artifactRequest{Step: afspec.StepTestSpec, Root: ws.Root(),
			Partial: acceptedArtifacts(t, artifacts, afspec.StepRequirements)})
		if agentrun.CategoryOf(err) != agentrun.CategoryNoResult || !strings.Contains(err.Error(), "1 submission(s) were rejected") {
			t.Errorf("err = %v", err)
		}
	})
}

// Every phase is bounded by the Runner like any other: one stopped by its
// turn limit returns that failure, with its category, not a missing result.
func TestPhases_StoppedByTheirBoundsReturnTheRunnersFailure(t *testing.T) {
	ws := newWorkspace(t)
	read := func(id string) faux.Turn { return faux.ToolCall(id, "list_files", `{}`) }
	for _, phase := range everyPhase {
		t.Run(phase.name, func(t *testing.T) {
			r, err := agentrun.NewRunner(agentrun.Config{
				Model: agentrun.Model{Spec: "faux", Row: *faux.Model()}, Provider: faux.New(read("1"), read("2"), read("3")),
				Workspace: ws, Bounds: agentrun.Bounds{MaxTurns: 2},
			})
			if err != nil {
				t.Fatal(err)
			}
			if err := phase.run(context.Background(), &agentAuthor{runner: r, ws: ws}, ws); agentrun.CategoryOf(err) != agentrun.CategoryMaxTurns {
				t.Errorf("err = %v; want max_turns", err)
			}
		})
	}
}

// The caller's --context block reaches the PRD phase after the input,
// independently of it, in the phase's cached prefix ahead of its task.
func TestWritePRD_CarriesTheCallersContextAfterTheInput(t *testing.T) {
	const block = "## Additional context from the caller\n\nTarget the v2 API only.\n"
	prefix := func(t *testing.T, callerContext string) (body, sent string) {
		t.Helper()
		ws := newWorkspace(t)
		runner, p := fauxRunner(t, ws, faux.Text("no PRD"))
		o := newOptions(ws, nil)
		o.Runner = runner
		o.Input.Context = callerContext
		_, _ = Run(context.Background(), o)
		return o.Input.Body, prefixText(firstRequest(t, p))
	}

	body, sent := prefix(t, block)
	if !strings.Contains(sent, "--- BEGIN INPUT ---\n"+body+"\n--- END INPUT ---") {
		t.Errorf("the input block is not the body, unchanged:\n%s", sent)
	}
	if i := strings.Index(sent, "Target the v2 API only."); i < strings.Index(sent, "--- END INPUT ---") {
		t.Errorf("the context should follow the input, not sit inside it or be missing:\n%s", sent)
	}
	if _, sent := prefix(t, ""); strings.Contains(sent, "Additional context") {
		t.Errorf("a prompt with no --context mentions one:\n%s", sent)
	}
}

// A rejected PRD submission comes back to the model as a tool error it
// corrects on the next turn, in the same conversation.
func TestWritePRD_ARejectedSubmissionIsCorrectedInTheLoop(t *testing.T) {
	ws := newWorkspace(t)
	writeFiles(t, ws.Root(), map[string]string{"real.go": "package main\n"})
	submission := func(path string) map[string]any {
		args := validPRDArgs()
		args["relevant_files"] = []map[string]any{{"path": path, "why": "w"}}
		return args
	}
	runner, _ := fauxRunner(t, ws,
		toolCall("c1", ToolSubmitPRD, submission("nonexistent.go")),
		toolCall("c2", ToolSubmitPRD, submission("real.go")))
	a := &agentAuthor{runner: runner, ws: ws}
	prd, res, err := a.WritePRD(context.Background(), prdRequest{Root: ws.Root(), SourceKind: "text", Input: "test input"})
	if err != nil {
		t.Fatalf("WritePRD: %v", err)
	}
	if len(prd.RelevantFiles) != 1 || prd.RelevantFiles[0].Path != "real.go" {
		t.Errorf("RelevantFiles = %+v, want the corrected one", prd.RelevantFiles)
	}
	if res.ToolErrors["submit_prd/invalid_relevant_files"] != 1 || res.ToolCalls[ToolSubmitPRD] != 2 {
		t.Errorf("tool errors %v, calls %v; want the rejection and the correction", res.ToolErrors, res.ToolCalls)
	}
}

// The architecture phase is told about the project like every other phase,
// and its task carries the PRD phase's notes and files and every artifact,
// the tasks included. An empty document is turned back.
func TestWriteArchitecture_IsSentTheRunContextAndEveryArtifact(t *testing.T) {
	ws := newWorkspace(t)
	runner, p := fauxRunner(t, ws,
		toolCall("a1", ToolSubmitArchitecture, map[string]any{"document": "  "}),
		toolCall("a2", ToolSubmitArchitecture, map[string]any{"document": "# Architecture: x\n"}))
	artifacts, _ := loadFixture(t, "01", "test_feature")
	partial := acceptedArtifacts(t, artifacts, afspec.GenerationSteps...)
	a := &agentAuthor{runner: runner}
	doc, _, err := a.WriteArchitecture(context.Background(), architectureRequest{
		SpecID: "01", SpecName: "test_feature", Root: ws.Root(), PRD: "## Intent\n\nx\n", Partial: partial,
		RepoMap: sampleMap, Steering: "Always write tests first.", CodebaseNotes: "model.go holds the Widget.",
		RelevantFiles: []RelevantFile{{Path: "a.go", Why: "the loader"}},
	})
	if err != nil || doc != "# Architecture: x\n" {
		t.Fatalf("WriteArchitecture = %q, %v", doc, err)
	}
	reqs := p.Requests()
	if sys := blocksText(reqs[0].System); !strings.Contains(sys, "## Repository map") || !strings.Contains(sys, "Always write tests first.") {
		t.Errorf("the system prompt lacks the run context:\n%s", sys)
	}
	user := requestUserText(reqs[0])
	for _, want := range []string{"model.go holds the Widget.", "`a.go` — the loader", "## requirements.json", "## test_spec.json",
		"## tasks.json", "01-REQ-1.1", "TS-01-1"} {
		if !strings.Contains(user, want) {
			t.Errorf("the architecture task lacks %q", want)
		}
	}
	if !strings.Contains(transcript(reqs[1]), "empty_document") {
		t.Error("the empty document was not turned back")
	}
}

// markedReviewRequest is a review request in which every part the phase must
// be sent carries a marker a test can look for.
func markedReviewRequest(ws *workspace.Workspace) reviewRequest {
	return reviewRequest{
		Root: ws.Root(), SourceKind: "text", SourceOrigin: "argument",
		Input:   "INPUT-MARK: cache the widgets",
		Context: "CONTEXT-MARK",
		Profile: project.DetectProfile(ws.Root()),
		Landscape: []afspec.SpecMeta{
			{SpecID: "07", SpecName: "widget_store", Status: "active", Dir: "/x/.specs/07_widget_store"},
		},
		SpecRoot: ".specs",
		Steering: "STEER-MARK",
		RepoMap:  "MAP-MARK",
		PRD: PRD{
			SpecName: "widget_cache", Title: "TITLE-MARK",
			Body:          "## Intent\n\nBODY-MARK\n",
			OpenQuestions: []OpenQuestion{{Question: "OQ-MARK", Decision: "decided", Why: "unsure"}},
			RelevantFiles: []RelevantFile{{Path: "go.mod", Why: "RF-MARK"}},
			CodebaseNotes: "NOTE-X",
		},
		Split: scopeTwoOfThree(),
		Round: 1, Limit: 2,
	}
}

// The review is a fresh conversation, sent what the PRD's author saw — the
// input and its context, the existing specs, the split — the PRD as it
// stands, and the author's notes as claims to check, with the run context
// every phase has.
func TestReview_IsSentEverythingTheAuthorSaw(t *testing.T) {
	ws := newWorkspace(t)
	runner, p := fauxRunner(t, ws, toolCall("r1", ToolSubmitPRDReview, map[string]any{"findings": []any{}}))
	a := &agentAuthor{runner: runner, ws: ws}
	req := markedReviewRequest(ws)
	findings, res, err := a.Review(context.Background(), req)
	if err != nil || findings == nil || len(findings) != 0 || res.Name != agentrun.PhasePRDReview {
		t.Fatalf("findings %#v, result %+v, err %v", findings, res, err)
	}
	first := firstRequest(t, p)
	if n := len(first.Messages); n != 1 {
		t.Errorf("the review starts with %d messages, want 1: a fresh conversation", n)
	}
	user := requestUserText(first)
	for _, want := range []string{"widget_cache", "TITLE-MARK", "BODY-MARK", "OQ-MARK", "RF-MARK", "go.mod",
		"INPUT-MARK", "CONTEXT-MARK", "NOTE-X", "07_widget_store", splitReviewBlock(req.Split)} {
		if !strings.Contains(user, want) {
			t.Errorf("the review prompt has no %q", want)
		}
	}
	if !strings.Contains(user, "the author's claims") || !strings.Contains(user, "not facts") {
		t.Errorf("the notes are not labelled as claims to check:\n%s", user)
	}
	// agentkit appends its tool guidelines after the phase's own system
	// prompt, so the run context is in it rather than at its end.
	if sys := blocksText(first.System); !strings.Contains(sys, runContextBlock(req.Profile.LanguageBlock(), req.Steering, req.RepoMap)) {
		t.Errorf("the system prompt does not carry the run context:\n%s", sys)
	}
}

// The revision is another fresh conversation, sent the review's context and
// the round's findings by id, and it returns the revised PRD and every
// disposition.
func TestRevise_IsSentTheFindingsAndTheReviewsContext(t *testing.T) {
	ws := newWorkspace(t)
	runner, p := fauxRunner(t, ws, toolCall("v1", ToolSubmitPRDRevision, map[string]any{
		"body":         "## Intent\n\nREVISED\n",
		"dispositions": []any{accept("R1-F1"), reject("R1-F2", "it holds")},
	}))
	a := &agentAuthor{runner: runner, ws: ws}
	findings := roundFindings("R1-F1", "R1-F2")
	findings[0].Flaw, findings[1].Evidence = "FLAW-ONE-MARK", "EVIDENCE-TWO-MARK"
	req := revisionRequest{reviewRequest: markedReviewRequest(ws), Findings: findings}
	prd, dispositions, res, err := a.Revise(context.Background(), req)
	if err != nil || prd.Body != "## Intent\n\nREVISED\n" || prd.SpecName != "widget_cache" || len(dispositions) != 2 ||
		res.Name != agentrun.PhasePRDRevision {
		t.Fatalf("prd %+v, dispositions %v, result %+v, err %v", prd, dispositions, res, err)
	}
	first := firstRequest(t, p)
	if n := len(first.Messages); n != 1 {
		t.Errorf("the revision starts with %d messages, want 1", n)
	}
	user := requestUserText(first)
	for _, want := range []string{"R1-F1", "R1-F2", "FLAW-ONE-MARK", "EVIDENCE-TWO-MARK", "BODY-MARK", "INPUT-MARK",
		"CONTEXT-MARK", "NOTE-X", "07_widget_store", splitReviewBlock(req.Split)} {
		if !strings.Contains(user, want) {
			t.Errorf("the revision prompt has no %q", want)
		}
	}
	if sys := blocksText(first.System); !strings.Contains(sys, runContextBlock(req.Profile.LanguageBlock(), req.Steering, req.RepoMap)) {
		t.Error("the revision does not carry the run context")
	}
}
