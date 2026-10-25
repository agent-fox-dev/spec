package specgen

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math/rand"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/agent-fox-dev/spec/envelope"
	"github.com/agent-fox-dev/spec/internal/agentrun"
	"github.com/agent-fox-dev/spec/internal/cli"
)

// roundEnv is a run environment for the round loop alone.
func roundEnv(t *testing.T, rv reviewer, limit int) *runEnv {
	t.Helper()
	ws := newWorkspace(t)
	o := newOptions(ws, nil)
	o.Adversarial, o.AdversarialRounds, o.reviewer = true, limit, rv
	return &runEnv{o: o, root: ws.Root(), specsDir: resolveSpecsDir(o, ws.Root()), reviewer: rv}
}

func roundPRD() PRD {
	return PRD{SpecName: "widget_cache", Title: "Widget cache", Body: "## Intent\n\nCache widgets.\n"}
}

func incompleteWarnings(e *runEnv) []cli.Warning {
	var out []cli.Warning
	for _, w := range e.o.Run.Warnings() {
		if w.Code == cli.WarnPRDReviewIncomplete {
			out = append(out, w)
		}
	}
	return out
}

// Another round follows only a round whose review raised a critical or major
// finding, and only below the limit.
func TestRunRounds_StopForTheRightReason(t *testing.T) {
	for _, tc := range []struct {
		name               string
		limit              int
		reviews            []reviewOutcome
		revisions          []revisionOutcome
		wantReviews, wantR int
		stop               string
	}{
		{"a review with no findings", 3, []reviewOutcome{{}}, nil, 1, 0, envelope.ReviewStopNoFindings},
		{"only minor findings", 5, []reviewOutcome{{severities: []string{"minor", "minor"}}}, []revisionOutcome{{}},
			1, 1, envelope.ReviewStopNoSeriousFindings},
		{"a major finding, then none", 2, []reviewOutcome{{severities: []string{"major", "minor"}}, {}}, []revisionOutcome{{}},
			2, 1, envelope.ReviewStopNoFindings},
		{"a critical finding at the limit", 2,
			[]reviewOutcome{{severities: []string{"critical"}}, {severities: []string{"critical"}}}, []revisionOutcome{{}, {}},
			2, 2, envelope.ReviewStopRoundsExhausted},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rv := &scriptedReviewer{t: t, reviews: tc.reviews, revisions: tc.revisions}
			prd := roundPRD()
			out, review, err := roundEnv(t, rv, tc.limit).runRounds(context.Background(), prd, "", nil)
			if err != nil {
				t.Fatal(err)
			}
			if reviews, revisions := rv.counts(); reviews != tc.wantReviews || revisions != tc.wantR {
				t.Errorf("reviews %d, revisions %d; want %d, %d", reviews, revisions, tc.wantReviews, tc.wantR)
			}
			if review.StopReason != tc.stop || review.RoundsRun != tc.wantReviews || review.RoundsAllowed != tc.limit {
				t.Errorf("review = %+v", review)
			}
			for i, round := range review.Rounds {
				if round.Round != i+1 || round.Findings == nil {
					t.Errorf("round %d = %+v", i+1, round)
				}
			}
			if (tc.wantR == 0) != (out.Body == prd.Body) {
				t.Errorf("body %q after %d revisions", out.Body, tc.wantR)
			}
		})
	}
}

