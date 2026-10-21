package specgen

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/agent-fox-dev/spec/afspec"
	"github.com/agent-fox-dev/spec/envelope"
	"github.com/agent-fox-dev/spec/internal/agentrun"
	"github.com/agent-fox-dev/spec/internal/cli"
	"github.com/agent-fox-dev/spec/internal/issuex"
)

// recordingAuthor is scriptedAuthor that also records what the architecture
// phase was sent.
type recordingAuthor struct {
	*scriptedAuthor
	archReqs []architectureRequest
}

func (a *recordingAuthor) WriteArchitecture(ctx context.Context, req architectureRequest) (string, agentrun.Result, error) {
	a.archReqs = append(a.archReqs, req)
	return a.scriptedAuthor.WriteArchitecture(ctx, req)
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

// adversarialOptions is newOptions with the review on and rv as its model
// half.
func adversarialOptions(t *testing.T, a author, rv reviewer, rounds int) Options {
	t.Helper()
	o := newOptions(newWorkspace(t), a)
	o.Adversarial, o.AdversarialRounds, o.reviewer = true, rounds, rv
	return o
}

func packageFiles(t *testing.T, dir string) []string {
	t.Helper()
	entries, err := os.ReadDir(dir)
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

func usageNames(run *cli.Run) []string {
	env := run.Envelope(cli.ExitOK, nil, nil)
	var out []string
	if env.Usage != nil {
		for _, p := range env.Usage.Phases {
			out = append(out, p.Name)
		}
	}
	return out
}

// TS-09-5 (integration): without --adversarial the run has the phases,
// result, summary, files and usage it had before the feature.
//
// Verifies: 09-REQ-1.5
func TestTS09_5_WithoutAdversarialNothingChanges(t *testing.T) {
	a := newAuthor(t, "01", "test_feature")
	o := newOptions(newWorkspace(t), a)
	o.reviewer = failingReviewer{t}
	res, err := Run(context.Background(), o)
	if err != nil {
		t.Fatal(err)
	}
	if want := afspec.GenerationSteps; !slices.Equal(a.steps, want) || len(a.prdRequests) != 1 {
		t.Errorf("phases: %d PRD, steps %v", len(a.prdRequests), a.steps)
	}
	raw, _ := json.Marshal(res)
	if strings.Contains(string(raw), `"review"`) || res.Review != nil {
		t.Errorf("the result has a review: %s", raw)
	}
	if strings.Contains(res.Summary(), "review:") {
		t.Errorf("summary = %q", res.Summary())
	}
	if got := packageFiles(t, filepath.Join(o.Workspace.Root(), res.SpecDir)); !slices.Equal(got,
		[]string{"prd.md", "requirements.json", "tasks.json", "test_spec.json"}) {
		t.Errorf("package files = %v", got)
	}
	if got := usageNames(o.Run); !slices.Equal(got, []string{"prd", "generate:requirements", "generate:test_spec", "generate:tasks"}) {
		t.Errorf("usage phases = %v", got)
	}
}

// TS-09-6 (integration): a dry run with --adversarial runs the review,
// writes nothing, and reports the review a real run reports.
//
// Verifies: 09-REQ-1.6
func TestTS09_6_ADryRunReviewsAndWritesNothing(t *testing.T) {
	script := func() *scriptedReviewer {
		return &scriptedReviewer{t: t,
			reviews:   []reviewOutcome{{severities: []string{"major"}}, {}},
			revisions: []revisionOutcome{{}},
		}
	}
	dry := script()
	o := adversarialOptions(t, newAuthor(t, "01", "test_feature"), dry, 2)
	o.DryRun = true
	d, err := Run(context.Background(), o)
	if err != nil {
		t.Fatal(err)
	}
	if reviews, revisions := dry.counts(); reviews != 2 || revisions != 1 {
		t.Errorf("the dry run ran %d reviews, %d revisions", reviews, revisions)
	}
	if _, err := os.Stat(filepath.Join(o.Workspace.Root(), ".specs")); !os.IsNotExist(err) {
		t.Error("the dry run wrote a spec root")
	}

	real := script()
	r, err := Run(context.Background(), adversarialOptions(t, newAuthor(t, "01", "test_feature"), real, 2))
	if err != nil {
		t.Fatal(err)
	}
	if d.Review == nil || !reflect.DeepEqual(d.Review, r.Review) {
		t.Errorf("dry review %+v, real review %+v", d.Review, r.Review)
	}
}

// TS-09-45 (integration): for an undivided input the rounds run after the
// PRD phase and --name, and before the package is reserved and generated.
//
// Verifies: 09-REQ-8.1
func TestTS09_45_TheRoundsRunBeforeThePackageIsReserved(t *testing.T) {
	a := newAuthor(t, "01", "custom_name")
	var log []string
	rv := &scriptedReviewer{t: t, reviews: []reviewOutcome{{severities: []string{"minor"}}}, revisions: []revisionOutcome{{}}}
	o := adversarialOptions(t, a, rv, 1)
	o.Name = "custom_name"
	a.prd.SpecName = "model_name"
	specsDir := filepath.Join(o.Workspace.Root(), ".specs")
	reserved := func() bool {
		_, err := os.Stat(filepath.Join(specsDir, "01_custom_name"))
		return err == nil
	}
	rv.onReview = func(req reviewRequest) {
		log = append(log, "prd_review")
		if len(a.prdRequests) != 1 || req.PRD.SpecName != "custom_name" || reserved() {
			t.Errorf("review: %d PRD phases, name %q, reserved %v", len(a.prdRequests), req.PRD.SpecName, reserved())
		}
	}
	rv.onRevise = func(revisionRequest) {
		log = append(log, "prd_revision")
		if reserved() {
			t.Error("the package was reserved before the revision")
		}
	}
	a.onGenerate = func(req artifactRequest) {
		if req.Step == afspec.StepRequirements {
			log = append(log, "reserved:"+map[bool]string{true: "yes", false: "no"}[reserved()], "requirements")
		}
	}
	if _, err := Run(context.Background(), o); err != nil {
		t.Fatal(err)
	}
	if want := []string{"prd_review", "prd_revision", "reserved:yes", "requirements"}; !slices.Equal(log, want) {
		t.Errorf("order = %v, want %v", log, want)
	}
}

// TS-09-53 (integration): the final PRD is what prd.md, the comment, the
// result and every later phase are made from.
//
// Verifies: 09-REQ-9.4
func TestTS09_53_TheFinalPRDFeedsEverythingAfterIt(t *testing.T) {
	ws := newWorkspace(t)
	if err := os.WriteFile(filepath.Join(ws.Root(), "cache.go"), []byte("package x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	a := &recordingAuthor{scriptedAuthor: newAuthor(t, "01", "test_feature")}
	a.arch = "# Architecture\n"
	revBody := a.prd.Body + "\n\nREVISED-BODY-MARK\n"
	revQuestions := []OpenQuestion{{Question: "REVISED-OQ", Decision: "d", Why: "w"}}
	revFiles := []RelevantFile{{Path: "cache.go", Why: "REVISED-RF"}}
	rv := &scriptedReviewer{t: t, reviews: []reviewOutcome{{severities: []string{"minor"}}},
		revisions: []revisionOutcome{{edit: func(p PRD) PRD {
			p.Body, p.OpenQuestions, p.RelevantFiles, p.CodebaseNotes = revBody, revQuestions, revFiles, "REVISED-NOTES"
			return p
		}}}}
	forge := &mockForgeClient{authenticated: true, commentURL: "https://forge/c/1"}
	o := newOptions(ws, a)
	o.Adversarial, o.AdversarialRounds, o.reviewer = true, 1, rv
	o.Architecture, o.Comment, o.Forge = true, true, forge
	o.Input = cli.Input{Kind: cli.KindIssue, Origin: "https://github.com/a/b/issues/1", Body: "an idea",
		Issue: &issuex.IssueRef{Repo: issuex.Repo{Owner: "a", Name: "b"}, Number: 1}}
	var genReqs []artifactRequest
	a.onGenerate = func(req artifactRequest) { genReqs = append(genReqs, req) }

	res, err := Run(context.Background(), o)
	if err != nil {
		t.Fatal(err)
	}
	prdMD, err := os.ReadFile(filepath.Join(ws.Root(), res.SpecDir, "prd.md"))
	if err != nil || !strings.Contains(string(prdMD), "REVISED-BODY-MARK") {
		t.Errorf("prd.md does not have the revised body (%v)", err)
	}
	if !strings.Contains(forge.lastBody, "REVISED-BODY-MARK") || !strings.Contains(forge.lastBody, "REVISED-OQ") {
		t.Errorf("the comment is not the revised PRD:\n%s", forge.lastBody)
	}
	if !reflect.DeepEqual(res.OpenQuestions, revQuestions) || !reflect.DeepEqual(res.RelevantFiles, revFiles) {
		t.Errorf("result open questions %+v, relevant files %+v", res.OpenQuestions, res.RelevantFiles)
	}
	if len(genReqs) != 3 {
		t.Fatalf("%d generation phases", len(genReqs))
	}
	for _, req := range genReqs {
		if !strings.Contains(req.PRD, "REVISED-BODY-MARK") || req.CodebaseNotes != "REVISED-NOTES" ||
			!reflect.DeepEqual(req.RelevantFiles, revFiles) {
			t.Errorf("%s was sent the PRD before its revision", req.Step)
		}
	}
	if len(a.archReqs) != 1 || !strings.Contains(a.archReqs[0].PRD, "REVISED-BODY-MARK") ||
		a.archReqs[0].CodebaseNotes != "REVISED-NOTES" || !reflect.DeepEqual(a.archReqs[0].RelevantFiles, revFiles) {
		t.Errorf("the architecture phase was sent %+v", a.archReqs)
	}
}

// TS-09-54 (integration): no review data reaches the package directory or
// the comment.
//
// Verifies: 09-REQ-9.5
func TestTS09_54_TheReviewStaysOutOfThePackageAndTheComment(t *testing.T) {
	a := newAuthor(t, "01", "test_feature")
	rv := &scriptedReviewer{t: t, reviews: []reviewOutcome{{severities: []string{"major"}, mark: "FINDING-MARK"}, {}},
		revisions: []revisionOutcome{{reject: []int{0}}}}
	forge := &mockForgeClient{authenticated: true, commentURL: "https://forge/c/1"}
	o := adversarialOptions(t, a, rv, 2)
	o.Comment, o.Forge = true, forge
	o.Input = cli.Input{Kind: cli.KindIssue, Origin: "https://github.com/a/b/issues/1", Body: "an idea",
		Issue: &issuex.IssueRef{Repo: issuex.Repo{Owner: "a", Name: "b"}, Number: 1}}
	res, err := Run(context.Background(), o)
	if err != nil {
		t.Fatal(err)
	}
	if res.Review == nil || !strings.Contains(res.Review.Rounds[0].Findings[0].Flaw, "FINDING-MARK") {
		t.Fatalf("review = %+v", res.Review)
	}
	dir := filepath.Join(o.Workspace.Root(), res.SpecDir)
	files := packageFiles(t, dir)
	if !slices.Equal(files, []string{"prd.md", "requirements.json", "tasks.json", "test_spec.json"}) {
		t.Errorf("package files = %v", files)
	}
	for _, f := range files {
		b, _ := os.ReadFile(filepath.Join(dir, f))
		if strings.Contains(string(b), "FINDING-MARK") || strings.Contains(string(b), "R1-F1") {
			t.Errorf("%s carries review data", f)
		}
	}
	if strings.Contains(forge.lastBody, "FINDING-MARK") || strings.Contains(strings.ToLower(forge.lastBody), "review:") {
		t.Errorf("the comment carries review data:\n%s", forge.lastBody)
	}
}

// The run-level halves of TS-09-39 and TS-09-43: a failed review still
// produces the package, from the PRD phase's PRD, with the exit code of a
// run without the failure; an interrupt produces none.
//
// Verifies: 09-REQ-7.1, 09-REQ-7.5
func TestTS09_39_43_AFailedReviewKeepsThePackageAndAnInterruptDoesNot(t *testing.T) {
	a := newAuthor(t, "01", "test_feature")
	rv := &scriptedReviewer{t: t, reviews: []reviewOutcome{{err: phaseErr(agentrun.PhasePRDReview, agentrun.CategoryBudget)}}}
	o := adversarialOptions(t, a, rv, 1)
	res, err := Run(context.Background(), o)
	if err != nil || res.SpecDir == "" || res.Review == nil || res.Review.StopReason != envelope.ReviewStopIncomplete {
		t.Fatalf("err %v, result %+v", err, res)
	}
	prdMD, _ := os.ReadFile(filepath.Join(o.Workspace.Root(), res.SpecDir, "prd.md"))
	if !strings.Contains(string(prdMD), strings.TrimSpace(strings.SplitN(a.prd.Body, "\n", 2)[0])) {
		t.Error("prd.md is not the PRD phase's PRD")
	}

	ctx, cancel := context.WithCancel(context.Background())
	a = newAuthor(t, "01", "test_feature")
	rv = &scriptedReviewer{t: t, reviews: []reviewOutcome{{err: phaseErr(agentrun.PhasePRDReview, agentrun.CategoryAborted)}},
		onReview: func(reviewRequest) { cancel() }}
	o = adversarialOptions(t, a, rv, 1)
	res, err = Run(ctx, o)
	var f *Failure
	if !errors.As(err, &f) || f.Stage != agentrun.PhasePRDReview || f.Category != agentrun.CategoryAborted {
		t.Fatalf("err = %v", err)
	}
	if len(a.steps) != 0 || (res != nil && res.SpecDir != "") {
		t.Errorf("an interrupted run generated %v", a.steps)
	}
	if _, err := os.Stat(filepath.Join(o.Workspace.Root(), ".specs", "01_test_feature")); !os.IsNotExist(err) {
		t.Error("an interrupted run reserved a package")
	}
}

// TS-09-57 (integration): each review and revision is reported as a phase,
// labelled with its scope and its round of the limit, and followed by the
// findings by severity or the answers by disposition.
//
// Verifies: 09-REQ-10.1
func TestTS09_57_TheReviewIsReportedAsPhases(t *testing.T) {
	var stderr strings.Builder
	rv := &scriptedReviewer{t: t,
		reviews:   []reviewOutcome{{}, {severities: []string{"critical", "minor", "minor"}}, {}, {}},
		revisions: []revisionOutcome{{reject: []int{1}}},
	}
	o := fileOptions(newWorkspace(t), splitAuthor(t))
	o.Adversarial, o.AdversarialRounds, o.reviewer = true, 2, rv
	o.Progress = cli.NewProgress(&lockedWriter{w: &stderr}, "spec", false, false, false)
	if _, err := Run(context.Background(), o); err != nil {
		t.Fatal(err)
	}
	out := stderr.String()
	for _, want := range []string{
		"[spec] scope 2 of 3 (widget_github): reviewing the PRD (round 1 of 2)\n",
		"[spec] scope 2 of 3 (widget_github): revising the PRD (round 1 of 2)\n",
		"[spec] scope 2 of 3 (widget_github): reviewing the PRD (round 2 of 2)\n",
		"[spec] scope 1 of 3 (widget_core): reviewing the PRD (round 1 of 2)\n",
		"1 critical, 2 minor",
		"2 accepted, 1 rejected",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("stderr has no %q:\n%s", want, out)
		}
	}
	// The footer is every phase's (D-2): turns, tokens and cost.
	footer := cli.PhaseSummary(agentrun.Result{Name: agentrun.PhasePRDReview, Turns: 2, Model: fakeModel})
	if !strings.Contains(out, "scope 2 of 3 (widget_github): reviewing the PRD (round 1 of 2) done (") ||
		!strings.Contains(out, footer) {
		t.Errorf("stderr has no phase footer %q:\n%s", footer, out)
	}
	if strings.Index(out, "1 critical, 2 minor") < strings.Index(out, "scope 2 of 3 (widget_github): reviewing the PRD (round 1 of 2) done") {
		t.Error("the severity line comes before the review ends")
	}
}

// lockedWriter serialises writes, as stderr is shared by the coordinator and
// the package goroutines.
type lockedWriter struct {
	mu sync.Mutex
	w  *strings.Builder
}

func (l *lockedWriter) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.w.Write(p)
}

// TS-09-58 (integration): each review and revision is recorded in
// usage.phases under its own name, with its scope, model, effort, turns and
// cost, and counts toward the run's spend.
//
// Verifies: 09-REQ-10.2
func TestTS09_58_TheReviewIsRecordedInUsage(t *testing.T) {
	a := newAuthor(t, "01", "test_feature")
	a.prdCost = 0.5
	rv := &scriptedReviewer{t: t, reviews: []reviewOutcome{{severities: []string{"minor"}, cost: 0.25}},
		revisions: []revisionOutcome{{cost: 0.125}}}
	o := adversarialOptions(t, a, rv, 1)
	if _, err := Run(context.Background(), o); err != nil {
		t.Fatal(err)
	}
	env := o.Run.Envelope(cli.ExitOK, nil, nil)
	var names []string
	byName := map[string]cli.PhaseInfo{}
	for _, p := range env.Usage.Phases {
		names = append(names, p.Name)
		byName[p.Name] = p
	}
	if want := []string{"prd", "prd_review", "prd_revision", "generate:requirements", "generate:test_spec",
		"generate:tasks"}; !slices.Equal(names, want) {
		t.Errorf("usage phases = %v, want %v", names, want)
	}
	for name, cost := range map[string]float64{"prd_review": 0.25, "prd_revision": 0.125} {
		p := byName[name]
		if p.Scope != "test_feature" || p.Model != "fake-model" || p.Effort != "high" || p.Turns == 0 || p.CostUSD != cost {
			t.Errorf("%s = %+v", name, p)
		}
	}
	if got := o.Run.CostUSD(); got != 0.875 {
		t.Errorf("spend = %v", got)
	}
	raw, _ := json.Marshal(env.Usage.Phases)
	if strings.Contains(string(raw), `"round"`) {
		t.Errorf("a usage entry carries a round: %s", raw)
	}
}
