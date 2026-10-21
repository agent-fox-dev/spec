package specgen

import (
	"context"
	"encoding/json"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/agent-fox-dev/agentkit-go/core"
	"github.com/agent-fox-dev/agentkit-go/provider/faux"
	"github.com/agent-fox-dev/agentkit-go/workspace"

	"github.com/agent-fox-dev/spec/afspec"
	"github.com/agent-fox-dev/spec/internal/agentrun"
	"github.com/agent-fox-dev/spec/internal/project"
)

// reviewRunner is a Runner on a catalog model, so a phase's spend has a price
// and --budget can stop it.
func reviewRunner(t *testing.T, ws *workspace.Workspace, p *faux.Provider, b agentrun.Bounds) *agentrun.Runner {
	t.Helper()
	m, err := agentrun.ResolveModel("claude-sonnet-5-5", "", false)
	if err != nil {
		t.Fatal(err)
	}
	r, err := agentrun.NewRunner(agentrun.Config{Model: m, Provider: p, Workspace: ws, Bounds: b})
	if err != nil {
		t.Fatal(err)
	}
	return r
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
		Round: 1, Limit: 2,
	}
}

func submitCall(id, tool string, args map[string]any) faux.Turn {
	b, err := json.Marshal(args)
	if err != nil {
		panic(err)
	}
	return faux.ToolCall(id, tool, string(b))
}

func toolNames(req core.Request) []string {
	var out []string
	for _, w := range req.Tools {
		out = append(out, w.Name)
	}
	slices.Sort(out)
	return out
}