// Each round reviews the PRD the previous revision left, the revision is sent
// the round's findings by id, and the result is the last revised PRD.
func TestRunRounds_ReviewWhatTheLastRevisionLeft(t *testing.T) {
	rv := &scriptedReviewer{t: t,
		reviews:   []reviewOutcome{{severities: []string{"major", "minor"}}, {}},
		revisions: []revisionOutcome{{}},
	}
	out, review, err := roundEnv(t, rv, 2).runRounds(context.Background(), roundPRD(), "", nil)
	if err != nil {
		t.Fatal(err)
	}
	if f := rv.reviseReqs[0].Findings; len(f) != 2 || f[0].ID != "R1-F1" || f[1].ID != "R1-F2" {
		t.Errorf("the revision was sent %+v", f)
	}
	if !strings.Contains(rv.reviewReqs[1].PRD.Body, "revised in round 1") || out.Body != rv.reviewReqs[1].PRD.Body {
		t.Errorf("round 2 reviewed %q; the result is %q", rv.reviewReqs[1].PRD.Body, out.Body)
	}
	for _, f := range review.Rounds[0].Findings {
		if f.Disposition != envelope.DispositionAccepted {
			t.Errorf("finding %s is %q", f.ID, f.Disposition)
		}
	}
}

// A later round's reviewer and reviser are both sent every earlier finding
// with its id, disposition and reason, and the reviewer the rule on rejected
// findings; round 1 has no earlier rounds.
func TestRunRounds_SendLaterRoundsTheEarlierFindings(t *testing.T) {
	rv := &scriptedReviewer{t: t,
		reviews:   []reviewOutcome{{severities: []string{"major", "major"}}, {severities: []string{"minor"}}},
		revisions: []revisionOutcome{{reject: []int{1}}, {}},
	}
	if _, _, err := roundEnv(t, rv, 2).runRounds(context.Background(), roundPRD(), "", nil); err != nil {
		t.Fatal(err)
	}
	earlier := rv.reviewReqs[1].Earlier
	if len(earlier) != 2 || earlier[0].Disposition != envelope.DispositionAccepted ||
		earlier[1].Disposition != envelope.DispositionRejected || earlier[1].Reason != "REASON-R1-F2" {
		t.Errorf("round 2 review was sent %+v", earlier)
	}
	review, revision := prdReviewUserPrompt(rv.reviewReqs[1]), prdRevisionUserPrompt(rv.reviseReqs[1])
	for name, prompt := range map[string]string{"review": review, "revision": revision} {
		for _, want := range []string{"R1-F1", "R1-F2", "accepted", "rejected", "REASON-R1-F2"} {
			if !strings.Contains(prompt, want) {
				t.Errorf("the round 2 %s prompt has no %q", name, want)
			}
		}
	}
	if !strings.Contains(review, "only with evidence the rejection did not answer") || !strings.Contains(review, "`reopens`") ||
		!strings.Contains(revision, "Do not undo what an earlier round accepted") {
		t.Error("round 2 is not told the rule on earlier findings")
	}
	if len(rv.reviewReqs[0].Earlier) != 0 || strings.Contains(prdReviewUserPrompt(rv.reviewReqs[0]), "Earlier rounds") ||
		strings.Contains(prdRevisionUserPrompt(rv.reviseReqs[0]), "Earlier rounds") {
		t.Error("round 1 was sent earlier rounds")
	}
}

// outcomeSequence is a random script for up to limit rounds. Each phase
// costs 0 or 1, so a total budget can stop the rounds anywhere.
func outcomeSequence(rng *rand.Rand, limit int) ([]reviewOutcome, []revisionOutcome) {
	sevs := []string{"critical", "major", "minor"}
	var reviews []reviewOutcome
	var revisions []revisionOutcome
	for range limit {
		r := reviewOutcome{cost: float64(rng.Intn(2))}
		switch rng.Intn(5) {
		case 0:
			r.err = errors.New("review failed")
		case 1:
			// no findings
		default:
			for range 1 + rng.Intn(4) {
				r.severities = append(r.severities, sevs[rng.Intn(3)])
			}
		}
		reviews = append(reviews, r)
		v := revisionOutcome{cost: float64(rng.Intn(2))}
		if rng.Intn(5) == 0 {
			v.err = errors.New("revision failed")
		}
		for n := range 4 {
			if rng.Intn(3) == 0 {
				v.reject = append(v.reject, n)
			}
		}
		revisions = append(revisions, v)
	}
	return reviews, revisions
}

