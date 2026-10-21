package specgen

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/agent-fox-dev/spec/envelope"
	"github.com/agent-fox-dev/spec/internal/agentrun"
	"github.com/agent-fox-dev/spec/internal/cli"
)

// phaseErr is a phase failure of the given category, as the Runner reports
// one.
func phaseErr(phase, category string) error {
	return &agentrun.Error{Phase: phase, Detail: "stopped: " + category, Cat: category}
}

func reviewWarnings(e *runEnv) []cli.Warning {
	var out []cli.Warning
	for _, w := range e.o.Run.Warnings() {
		if w.Code == cli.WarnPRDReviewIncomplete {
			out = append(out, w)
		}
	}
	return out
}

// TS-09-39 (integration): a review or revision that fails, for any reason
// but an interrupt, stops the rounds with a high prd_review_incomplete
// warning, and the run goes on from the last complete PRD.
//
// Verifies: 09-REQ-7.1
func TestTS09_39_AFailedRoundStopsTheRoundsWithAWarning(t *testing.T) {
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
			rv := &scriptedReviewer{t: t}
			if phase == agentrun.PhasePRDReview {
				rv.reviews = []reviewOutcome{{err: failure}}
			} else {
				rv.reviews = []reviewOutcome{{severities: []string{"major"}}}
				rv.revisions = []revisionOutcome{{err: failure}}
			}
			e := roundEnv(t, rv, 2)
			prd := roundPRD()
			out, review, err := e.runRounds(context.Background(), prd, "", nil)
			if err != nil {
				t.Fatalf("%s in %s: the round loop failed the run: %v", name, phase, err)
			}
			if review.StopReason != envelope.ReviewStopIncomplete || out.Body != prd.Body {
				t.Errorf("%s in %s: stop %q, body %q", name, phase, review.StopReason, out.Body)
			}
			ws := reviewWarnings(e)
			if len(ws) != 1 {
				t.Fatalf("%s in %s: warnings %+v", name, phase, e.o.Run.Warnings())
			}
			w := ws[0]
			for _, want := range []string{prd.SpecName, "round 1", phase, failure.Error()} {
				if !strings.Contains(w.Message, want) {
					t.Errorf("%s in %s: the warning does not name %q: %s", name, phase, want, w.Message)
				}
			}
			if w.Severity != "high" || w.Stage != "prd_review" {
				t.Errorf("%s in %s: warning %+v", name, phase, w)
			}
		}
	}

	// A later round's failure keeps what the earlier rounds revised.
	rv := &scriptedReviewer{t: t,
		reviews:   []reviewOutcome{{severities: []string{"major"}}, {err: phaseErr(agentrun.PhasePRDReview, agentrun.CategoryAPI)}},
		revisions: []revisionOutcome{{}},
	}
	e := roundEnv(t, rv, 3)
	out, review, err := e.runRounds(context.Background(), roundPRD(), "", nil)
	if err != nil || !strings.Contains(out.Body, "revised in round 1") || review.StopReason != envelope.ReviewStopIncomplete ||
		review.RoundsRun != 1 {
		t.Errorf("round 2 failed: err %v, body %q, review %+v", err, out.Body, review)
	}
	if ws := reviewWarnings(e); len(ws) != 1 || !strings.Contains(ws[0].Message, "round 2") {
		t.Errorf("warnings = %+v", ws)
	}
}

