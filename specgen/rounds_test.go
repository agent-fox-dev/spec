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

	"github.com/agent-fox-dev/spec/afspec"
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

// The rounds stop as converged on two consecutive clean rounds, a clean round
// being one whose review raised nothing critical or major, and as exhausted at
// the limit otherwise; a limit of one never converges.
func TestRunRounds_ConvergeOnTwoConsecutiveCleanRounds(t *testing.T) {
	none, minor, major := reviewOutcome{}, reviewOutcome{severities: []string{"minor"}}, reviewOutcome{severities: []string{"major"}}
	for _, tc := range []struct {
		name    string
		limit   int
		reviews []reviewOutcome
		stop    string
		rounds  int
	}{
		{"clean then clean", 3, []reviewOutcome{none, minor}, envelope.ReviewStopConverged, 2},
		{"clean, major, clean, clean", 5, []reviewOutcome{minor, major, none, minor}, envelope.ReviewStopConverged, 4},
		{"major to the limit", 3, []reviewOutcome{major, major, major}, envelope.ReviewStopRoundsExhausted, 3},
		{"clean rounds that are not consecutive", 3, []reviewOutcome{none, major, minor}, envelope.ReviewStopRoundsExhausted, 3},
		{"one clean round at a limit of 1", 1, []reviewOutcome{none}, envelope.ReviewStopRoundsExhausted, 1},
		{"one minor round at a limit of 1", 1, []reviewOutcome{minor}, envelope.ReviewStopRoundsExhausted, 1},
		{"one major round at a limit of 1", 1, []reviewOutcome{major}, envelope.ReviewStopRoundsExhausted, 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rv := &scriptedReviewer{t: t, reviews: tc.reviews, revisions: make([]revisionOutcome, len(tc.reviews))}
			_, review, err := roundEnv(t, rv, tc.limit).runRounds(context.Background(), roundPRD(), "", nil)
			if err != nil {
				t.Fatal(err)
			}
			if review.StopReason != tc.stop || review.RoundsRun != tc.rounds || review.RoundsAllowed != tc.limit {
				t.Errorf("stop %q after %d rounds; want %q after %d", review.StopReason, review.RoundsRun, tc.stop, tc.rounds)
			}
			if reviews, _ := rv.counts(); reviews != tc.rounds {
				t.Errorf("%d reviews ran; want %d", reviews, tc.rounds)
			}
		})
	}
}

// A round whose review raised only minor findings is clean and still revised;
// one whose review raised nothing is clean and not revised; and a clean round
// stops nothing unless the round before it was clean.
func TestRunRounds_ReviseAMinorRoundAndNotAnEmptyOne(t *testing.T) {
	rv := &scriptedReviewer{t: t,
		reviews:   []reviewOutcome{{severities: []string{"major"}}, {severities: []string{"minor", "minor"}}, {}},
		revisions: []revisionOutcome{{}, {}},
	}
	_, review, err := roundEnv(t, rv, 3).runRounds(context.Background(), roundPRD(), "", nil)
	if err != nil {
		t.Fatal(err)
	}
	if got := []bool{review.Rounds[0].Clean, review.Rounds[1].Clean, review.Rounds[2].Clean}; !slices.Equal(got, []bool{false, true, true}) {
		t.Errorf("clean = %v; want false, true, true", got)
	}
	if reviews, revisions := rv.counts(); reviews != 3 || revisions != 2 {
		t.Errorf("reviews %d, revisions %d; want 3 and 2: the minor round revised, the empty one not", reviews, revisions)
	}
	for _, f := range review.Rounds[1].Findings {
		if f.Disposition != envelope.DispositionFixed {
			t.Errorf("%s of the minor round is %s; want it answered", f.ID, f.Disposition)
		}
	}
	if review.StopReason != envelope.ReviewStopConverged {
		t.Errorf("stop %q; want converged on rounds 2 and 3", review.StopReason)
	}
}