// expectedStop applies the stop rules to a script, as a reference the loop is
// checked against: the reason, and how many reviews run. budget is the total
// budget, 0 for none.
func expectedStop(limit int, budget float64, reviews []reviewOutcome, revisions []revisionOutcome) (string, int) {
	spent := 0.0
	over := func() bool { return budget > 0 && spent >= budget }
	for round := 1; round <= limit; round++ {
		if over() {
			return envelope.ReviewStopIncomplete, round - 1
		}
		r := reviews[round-1]
		spent += r.cost
		if r.err != nil {
			return envelope.ReviewStopIncomplete, round
		}
		if len(r.severities) == 0 {
			return envelope.ReviewStopNoFindings, round
		}
		if over() {
			return envelope.ReviewStopIncomplete, round
		}
		v := revisions[round-1]
		spent += v.cost
		if v.err != nil {
			return envelope.ReviewStopIncomplete, round
		}
		if !slices.ContainsFunc(r.severities, func(s string) bool { return s != "minor" }) {
			return envelope.ReviewStopNoSeriousFindings, round
		}
		if round == limit {
			return envelope.ReviewStopRoundsExhausted, round
		}
	}
	return "", 0
}

// For any limit and any outcomes, failures and budget stops included, the
// loop runs at most the limit of reviews and ends with the one stop reason
// the rules give.
func TestRunRounds_AreBoundedWithOneStopReason(t *testing.T) {
	rng := rand.New(rand.NewSource(38))
	e := roundEnv(t, nil, 1)
	for range 1000 {
		limit := 1 + rng.Intn(5)
		budget := float64(rng.Intn(4)) // 0 is no total budget
		reviews, revisions := outcomeSequence(rng, limit)
		rv := &scriptedReviewer{t: t, reviews: reviews, revisions: revisions}
		e.reviewer, e.o.Run = rv, cli.NewRun("spec", "test")
		e.o.AdversarialRounds, e.o.TotalBudgetUSD = limit, budget
		_, review, err := e.runRounds(context.Background(), roundPRD(), "", nil)
		if err != nil {
			t.Fatal(err)
		}
		nReviews, _ := rv.counts()
		want, wantReviews := expectedStop(limit, budget, reviews, revisions)
		if review.StopReason != want || nReviews != wantReviews || review.RoundsRun > limit {
			t.Fatalf("limit %d, budget %v: stop %q after %d reviews (%d rounds), want %q after %d",
				limit, budget, review.StopReason, nReviews, review.RoundsRun, want, wantReviews)
		}
	}
}

// A review's totals agree with its rounds whatever the findings and their
// dispositions.
func TestBuildReview_TotalsAgreeWithTheRounds(t *testing.T) {
	rng := rand.New(rand.NewSource(52))
	dispositions := []string{envelope.DispositionAccepted, envelope.DispositionRejected, envelope.DispositionUnanswered}
	for range 1000 {
		var rounds []ReviewRound
		total, accepted := 0, 0
		for r := range rng.Intn(6) {
			round := ReviewRound{Round: r + 1, Findings: []Finding{}}
			for n := range rng.Intn(5) {
				d := dispositions[rng.Intn(3)]
				if d == envelope.DispositionAccepted {
					accepted++
				}
				round.Findings = append(round.Findings, Finding{ID: fmt.Sprintf("R%d-F%d", r+1, n+1), Disposition: d})
			}
			total += len(round.Findings)
			rounds = append(rounds, round)
		}
		review := buildReview(rounds, 5, envelope.ReviewStopIncomplete)
		if review.FindingsCount != total || review.AcceptedCount != accepted ||
			review.FindingsCount != review.AcceptedCount+review.RejectedCount+review.UnansweredCount ||
			review.RoundsRun != len(rounds) {
			t.Fatalf("review = %+v for %d findings in %d rounds", review, total, len(rounds))
		}
	}
}