// TS-09-40 (integration): the findings of a round whose revision failed are
// unanswered, and the PRD is the one the review read.
//
// Verifies: 09-REQ-7.2
func TestTS09_40_AFailedRevisionLeavesItsFindingsUnanswered(t *testing.T) {
	rv := &scriptedReviewer{t: t, reviews: []reviewOutcome{{severities: []string{"major", "minor"}}},
		revisions: []revisionOutcome{{err: phaseErr(agentrun.PhasePRDRevision, agentrun.CategoryMaxTurns)}}}
	e := roundEnv(t, rv, 2)
	prd := roundPRD()
	out, review, err := e.runRounds(context.Background(), prd, "", nil)
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

// TS-09-41 (integration): a total budget already spent stops the rounds
// before the first review, with a warning naming the budget.
//
// Verifies: 09-REQ-7.3
func TestTS09_41_ASpentTotalBudgetStopsTheRoundsBeforeTheReview(t *testing.T) {
	rv := &scriptedReviewer{t: t}
	e := roundEnv(t, rv, 2)
	e.o.Run.AddPhase(cli.PhaseInfo{Name: "prd", CostUSD: 3.5})
	e.o.TotalBudgetUSD = 3.5
	out, review, err := e.runRounds(context.Background(), roundPRD(), "", nil)
	if err != nil {
		t.Fatal(err)
	}
	if reviews, _ := rv.counts(); reviews != 0 {
		t.Errorf("%d reviews ran", reviews)
	}
	if review.RoundsRun != 0 || review.StopReason != envelope.ReviewStopIncomplete || out.Body != roundPRD().Body {
		t.Errorf("review = %+v", review)
	}
	ws := reviewWarnings(e)
	if len(ws) != 1 {
		t.Fatalf("warnings = %+v", e.o.Run.Warnings())
	}
	for _, want := range []string{"total budget", "$3.50", "round 1", agentrun.PhasePRDReview} {
		if !strings.Contains(ws[0].Message, want) {
			t.Errorf("the warning does not say %q: %s", want, ws[0].Message)
		}
	}
}

// TS-09-42 (integration): a review whose cost reaches the total budget
// leaves its findings unanswered: the revision does not start.
//
// Verifies: 09-REQ-7.4
func TestTS09_42_ABudgetStopBeforeTheRevisionLeavesTheFindingsUnanswered(t *testing.T) {
	rv := &scriptedReviewer{t: t, reviews: []reviewOutcome{{severities: []string{"critical", "minor"}, cost: 2}}}
	e := roundEnv(t, rv, 2)
	e.o.TotalBudgetUSD = 2
	_, review, err := e.runRounds(context.Background(), roundPRD(), "", nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, revisions := rv.counts(); revisions != 0 {
		t.Errorf("%d revisions ran", revisions)
	}
	for _, f := range review.Rounds[0].Findings {
		if f.Disposition != envelope.DispositionUnanswered {
			t.Errorf("finding %+v", f)
		}
	}
	if review.UnansweredCount != review.FindingsCount || review.FindingsCount != 2 {
		t.Errorf("review = %+v", review)
	}
	if ws := reviewWarnings(e); len(ws) != 1 || !strings.Contains(ws[0].Message, agentrun.PhasePRDRevision) ||
		!strings.Contains(ws[0].Message, "total budget") {
		t.Errorf("warnings = %+v", ws)
	}
	// The review's own cost is recorded, which is what reached the budget.
	if got := e.o.Run.CostUSD(); got != 2 {
		t.Errorf("recorded spend = %v", got)
	}
}

// TS-09-43 (integration): an interrupt during a review or a revision stops
// the run as aborted, with no review warning and no PRD to generate from.
//
// Verifies: 09-REQ-7.5
func TestTS09_43_AnInterruptAbortsTheRun(t *testing.T) {
	for _, phase := range []string{agentrun.PhasePRDReview, agentrun.PhasePRDRevision} {
		ctx, cancel := context.WithCancel(context.Background())
		aborted := phaseErr(phase, agentrun.CategoryAborted)
		rv := &scriptedReviewer{t: t}
		if phase == agentrun.PhasePRDReview {
			rv.reviews = []reviewOutcome{{err: aborted}}
			rv.onReview = func(reviewRequest) { cancel() }
		} else {
			rv.reviews = []reviewOutcome{{severities: []string{"major"}}}
			rv.revisions = []revisionOutcome{{err: aborted}}
			rv.onRevise = func(revisionRequest) { cancel() }
		}
		e := roundEnv(t, rv, 2)
		out, review, err := e.runRounds(ctx, roundPRD(), "", nil)
		cancel()
		var f *Failure
		if !errors.As(err, &f) || f.Stage != phase || f.Category != agentrun.CategoryAborted {
			t.Fatalf("%s: err = %v", phase, err)
		}
		if cli.ExitCodeFor(f.Category) != cli.ExitFailed {
			t.Errorf("%s: exit code %d", phase, cli.ExitCodeFor(f.Category))
		}
		if len(reviewWarnings(e)) != 0 {
			t.Errorf("%s: an interrupt warned %+v", phase, reviewWarnings(e))
		}
		if review != nil || out.SpecName != "" {
			t.Errorf("%s: an interrupted round handed on %+v, %+v", phase, out, review)
		}
	}
}

// The spend a review reports is recorded under its phase name, whether it
// finished or not, so the budget check before the next phase counts it.
func TestTS09_43_AFailedPhasesSpendIsRecorded(t *testing.T) {
	rv := &scriptedReviewer{t: t, reviews: []reviewOutcome{{severities: []string{"major"}, cost: 0.25}},
		revisions: []revisionOutcome{{err: phaseErr(agentrun.PhasePRDRevision, agentrun.CategoryBudget), cost: 0.5}}}
	e := roundEnv(t, rv, 1)
	if _, _, err := e.runRounds(context.Background(), roundPRD(), "", nil); err != nil {
		t.Fatal(err)
	}
	if got := fmt.Sprintf("%.2f", e.o.Run.CostUSD()); got != "0.75" {
		t.Errorf("recorded spend = %s", got)
	}
}