// TS-09-7 (integration): the review is a fresh conversation, sent the PRD,
// the input and its context, the notes as claims to check, the run context
// and the read-only tools.
//
// Verifies: 09-REQ-2.1
func TestTS09_7_TheReviewIsSentEverythingTheAuthorSaw(t *testing.T) {
	ws := newWorkspace(t)
	for _, cs := range []bool{false, true} {
		p := faux.New(submitCall("r1", ToolSubmitPRDReview, map[string]any{"findings": []any{}}))
		r := reviewRunner(t, ws, p, agentrun.Bounds{})
		idx := &fakeIndex{}
		if cs {
			var err error
			r, err = agentrun.NewRunner(agentrun.Config{Model: r.Model(), Provider: p, Workspace: ws, Index: idx})
			if err != nil {
				t.Fatal(err)
			}
		}
		a := &agentAuthor{runner: r, ws: ws, codeSearch: cs}
		req := markedReviewRequest(ws)
		findings, res, err := a.Review(context.Background(), req)
		if err != nil || len(findings) != 0 || res.Name != agentrun.PhasePRDReview {
			t.Fatalf("findings %v, result %+v, err %v", findings, res, err)
		}
		reqs := p.Requests()
		if len(reqs) == 0 {
			t.Fatal("no request")
		}
		first := reqs[0]
		if n := len(first.Messages); n != 1 {
			t.Errorf("the review starts with %d messages, want 1: a fresh conversation", n)
		}
		user, system := requestUserText(first), blocksText(first.System)
		for _, want := range []string{"widget_cache", "TITLE-MARK", "BODY-MARK", "OQ-MARK", "RF-MARK", "go.mod",
			"INPUT-MARK", "CONTEXT-MARK", "NOTE-X", "07_widget_store"} {
			if !strings.Contains(user, want) {
				t.Errorf("the review prompt has no %q", want)
			}
		}
		if !strings.Contains(user, "the author's claims") || !strings.Contains(user, "not facts") {
			t.Errorf("the notes are not labelled as claims to check:\n%s", user)
		}
		// agentkit appends its tool guidelines after the phase's own system
		// prompt, so the run context is in it rather than at its end.
		if !strings.Contains(system, runContextBlock(req.Profile.LanguageBlock(), req.Steering, req.RepoMap)) ||
			!strings.Contains(system, "STEER-MARK") || !strings.Contains(system, "MAP-MARK") {
			t.Errorf("the system prompt does not carry the run context:\n%s", system)
		}
		want := append([]string{ToolSubmitPRDReview}, agentrun.ReadOnlyFileTools...)
		if cs {
			want = append(want, "code_search")
		}
		slices.Sort(want)
		if got := toolNames(first); !slices.Equal(got, want) {
			t.Errorf("code search %v: tools %v, want %v", cs, got, want)
		}
	}
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

// TS-09-8 (integration): the review of a split scope is told the split and
// what the earlier scopes deliver, and that another scope's work is not a
// dropped ask, without the PRD-writing instructions.
//
// Verifies: 09-REQ-2.2
func TestTS09_8_AScopesReviewIsToldTheSplitButNotToWriteAPRD(t *testing.T) {
	block := splitReviewBlock(scopeTwoOfThree())
	for _, want := range []string{"widget_store", "widget_cache", "widget_evict", "STORE-SCOPE", "CACHE-SCOPE",
		"EVICT-SCOPE", "scope 2", "DELIVERS-MARK", "EARLIER-NOTES-MARK", "not a dropped ask"} {
		if !strings.Contains(block, want) {
			t.Errorf("the review's split block has no %q:\n%s", want, block)
		}
	}
	for _, instruction := range []string{"Write the PRD for scope", "Submit exactly that name", "recommended_split"} {
		if strings.Contains(block, instruction) {
			t.Errorf("the review's split block carries the PRD-writing instruction %q", instruction)
		}
	}
	if splitReviewBlock(nil) != "" {
		t.Error("an undivided input has a split block")
	}

	ws := newWorkspace(t)
	p := faux.New(submitCall("r1", ToolSubmitPRDReview, map[string]any{"findings": []any{}}))
	a := &agentAuthor{runner: reviewRunner(t, ws, p, agentrun.Bounds{}), ws: ws}
	req := markedReviewRequest(ws)
	req.Split = scopeTwoOfThree()
	if _, _, err := a.Review(context.Background(), req); err != nil {
		t.Fatal(err)
	}
	if user := requestUserText(p.Requests()[0]); !strings.Contains(user, block) {
		t.Error("the review prompt does not carry the split block")
	}
}

// TS-09-9 (unit): the review system prompt names every kind of flaw and
// tells the reviewer not to approve.
//
// Verifies: 09-REQ-2.3
func TestTS09_9_TheReviewMandateNamesEveryFlawKind(t *testing.T) {
	s := strings.ToLower(prdReviewSystemPrompt())
	for _, kind := range []string{
		"without an error path", "cannot be verified", "the code contradicts", "already exists under another name",
		"did not ask for", "dropped", "two readers", "contradict each other", "design decisions whose reason",
		"internal design", "do not approve",
	} {
		if !strings.Contains(s, kind) {
			t.Errorf("the review system prompt does not say %q", kind)
		}
	}
}

// TS-09-12 (unit): a review that ends without an accepted submission is a
// no-result error naming the phase and its tool.
//
// Verifies: 09-REQ-2.6
func TestTS09_12_AReviewWithoutASubmissionIsNoResult(t *testing.T) {
	ws := newWorkspace(t)
	a := &agentAuthor{runner: reviewRunner(t, ws, faux.New(faux.Text("looks fine")), agentrun.Bounds{}), ws: ws}
	_, _, err := a.Review(context.Background(), markedReviewRequest(ws))
	if err == nil || agentrun.CategoryOf(err) != agentrun.CategoryNoResult ||
		!strings.Contains(err.Error(), "prd_review") || !strings.Contains(err.Error(), ToolSubmitPRDReview) {
		t.Errorf("err = %v", err)
	}
}

// markedRevisionRequest is markedReviewRequest with two findings to answer.
func markedRevisionRequest(ws *workspace.Workspace) revisionRequest {
	f := roundFindings("R1-F1", "R1-F2")
	f[0].Flaw, f[1].Evidence = "FLAW-ONE-MARK", "EVIDENCE-TWO-MARK"
	return revisionRequest{reviewRequest: markedReviewRequest(ws), Findings: f}
}

// TS-09-20 (integration): the revision is another fresh conversation, sent
// the PRD, the findings with their ids, the input, the context and the
// notes, with the review's split context, run context and tools.
//
// Verifies: 09-REQ-4.1
func TestTS09_20_TheRevisionIsSentTheFindingsAndTheReviewsContext(t *testing.T) {
	ws := newWorkspace(t)
	split := scopeTwoOfThree()

	rp := faux.New(submitCall("r1", ToolSubmitPRDReview, map[string]any{"findings": []any{}}))
	review := &agentAuthor{runner: reviewRunner(t, ws, rp, agentrun.Bounds{}), ws: ws}
	rreq := markedReviewRequest(ws)
	rreq.Split = split
	if _, _, err := review.Review(context.Background(), rreq); err != nil {
		t.Fatal(err)
	}

	vp := faux.New(submitCall("v1", ToolSubmitPRDRevision, map[string]any{
		"body":         "## Intent\n\nREVISED\n",
		"dispositions": []any{accept("R1-F1"), reject("R1-F2", "it holds")},
	}))
	revise := &agentAuthor{runner: reviewRunner(t, ws, vp, agentrun.Bounds{}), ws: ws}
	vreq := markedRevisionRequest(ws)
	vreq.Split = split
	prd, dispositions, res, err := revise.Revise(context.Background(), vreq)
	if err != nil || prd.Body != "## Intent\n\nREVISED\n" || len(dispositions) != 2 || res.Name != agentrun.PhasePRDRevision {
		t.Fatalf("prd %+v, dispositions %v, result %+v, err %v", prd, dispositions, res, err)
	}

	first := vp.Requests()[0]
	if n := len(first.Messages); n != 1 {
		t.Errorf("the revision starts with %d messages, want 1", n)
	}
	user := requestUserText(first)
	for _, want := range []string{"R1-F1", "R1-F2", "FLAW-ONE-MARK", "EVIDENCE-TWO-MARK", "BODY-MARK", "INPUT-MARK",
		"CONTEXT-MARK", "NOTE-X", "07_widget_store", splitReviewBlock(split)} {
		if !strings.Contains(user, want) {
			t.Errorf("the revision prompt has no %q", want)
		}
	}
	reviewFirst := rp.Requests()[0]
	runCtx := runContextBlock(vreq.Profile.LanguageBlock(), vreq.Steering, vreq.RepoMap)
	if !strings.Contains(blocksText(first.System), runCtx) || !strings.Contains(blocksText(reviewFirst.System), runCtx) {
		t.Error("the review and the revision do not carry the same run context")
	}
	reviewTools := slices.DeleteFunc(toolNames(reviewFirst), func(n string) bool { return n == ToolSubmitPRDReview })
	revisionTools := slices.DeleteFunc(toolNames(first), func(n string) bool { return n == ToolSubmitPRDRevision })
	if !slices.Equal(reviewTools, revisionTools) {
		t.Errorf("read-only tools differ: review %v, revision %v", reviewTools, revisionTools)
	}
}

// TS-09-21 (unit): the revision system prompt asks for an answer to every
// finding, a rejection that says why the finding does not hold, and the
// settled points in ## Design Decisions.
//
// Verifies: 09-REQ-4.2
func TestTS09_21_TheRevisionMandate(t *testing.T) {
	s := prdRevisionSystemPrompt()
	for _, want := range []string{"every finding", "accepted", "rejected", "does not hold", "## Design Decisions"} {
		if !strings.Contains(s, want) {
			t.Errorf("the revision system prompt does not say %q", want)
		}
	}
}

// TS-09-26 (unit): a revision that ends without an accepted submission is a
// no-result error naming the phase and its tool.
//
// Verifies: 09-REQ-4.7
func TestTS09_26_ARevisionWithoutASubmissionIsNoResult(t *testing.T) {
	ws := newWorkspace(t)
	a := &agentAuthor{runner: reviewRunner(t, ws, faux.New(faux.Text("done")), agentrun.Bounds{}), ws: ws}
	_, _, _, err := a.Revise(context.Background(), markedRevisionRequest(ws))
	if err == nil || agentrun.CategoryOf(err) != agentrun.CategoryNoResult ||
		!strings.Contains(err.Error(), "prd_revision") || !strings.Contains(err.Error(), ToolSubmitPRDRevision) {
		t.Errorf("err = %v", err)
	}
}

// TS-09-63 (integration): each review is bounded by --max-turns, --budget,
// --phase-timeout and --max-tokens as a phase of its own.
//
// Verifies: 09-REQ-10.6
func TestTS09_63_AReviewIsBoundLikeEveryPhase(t *testing.T) {
	ws := newWorkspace(t)
	read := func(id string) faux.Turn { return faux.ToolCall(id, "list_files", `{}`) }

	p := faux.New(read("1"), read("2"), read("3"))
	a := &agentAuthor{runner: reviewRunner(t, ws, p, agentrun.Bounds{MaxTurns: 2}), ws: ws}
	_, res, err := a.Review(context.Background(), markedReviewRequest(ws))
	if agentrun.CategoryOf(err) != agentrun.CategoryMaxTurns || res.StopReason != core.RunStopMaxTurns {
		t.Errorf("--max-turns: err %v, stop %s", err, res.StopReason)
	}

	expensive := read("1")
	expensive.Usage = core.Usage{InputTokens: 2_000_000, OutputTokens: 10}
	p = faux.New(expensive, read("2"))
	a = &agentAuthor{runner: reviewRunner(t, ws, p, agentrun.Bounds{MaxBudgetUSD: 1}), ws: ws}
	_, res, err = a.Review(context.Background(), markedReviewRequest(ws))
	if agentrun.CategoryOf(err) != agentrun.CategoryBudget || res.StopReason != core.RunStopBudgetExceeded {
		t.Errorf("--budget: err %v, stop %s", err, res.StopReason)
	}

	slow := read("1")
	slow.Delay = 2 * time.Second
	p = faux.New(slow)
	a = &agentAuthor{runner: reviewRunner(t, ws, p, agentrun.Bounds{Timeout: 50 * time.Millisecond}), ws: ws}
	ctx := context.Background()
	_, _, err = a.Review(ctx, markedReviewRequest(ws))
	if err == nil || ctx.Err() != nil || agentrun.CategoryOf(err) != agentrun.CategoryAborted {
		t.Errorf("--phase-timeout: err %v (run context %v)", err, ctx.Err())
	}

	p = faux.New(submitCall("r1", ToolSubmitPRDReview, map[string]any{"findings": []any{}}))
	a = &agentAuthor{runner: reviewRunner(t, ws, p, agentrun.Bounds{MaxTokens: 1234}), ws: ws}
	if _, _, err := a.Review(context.Background(), markedReviewRequest(ws)); err != nil {
		t.Fatal(err)
	}
	if n := p.Requests()[0].MaxTokens; n == nil || *n != 1234 {
		t.Errorf("--max-tokens: max_tokens = %v, want 1234", n)
	}

	// The bound is per phase: what an earlier phase on the same Runner spent
	// does not count against the review.
	costly := submitCall("r1", ToolSubmitPRDReview, map[string]any{"findings": []any{}})
	costly.Usage = core.Usage{InputTokens: 150_000, OutputTokens: 10}
	again := costly
	p = faux.New(costly, again)
	a = &agentAuthor{runner: reviewRunner(t, ws, p, agentrun.Bounds{MaxBudgetUSD: 1}), ws: ws}
	for i := range 2 {
		_, res, err := a.Review(context.Background(), markedReviewRequest(ws))
		if err != nil || res.Usage.CostUSD <= 0.3 {
			t.Errorf("review %d: err %v, cost %v", i+1, err, res.Usage.CostUSD)
		}
	}
}