// A review or revision that fails, for any reason but an interrupt, stops the
// rounds with a high warning naming the PRD, the round, the phase and why,
// and the run goes on from the last complete PRD.
func TestRunRounds_AFailedPhaseStopsTheRoundsWithAWarning(t *testing.T) {
	failures := map[string]error{
		"turn limit": phaseErr(agentrun.PhasePRDReview, agentrun.CategoryMaxTurns),
		"timeout":    phaseErr(agentrun.PhasePRDReview, agentrun.CategoryAborted),
		"refusal":    phaseErr(agentrun.PhasePRDReview, agentrun.CategoryRefusal),
		"api error":  phaseErr(agentrun.PhasePRDReview, agentrun.CategoryAPI),
		"internal":   errors.New("an internal error"),
		"no result":  phaseErr(agentrun.PhasePRDReview, agentrun.CategoryNoResult),
	}
	for name, failure := range failures {
		for _, phase := range []string{agentrun.PhasePRDReview, agentrun.PhasePRDRevision} {
			t.Run(name+" in "+phase, func(t *testing.T) {
				rv := &scriptedReviewer{t: t, reviews: []reviewOutcome{{err: failure}}}
				if phase == agentrun.PhasePRDRevision {
					rv.reviews = []reviewOutcome{{severities: []string{"major"}}}
					rv.revisions = []revisionOutcome{{err: failure}}
				}
				e := roundEnv(t, rv, 2)
				prd := roundPRD()
				out, review, err := e.runRounds(context.Background(), prd, "", nil)
				if err != nil {
					t.Fatalf("the round loop failed the run: %v", err)
				}
				if review.StopReason != envelope.ReviewStopIncomplete || out.Body != prd.Body {
					t.Errorf("stop %q, body %q", review.StopReason, out.Body)
				}
				ws := incompleteWarnings(e)
				if len(ws) != 1 || ws[0].Severity != "high" {
					t.Fatalf("warnings %+v", e.o.Run.Warnings())
				}
				for _, want := range []string{prd.SpecName, "round 1", phase, failure.Error()} {
					if !strings.Contains(ws[0].Message, want) {
						t.Errorf("the warning does not name %q: %s", want, ws[0].Message)
					}
				}
			})
		}
	}

	t.Run("in a later round", func(t *testing.T) {
		rv := &scriptedReviewer{t: t,
			reviews:   []reviewOutcome{{severities: []string{"major"}}, {err: phaseErr(agentrun.PhasePRDReview, agentrun.CategoryAPI)}},
			revisions: []revisionOutcome{{}},
		}
		e := roundEnv(t, rv, 3)
		out, review, err := e.runRounds(context.Background(), roundPRD(), "", nil)
		if err != nil || !strings.Contains(out.Body, "revised in round 1") || review.StopReason != envelope.ReviewStopIncomplete ||
			review.RoundsRun != 1 {
			t.Errorf("err %v, body %q, review %+v; want the earlier round's revision kept", err, out.Body, review)
		}
		if ws := incompleteWarnings(e); len(ws) != 1 || !strings.Contains(ws[0].Message, "round 2") {
			t.Errorf("warnings = %+v", ws)
		}
	})
}