// A phase that does not finish stops the rounds as incomplete, whatever else
// held — a round that would have converged included — and the findings of a
// revision that did not finish are unanswered while the earlier rounds keep
// their answers.
func TestRunRounds_AnUnfinishedPhaseIsIncompleteWhateverElseHeld(t *testing.T) {
	failed := errors.New("the phase failed")
	minor, major := []string{"minor"}, []string{"major"}
	for _, tc := range []struct {
		name      string
		budget    float64
		reviews   []reviewOutcome
		revisions []revisionOutcome
		answered  []string // the dispositions of each round's first finding
	}{
		{"round 1's review", 0, []reviewOutcome{{err: failed}}, nil, nil},
		{"round 1's revision", 0, []reviewOutcome{{severities: major}}, []revisionOutcome{{err: failed}}, []string{"unanswered"}},
		{"round 2's revision, after a clean round 1 and a clean round 2", 0,
			[]reviewOutcome{{severities: minor}, {severities: minor}}, []revisionOutcome{{}, {err: failed}}, []string{"fixed", "unanswered"}},
		{"the budget before round 2", 1, []reviewOutcome{{severities: major}}, []revisionOutcome{{cost: 1}}, []string{"fixed"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rv := &scriptedReviewer{t: t, reviews: tc.reviews, revisions: tc.revisions}
			e := roundEnv(t, rv, 3)
			e.o.TotalBudgetUSD = tc.budget
			prd := roundPRD()
			out, review, err := e.runRounds(context.Background(), prd, "", nil)
			if err != nil {
				t.Fatal(err)
			}
			if review.StopReason != envelope.ReviewStopIncomplete {
				t.Errorf("stop %q; want incomplete", review.StopReason)
			}
			for i, want := range tc.answered {
				if got := review.Rounds[i].Findings[0]; got.Disposition != want || (want == "unanswered" && got.Reason != "") {
					t.Errorf("round %d: %+v; want %s", i+1, got, want)
				}
			}
			ws := incompleteWarnings(e)
			if len(ws) != 1 || ws[0].Severity != "high" || !strings.Contains(ws[0].Message, "widget_cache") {
				t.Errorf("warnings %+v; want one high warning naming the scope", ws)
			}
			if fixedRounds := slices.Index(tc.answered, "unanswered"); fixedRounds == 0 && out.Body != prd.Body {
				t.Errorf("the PRD %q is not the last complete one", out.Body)
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
		if f.Disposition != envelope.DispositionFixed {
			t.Errorf("finding %s is %q", f.ID, f.Disposition)
		}
	}
}

// A later round's reviewer and reviser are both sent every earlier finding
// with its id, disposition and the fields of its answer, and the reviewer the
// rules on repeats; round 1 has no earlier rounds.
func TestRunRounds_SendLaterRoundsTheEarlierFindings(t *testing.T) {
	rv := &scriptedReviewer{t: t,
		reviews:   []reviewOutcome{{severities: []string{"major", "major"}}, {severities: []string{"minor"}}},
		revisions: []revisionOutcome{{contest: []int{1}}, {}},
	}
	if _, _, err := roundEnv(t, rv, 2).runRounds(context.Background(), roundPRD(), "", nil); err != nil {
		t.Fatal(err)
	}
	earlier := rv.reviewReqs[1].Earlier
	if len(earlier) != 2 || earlier[0].Disposition != envelope.DispositionFixed ||
		earlier[1].Disposition != envelope.DispositionContested || earlier[1].Reason != "REASON-R1-F2" {
		t.Errorf("round 2 review was sent %+v", earlier)
	}
	review, revision := prdReviewUserPrompt(rv.reviewReqs[1]), prdRevisionUserPrompt(rv.reviseReqs[1])
	for name, prompt := range map[string]string{"review": review, "revision": revision} {
		for _, want := range []string{"R1-F1", "R1-F2", "fixed", "contested", "REASON-R1-F2", "COUNTER-R1-F2"} {
			if !strings.Contains(prompt, want) {
				t.Errorf("the round 2 %s prompt has no %q", name, want)
			}
		}
	}
	if !strings.Contains(review, "evidence the contest did not answer") || !strings.Contains(review, "`reopens`") ||
		!strings.Contains(revision, "Do not undo what an earlier round settled") {
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
				v.contest = append(v.contest, n)
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
	before := false // whether the round before was clean
	revised := 0    // revisions run: a round with no findings has none
	for round := 1; round <= limit; round++ {
		if over() {
			return envelope.ReviewStopIncomplete, round - 1
		}
		r := reviews[round-1]
		spent += r.cost
		if r.err != nil {
			return envelope.ReviewStopIncomplete, round
		}
		clean := !slices.ContainsFunc(r.severities, func(s string) bool { return s != "minor" })
		if len(r.severities) > 0 {
			if over() {
				return envelope.ReviewStopIncomplete, round
			}
			v := revisions[revised]
			revised++
			spent += v.cost
			if v.err != nil {
				return envelope.ReviewStopIncomplete, round
			}
		}
		if clean && before {
			return envelope.ReviewStopConverged, round
		}
		before = clean
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

// The record carries, per round, whether it was clean; per finding, its
// rubric and only the answer fields its disposition has; and six totals
// derived from the rounds, the first the sum of the other five.
func TestBuildReview_RecordsTheAnswersAndDerivesTheTotals(t *testing.T) {
	full := Disposition{Reason: "REASON", CounterEvidence: "COUNTER", Question: "QUESTION", Options: []string{"A.", "B."}, Decision: "DECISION"}
	answered := func(d string) Disposition { a := full; a.Disposition = d; return a }
	finding := func(id, severity, rubric string) Finding {
		return Finding{ID: id, Severity: severity, Rubric: rubric, Section: "s", Flaw: "f", Evidence: "e", Remedy: "r"}
	}
	round1 := []Finding{finding("R1-F1", "critical", "prd"), finding("R1-F2", "minor", "steering"), finding("R1-F3", "major", "steering")}
	answer(round1, []Disposition{
		withID(answered(envelope.DispositionFixed), "R1-F1"),
		withID(answered(envelope.DispositionContested), "R1-F2"),
		withID(answered(envelope.DispositionEscalated), "R1-F3"),
	})
	round2 := []Finding{finding("R2-F1", "minor", "prd"), finding("R2-F2", "minor", "prd")}
	answer(round2, []Disposition{withID(answered(envelope.DispositionDeclined), "R2-F1"), withID(answered(envelope.DispositionFixed), "R2-F2")})
	round3 := []Finding{finding("R3-F1", "major", "prd")}
	answer(round3, nil)

	review := buildReview([]ReviewRound{{Round: 1, Findings: round1}, {Round: 2, Findings: round2}, {Round: 3, Findings: round3}},
		3, envelope.ReviewStopIncomplete)
	if got := []bool{review.Rounds[0].Clean, review.Rounds[1].Clean, review.Rounds[2].Clean}; !slices.Equal(got, []bool{false, true, false}) {
		t.Errorf("clean = %v; want false, true, false", got)
	}
	type fields struct {
		reason, counter, question, decision string
		options                             int
	}
	want := map[string]fields{
		envelope.DispositionFixed:      {},
		envelope.DispositionContested:  {reason: "REASON", counter: "COUNTER"},
		envelope.DispositionEscalated:  {reason: "REASON", question: "QUESTION", decision: "DECISION", options: 2},
		envelope.DispositionDeclined:   {reason: "REASON"},
		envelope.DispositionUnanswered: {},
	}
	for _, round := range review.Rounds {
		for _, f := range round.Findings {
			got := fields{f.Reason, f.CounterEvidence, f.Question, f.Decision, len(f.Options)}
			if got != want[f.Disposition] {
				t.Errorf("%s (%s) carries %+v; want %+v", f.ID, f.Disposition, got, want[f.Disposition])
			}
			if f.Rubric != envelope.RubricPRD && f.Rubric != envelope.RubricSteering {
				t.Errorf("%s has rubric %q", f.ID, f.Rubric)
			}
		}
	}
	counts := []int{review.FixedCount, review.ContestedCount, review.EscalatedCount, review.DeclinedCount, review.UnansweredCount}
	if !slices.Equal(counts, []int{2, 1, 1, 1, 1}) || review.FindingsCount != 6 {
		t.Errorf("findings %d, counts %v; want 6 and 2, 1, 1, 1, 1", review.FindingsCount, counts)
	}

	// Whatever the answers, the findings count is the sum of the other five.
	rng := rand.New(rand.NewSource(52))
	dispositions := []string{envelope.DispositionFixed, envelope.DispositionContested, envelope.DispositionEscalated,
		envelope.DispositionDeclined, envelope.DispositionUnanswered}
	for range 500 {
		var rounds []ReviewRound
		total := 0
		for r := range rng.Intn(6) {
			round := ReviewRound{Round: r + 1, Findings: []Finding{}}
			for n := range rng.Intn(5) {
				round.Findings = append(round.Findings, Finding{ID: fmt.Sprintf("R%d-F%d", r+1, n+1), Disposition: dispositions[rng.Intn(5)]})
			}
			total += len(round.Findings)
			rounds = append(rounds, round)
		}
		v := buildReview(rounds, 5, envelope.ReviewStopIncomplete)
		if v.FindingsCount != total || v.FindingsCount != v.FixedCount+v.ContestedCount+v.EscalatedCount+v.DeclinedCount+v.UnansweredCount ||
			v.RoundsRun != len(rounds) {
			t.Fatalf("review = %+v for %d findings in %d rounds", v, total, len(rounds))
		}
	}
}

// withID is d answering the finding id.
func withID(d Disposition, id string) Disposition { d.ID = id; return d }

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
	// The first and last scopes converge on two empty reviews; the second
	// raises a critical finding, is revised, and stops at the limit.
	rv := &scriptedReviewer{t: t,
		reviews:   []reviewOutcome{{}, {}, {severities: []string{"critical", "minor", "minor"}}, {}, {}, {}},
		revisions: []revisionOutcome{{contest: []int{1}}},
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
		footer, "1 critical, 2 minor; the round is not clean", "2 fixed, 1 contested", "no findings; the round is clean",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("stderr has no %q:\n%s", want, out)
		}
	}
	if strings.Index(out, "1 critical, 2 minor") < strings.Index(out, "scope 2 of 3 (widget_github): reviewing the PRD (round 1 of 2) done") {
		t.Error("the severity line comes before the review ends")
	}
}

// planRounds runs the plan rounds of the fixture's plan with rv at limit.
func planRounds(t *testing.T, rv *scriptedReviewer, limit int, label string) (*runEnv, afspec.PartialSpec, *Review) {
	t.Helper()
	e := roundEnv(t, rv, limit)
	plan, review, err := e.runPlanRounds(context.Background(), roundPRD(), *planUnderReview(t), nil, label)
	if err != nil {
		t.Fatal(err)
	}
	return e, plan, review
}

// The plan rounds are the PRD's: the same ids, answers, convergence and
// record.
func TestRunPlanRounds_ApplyThePRDReviewsRules(t *testing.T) {
	escalation := Disposition{Disposition: "escalated", Question: "Q?", Options: []string{"A.", "B."}, Decision: "A.", Reason: "unsure"}
	rv := &scriptedReviewer{t: t,
		planReviews: []reviewOutcome{{severities: []string{"major", "major", "minor", "minor"}}, {severities: []string{"minor"}}, {}},
		planRevisions: []revisionOutcome{
			{contest: []int{1}, answers: map[int]Disposition{2: escalation, 3: {Disposition: "declined", Reason: "a later package"}}},
			{},
		},
	}
	_, plan, review := planRounds(t, rv, 3, "")
	if review.StopReason != envelope.ReviewStopConverged || review.RoundsRun != 3 {
		t.Fatalf("stop %q after %d rounds; want converged after 3", review.StopReason, review.RoundsRun)
	}
	if got := []bool{review.Rounds[0].Clean, review.Rounds[1].Clean, review.Rounds[2].Clean}; !slices.Equal(got, []bool{false, true, true}) {
		t.Errorf("clean = %v", got)
	}
	var dispositions []string
	for _, f := range review.Rounds[0].Findings {
		dispositions = append(dispositions, f.Disposition)
	}
	if !slices.Equal(dispositions, []string{"fixed", "contested", "escalated", "declined"}) || review.Rounds[0].Findings[3].ID != "R1-F4" {
		t.Errorf("round 1: %v, last id %s", dispositions, review.Rounds[0].Findings[3].ID)
	}
	if sum := review.FixedCount + review.ContestedCount + review.EscalatedCount + review.DeclinedCount + review.UnansweredCount; sum != review.FindingsCount || review.FindingsCount != 5 {
		t.Errorf("findings %d, the five counts sum to %d", review.FindingsCount, sum)
	}
	if plan.Tasks.Tasks[0].Title != "revised in round 2" {
		t.Errorf("the plan returned has task 1 titled %q; want the last revision's", plan.Tasks.Tasks[0].Title)
	}
	if earlier := rv.planReviewReqs[1].Earlier; len(earlier) != 4 || earlier[1].Disposition != "contested" {
		t.Errorf("round 2 was handed %+v; want round 1's findings with their answers", earlier)
	}

	// The tools hold a plan's answers and repeats to the same rules.
	settled := []Finding{{ID: "R1-F1", Severity: "major", Rubric: "zero_decision", Section: "task 1", Flaw: "open", Evidence: "e", Remedy: "r", Disposition: "declined", Reason: "later"}}
	var sink reviewSink
	repeat := planFinding()
	repeat.Section, repeat.Flaw = "task 1", "open"
	if r := submitReview(t, newPlanReviewTool(&sink, 2, settled, planUnderReview(t), nil, ""), []reviewInput{repeat}); r.OK ||
		!strings.Contains(r.Detail, "declined") {
		t.Errorf("a declined plan finding raised again: %+v", r)
	}
	r, _ := revisePlan(t, planUnderReview(t), planFindings("grounding"), planAsSubmitted(t, planUnderReview(t)), nil,
		contest("R1-F1", "it holds", "a quote that is nowhere"))
	if r.OK || !strings.Contains(r.Detail, "counter-evidence") {
		t.Errorf("a plan contest without counter-evidence the run finds: %+v", r)
	}
}

// A plan phase that does not finish stops the rounds as incomplete with a
// warning of the plan's own, and leaves its round's findings unanswered.
func TestRunPlanRounds_AnUnfinishedPhaseStopsTheRounds(t *testing.T) {
	failed := errors.New("the phase failed")
	major := []string{"major"}
	for _, tc := range []struct {
		name      string
		budget    float64
		reviews   []reviewOutcome
		revisions []revisionOutcome
		phase     string
		says      string
		answered  []string
		title     string
	}{
		{"round 1's review", 0, []reviewOutcome{{err: failed}}, nil, "plan_review", "the phase failed", nil, ""},
		{"round 1's revision", 0, []reviewOutcome{{severities: major}}, []revisionOutcome{{err: failed}}, "plan_revision", "the phase failed",
			[]string{"unanswered"}, ""},
		{"round 2's revision", 0, []reviewOutcome{{severities: major}, {severities: major}}, []revisionOutcome{{}, {err: failed}},
			"plan_revision", "the phase failed", []string{"fixed", "unanswered"}, "revised in round 1"},
		{"the budget before round 2's review", 1, []reviewOutcome{{severities: major}}, []revisionOutcome{{cost: 1}}, "plan_review", "total budget",
			[]string{"fixed"}, "revised in round 1"},
		{"the budget before round 2's revision", 2, []reviewOutcome{{severities: major}, {severities: major, cost: 1}}, []revisionOutcome{{cost: 1}},
			"plan_revision", "total budget", []string{"fixed", "unanswered"}, "revised in round 1"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rv := &scriptedReviewer{t: t, planReviews: tc.reviews, planRevisions: tc.revisions}
			e := roundEnv(t, rv, 3)
			e.o.TotalBudgetUSD = tc.budget
			original := *planUnderReview(t)
			plan, review, err := e.runPlanRounds(context.Background(), roundPRD(), original, nil, "")
			if err != nil {
				t.Fatal(err)
			}
			if review.StopReason != envelope.ReviewStopIncomplete {
				t.Errorf("stop %q; want incomplete", review.StopReason)
			}
			for i, want := range tc.answered {
				if got := review.Rounds[i].Findings[0].Disposition; got != want {
					t.Errorf("round %d: %s; want %s", i+1, got, want)
				}
			}
			var ws []cli.Warning
			for _, w := range e.o.Run.Warnings() {
				if w.Code == cli.WarnPlanReviewIncomplete {
					ws = append(ws, w)
				}
			}
			if len(ws) != 1 || ws[0].Severity != "high" || !strings.Contains(ws[0].Message, "widget_cache") ||
				!strings.Contains(ws[0].Message, tc.phase) || !strings.Contains(ws[0].Message, tc.says) {
				t.Errorf("warnings %+v; want one high plan_review_incomplete naming the scope, %s and %q", ws, tc.phase, tc.says)
			}
			want := tc.title
			if want == "" {
				want = original.Tasks.Tasks[0].Title
			}
			if plan.Tasks.Tasks[0].Title != want {
				t.Errorf("the plan returned has task 1 titled %q; want %q", plan.Tasks.Tasks[0].Title, want)
			}
		})
	}
}

// Each plan phase is a progress line naming the scope and the round, and each
// review's findings are counted by severity.
func TestRunPlanRounds_ReportEachPhaseWithItsScopeAndRound(t *testing.T) {
	for _, label := range []string{"scope 2 of 3 (widget_github)", ""} {
		rv := &scriptedReviewer{t: t, planReviews: []reviewOutcome{{severities: []string{"major", "minor"}}, {}}, planRevisions: []revisionOutcome{{}}}
		var stderr lockedWriter
		e := roundEnv(t, rv, 2)
		e.o.Progress = cli.NewProgress(&stderr, "spec", false, false, false)
		if _, _, err := e.runPlanRounds(context.Background(), roundPRD(), *planUnderReview(t), nil, label); err != nil {
			t.Fatal(err)
		}
		prefix := ""
		if label != "" {
			prefix = label + ": "
		}
		out := stderr.w.String()
		for _, want := range []string{
			"[spec] " + prefix + "reviewing the plan (round 1 of 2)",
			"[spec] " + prefix + "revising the plan (round 1 of 2)",
			"[spec] " + prefix + "reviewing the plan (round 2 of 2)",
			"1 major, 1 minor",
		} {
			if !strings.Contains(out, want) {
				t.Errorf("label %q: stderr has no %q:\n%s", label, want, out)
			}
		}
	}
}