// The findings of a round whose revision failed are unanswered, and the PRD is
// the one the review read.
func TestRunRounds_AFailedRevisionLeavesItsFindingsUnanswered(t *testing.T) {
	rv := &scriptedReviewer{t: t, reviews: []reviewOutcome{{severities: []string{"major", "minor"}}},
		revisions: []revisionOutcome{{err: phaseErr(agentrun.PhasePRDRevision, agentrun.CategoryMaxTurns)}}}
	prd := roundPRD()
	out, review, err := roundEnv(t, rv, 2).runRounds(context.Background(), prd, "", nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range review.Rounds[0].Findings {
		if f.Disposition != envelope.DispositionUnanswered || f.Reason != "" {
			t.Errorf("finding %+v", f)
		}
	}
	if review.UnansweredCount != 2 || out.Body != prd.Body {
		t.Errorf("review %+v, body %q", review, out.Body)
	}
}

// A review or revision is never started past --total-budget: the rounds stop
// with a warning naming the budget, and a review whose own cost reached it
// leaves its findings unanswered.
func TestRunRounds_StopAtTheTotalBudget(t *testing.T) {
	t.Run("spent before the first review", func(t *testing.T) {
		rv := &scriptedReviewer{t: t}
		e := roundEnv(t, rv, 2)
		e.o.Run.AddPhase(cli.PhaseInfo{Name: "prd", CostUSD: 3.5})
		e.o.TotalBudgetUSD = 3.5
		out, review, err := e.runRounds(context.Background(), roundPRD(), "", nil)
		if err != nil {
			t.Fatal(err)
		}
		if reviews, _ := rv.counts(); reviews != 0 || review.RoundsRun != 0 || review.StopReason != envelope.ReviewStopIncomplete ||
			out.Body != roundPRD().Body {
			t.Errorf("%d reviews, review %+v", reviews, review)
		}
		ws := incompleteWarnings(e)
		if len(ws) != 1 {
			t.Fatalf("warnings = %+v", e.o.Run.Warnings())
		}
		for _, want := range []string{"total budget", "$3.50", "round 1", agentrun.PhasePRDReview} {
			if !strings.Contains(ws[0].Message, want) {
				t.Errorf("the warning does not say %q: %s", want, ws[0].Message)
			}
		}
	})
	t.Run("reached by the review", func(t *testing.T) {
		rv := &scriptedReviewer{t: t, reviews: []reviewOutcome{{severities: []string{"critical", "minor"}, cost: 2}}}
		e := roundEnv(t, rv, 2)
		e.o.TotalBudgetUSD = 2
		_, review, err := e.runRounds(context.Background(), roundPRD(), "", nil)
		if err != nil {
			t.Fatal(err)
		}
		if _, revisions := rv.counts(); revisions != 0 || review.UnansweredCount != 2 || review.FindingsCount != 2 {
			t.Errorf("%d revisions, review %+v", revisions, review)
		}
		if ws := incompleteWarnings(e); len(ws) != 1 || !strings.Contains(ws[0].Message, agentrun.PhasePRDRevision) ||
			!strings.Contains(ws[0].Message, "total budget") {
			t.Errorf("warnings = %+v", ws)
		}
	})
}

// An interrupt during a review or a revision stops the run as aborted, at the
// phase it stopped, with no review warning and no PRD to generate from.
func TestRunRounds_AnInterruptAbortsTheRun(t *testing.T) {
	for _, phase := range []string{agentrun.PhasePRDReview, agentrun.PhasePRDRevision} {
		t.Run(phase, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			aborted := phaseErr(phase, agentrun.CategoryAborted)
			rv := &scriptedReviewer{t: t, reviews: []reviewOutcome{{err: aborted}}, onReview: func(reviewRequest) { cancel() }}
			if phase == agentrun.PhasePRDRevision {
				rv = &scriptedReviewer{t: t, reviews: []reviewOutcome{{severities: []string{"major"}}},
					revisions: []revisionOutcome{{err: aborted}}, onRevise: func(revisionRequest) { cancel() }}
			}
			e := roundEnv(t, rv, 2)
			out, review, err := e.runRounds(ctx, roundPRD(), "scope 1 of 2 (widget_cache)", nil)
			f := asFailure(t, err)
			if f.Stage != phase || f.Category != agentrun.CategoryAborted || cli.ExitCodeFor(f.Category) != cli.ExitFailed ||
				!strings.HasPrefix(f.Error(), "scope 1 of 2 (widget_cache): ") {
				t.Errorf("failure %s/%s %q", f.Stage, f.Category, f.Error())
			}
			if len(incompleteWarnings(e)) != 0 || review != nil || out.SpecName != "" {
				t.Errorf("warnings %+v, handed on %+v, %+v", incompleteWarnings(e), out, review)
			}
		})
	}
}

// Each review and revision is recorded in usage under its own name, with its
// scope, model, effort, turns and cost, whether it finished or not, so the
// budget check before the next phase counts it.
func TestRunRounds_RecordEveryPhaseInUsage(t *testing.T) {
	rv := &scriptedReviewer{t: t,
		reviews: []reviewOutcome{{severities: []string{"major"}, cost: 0.25}, {severities: []string{"major"}, cost: 0.125}},
		revisions: []revisionOutcome{{cost: 0.5},
			{err: phaseErr(agentrun.PhasePRDRevision, agentrun.CategoryBudget), cost: 0.0625}}}
	e := roundEnv(t, rv, 2)
	if _, _, err := e.runRounds(context.Background(), roundPRD(), "", nil); err != nil {
		t.Fatal(err)
	}
	env := e.o.Run.Envelope(cli.ExitOK, nil, nil)
	var names []string
	for _, p := range env.Usage.Phases {
		names = append(names, p.Name)
		if p.Scope != "widget_cache" || p.Model != "fake-model" || p.Effort != "high" || p.Turns == 0 || p.CostUSD == 0 {
			t.Errorf("%s = %+v", p.Name, p)
		}
	}
	if want := []string{"prd_review", "prd_revision", "prd_review", "prd_revision"}; !slices.Equal(names, want) {
		t.Errorf("usage phases = %v, want %v", names, want)
	}
	if got := e.o.Run.CostUSD(); got != 0.9375 {
		t.Errorf("spend = %v", got)
	}
	if raw, _ := json.Marshal(env.Usage.Phases); strings.Contains(string(raw), `"round"`) {
		t.Errorf("a usage entry carries a round: %s", raw)
	}
}

// lockedWriter serialises writes, as stderr is shared by the coordinator and
// the package goroutines.
type lockedWriter struct {
	mu sync.Mutex
	w  strings.Builder
}

func (l *lockedWriter) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.w.Write(p)
}

// Each review and revision is reported as a phase, labelled with its scope
// and its round of the limit, ended by every phase's footer, and followed by
// the findings by severity or the answers by disposition.
func TestRunRounds_ReportEachPhaseWithItsScopeAndRound(t *testing.T) {
	rv := &scriptedReviewer{t: t,
		reviews:   []reviewOutcome{{}, {severities: []string{"critical", "minor", "minor"}}, {}, {}},
		revisions: []revisionOutcome{{reject: []int{1}}},
	}
	var stderr lockedWriter
	o := fileOptions(newWorkspace(t), splitAuthor(t))
	o.Adversarial, o.AdversarialRounds, o.reviewer = true, 2, rv
	o.Progress = cli.NewProgress(&stderr, "spec", false, false, false)
	if _, err := Run(context.Background(), o); err != nil {
		t.Fatal(err)
	}
	out := stderr.w.String()
	footer := cli.PhaseSummary(agentrun.Result{Name: agentrun.PhasePRDReview, Turns: 2, Model: fakeModel})
	for _, want := range []string{
		"[spec] scope 1 of 3 (widget_core): reviewing the PRD (round 1 of 2)\n",
		"[spec] scope 2 of 3 (widget_github): reviewing the PRD (round 1 of 2)\n",
		"[spec] scope 2 of 3 (widget_github): revising the PRD (round 1 of 2)\n",
		"[spec] scope 2 of 3 (widget_github): reviewing the PRD (round 2 of 2)\n",
		"scope 2 of 3 (widget_github): reviewing the PRD (round 1 of 2) done (",
		footer, "1 critical, 2 minor", "2 accepted, 1 rejected", "no findings",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("stderr has no %q:\n%s", want, out)
		}
	}
	if strings.Index(out, "1 critical, 2 minor") < strings.Index(out, "scope 2 of 3 (widget_github): reviewing the PRD (round 1 of 2) done") {
		t.Error("the severity line comes before the review ends")
	}
}
