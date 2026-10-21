package specgen

import (
	"context"
	"errors"
	"fmt"
	"math/rand"
	"strings"
	"sync"
	"testing"

	"github.com/agent-fox-dev/spec/envelope"
	"github.com/agent-fox-dev/spec/internal/agentrun"
)

// reviewOutcome is what one scripted review does: fail, or raise findings of
// these severities (none for an empty review).
type reviewOutcome struct {
	severities []string
	err        error
	// cost is what the phase reports spending.
	cost float64
}

// revisionOutcome is what one scripted revision does: fail, or accept every
// finding but the ones listed in reject.
type revisionOutcome struct {
	reject []int
	err    error
	cost   float64
}

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
	res := agentrun.Result{Name: agentrun.PhasePRDReview, Turns: 2}
	res.Usage.CostUSD = out.cost
	if out.err != nil {
		return nil, res, out.err
	}
	findings := make([]Finding, len(out.severities))
	for n, sev := range out.severities {
		findings[n] = Finding{ID: fmt.Sprintf("R%d-F%d", req.Round, n+1), Severity: sev,
			Section: "Goals", Flaw: fmt.Sprintf("flaw %d.%d", req.Round, n+1), Evidence: "e", Remedy: "r"}
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
	res := agentrun.Result{Name: agentrun.PhasePRDRevision, Turns: 3}
	res.Usage.CostUSD = out.cost
	if out.err != nil {
		return PRD{}, nil, res, out.err
	}
	var ds []Disposition
	for n, f := range req.Findings {
		d := Disposition{ID: f.ID, Disposition: envelope.DispositionAccepted}
		for _, r := range out.reject {
			if r == n {
				d = Disposition{ID: f.ID, Disposition: envelope.DispositionRejected, Reason: "REASON-" + f.ID}
			}
		}
		ds = append(ds, d)
	}
	revised := req.PRD
	revised.Body = fmt.Sprintf("%s\nrevised in round %d\n", req.PRD.Body, req.Round)
	return revised, ds, res, nil
}

func (s *scriptedReviewer) counts() (reviews, revisions int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.reviewReqs), len(s.reviseReqs)
}

// roundEnv is a run environment for the round loop alone.
func roundEnv(t *testing.T, rv reviewer, limit int) *runEnv {
	t.Helper()
	ws := newWorkspace(t)
	o := newOptions(ws, newAuthor(t, "01", "test_feature"))
	o.Adversarial, o.AdversarialRounds, o.reviewer = true, limit, rv
	return &runEnv{o: o, root: ws.Root(), specsDir: resolveSpecsDir(o, ws.Root()), author: o.author, reviewer: rv}
}

func roundPRD() PRD {
	return PRD{SpecName: "widget_cache", Title: "Widget cache", Body: "## Intent\n\nCache widgets.\n"}
}

// TS-09-33 (integration): a review with no findings ends the rounds and
// passes the PRD on unchanged.
//
// Verifies: 09-REQ-6.1
func TestTS09_33_AReviewWithNoFindingsEndsTheRounds(t *testing.T) {
	rv := &scriptedReviewer{t: t, reviews: []reviewOutcome{{}}}
	e := roundEnv(t, rv, 3)
	prd := roundPRD()
	out, review, err := e.runRounds(context.Background(), prd, "", nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, revisions := rv.counts(); revisions != 0 || out.Body != prd.Body {
		t.Errorf("revisions %d, body %q", revisions, out.Body)
	}
	if review.RoundsRun != 1 || len(review.Rounds) != 1 || len(review.Rounds[0].Findings) != 0 ||
		review.Rounds[0].Round != 1 || review.StopReason != envelope.ReviewStopNoFindings || review.RoundsAllowed != 3 {
		t.Errorf("review = %+v", review)
	}
}

// TS-09-34 (integration): findings are revised, and a major one starts a
// round that reviews the PRD the revision left.
//
// Verifies: 09-REQ-6.2, 09-REQ-6.3
func TestTS09_34_AMajorFindingStartsARoundOnTheRevisedPRD(t *testing.T) {
	rv := &scriptedReviewer{t: t,
		reviews:   []reviewOutcome{{severities: []string{"major", "minor"}}, {}},
		revisions: []revisionOutcome{{}},
	}
	e := roundEnv(t, rv, 2)
	out, review, err := e.runRounds(context.Background(), roundPRD(), "", nil)
	if err != nil {
		t.Fatal(err)
	}
	if reviews, revisions := rv.counts(); reviews != 2 || revisions != 1 {
		t.Fatalf("reviews %d, revisions %d", reviews, revisions)
	}
	if len(rv.reviseReqs[0].Findings) != 2 || rv.reviseReqs[0].Findings[0].ID != "R1-F1" {
		t.Errorf("the revision was sent %+v", rv.reviseReqs[0].Findings)
	}
	if !strings.Contains(rv.reviewReqs[1].PRD.Body, "revised in round 1") || out.Body != rv.reviewReqs[1].PRD.Body {
		t.Errorf("round 2 reviewed %q; the result is %q", rv.reviewReqs[1].PRD.Body, out.Body)
	}
	if review.RoundsRun != 2 || review.StopReason != envelope.ReviewStopNoFindings {
		t.Errorf("review = %+v", review)
	}
	for _, f := range review.Rounds[0].Findings {
		if f.Disposition != envelope.DispositionAccepted {
			t.Errorf("finding %s is %q", f.ID, f.Disposition)
		}
	}
}

// TS-09-35 (integration): a round whose findings are all minor is revised and
// is the last.
//
// Verifies: 09-REQ-6.4
func TestTS09_35_OnlyMinorFindingsEndTheRoundsAfterTheirRevision(t *testing.T) {
	rv := &scriptedReviewer{t: t, reviews: []reviewOutcome{{severities: []string{"minor", "minor"}}},
		revisions: []revisionOutcome{{}}}
	e := roundEnv(t, rv, 5)
	_, review, err := e.runRounds(context.Background(), roundPRD(), "", nil)
	if err != nil {
		t.Fatal(err)
	}
	if reviews, revisions := rv.counts(); reviews != 1 || revisions != 1 {
		t.Errorf("reviews %d, revisions %d", reviews, revisions)
	}
	if review.StopReason != envelope.ReviewStopNoSeriousFindings || review.RoundsRun != 1 {
		t.Errorf("review = %+v", review)
	}
}

// TS-09-36 (integration): a critical finding in the last allowed round ends
// the rounds after its revision.
//
// Verifies: 09-REQ-6.5
func TestTS09_36_ACriticalFindingAtTheLimitExhaustsTheRounds(t *testing.T) {
	rv := &scriptedReviewer{t: t,
		reviews:   []reviewOutcome{{severities: []string{"critical"}}, {severities: []string{"critical"}}},
		revisions: []revisionOutcome{{}, {}},
	}
	e := roundEnv(t, rv, 2)
	_, review, err := e.runRounds(context.Background(), roundPRD(), "", nil)
	if err != nil {
		t.Fatal(err)
	}
	if reviews, revisions := rv.counts(); reviews != 2 || revisions != 2 {
		t.Errorf("reviews %d, revisions %d", reviews, revisions)
	}
	if review.StopReason != envelope.ReviewStopRoundsExhausted || review.RoundsRun != 2 {
		t.Errorf("review = %+v", review)
	}
}

// TS-09-37 (integration): a later round's reviewer and reviser are both sent
// every earlier finding with its id, disposition and reason, and the
// reviewer the rule on rejected findings; round 1 has no earlier rounds.
//
// Verifies: 09-REQ-6.6
func TestTS09_37_LaterRoundsAreSentTheEarlierFindings(t *testing.T) {
	rv := &scriptedReviewer{t: t,
		reviews:   []reviewOutcome{{severities: []string{"major", "major"}}, {severities: []string{"minor"}}},
		revisions: []revisionOutcome{{reject: []int{1}}, {}},
	}
	e := roundEnv(t, rv, 2)
	if _, _, err := e.runRounds(context.Background(), roundPRD(), "", nil); err != nil {
		t.Fatal(err)
	}
	if len(rv.reviewReqs) != 2 || len(rv.reviseReqs) != 2 {
		t.Fatalf("reviews %d, revisions %d", len(rv.reviewReqs), len(rv.reviseReqs))
	}
	earlier := rv.reviewReqs[1].Earlier
	if len(earlier) != 2 || earlier[0].Disposition != envelope.DispositionAccepted ||
		earlier[1].Disposition != envelope.DispositionRejected || earlier[1].Reason != "REASON-R1-F2" {
		t.Errorf("round 2 review was sent %+v", earlier)
	}
	for name, prompt := range map[string]string{
		"review":   prdReviewUserPrompt(rv.reviewReqs[1]),
		"revision": prdRevisionUserPrompt(rv.reviseReqs[1]),
	} {
		for _, want := range []string{"R1-F1", "R1-F2", "accepted", "rejected", "REASON-R1-F2"} {
			if !strings.Contains(prompt, want) {
				t.Errorf("the round 2 %s prompt has no %q", name, want)
			}
		}
	}
	if p := prdReviewUserPrompt(rv.reviewReqs[1]); !strings.Contains(p, "only with evidence the rejection did not answer") ||
		!strings.Contains(p, "`reopens`") {
		t.Error("the round 2 review is not told the rule on rejected findings")
	}
	if len(rv.reviewReqs[0].Earlier) != 0 || strings.Contains(prdReviewUserPrompt(rv.reviewReqs[0]), "Earlier rounds") ||
		strings.Contains(prdRevisionUserPrompt(rv.reviseReqs[0]), "Earlier rounds") {
		t.Error("round 1 was sent earlier rounds")
	}
}

// outcomeSequence is a random script for up to limit rounds.
func outcomeSequence(rng *rand.Rand, limit int) ([]reviewOutcome, []revisionOutcome) {
	sevs := []string{"critical", "major", "minor"}
	var reviews []reviewOutcome
	var revisions []revisionOutcome
	for range limit {
		r := reviewOutcome{}
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
		v := revisionOutcome{}
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

// TS-09-38 (property): for any limit and any outcomes the loop runs at most
// the limit of reviews and ends with exactly one stop reason, incomplete
// whenever a phase did not finish.
//
// Verifies: 09-REQ-6.7
func TestTS09_38_TheLoopIsBoundedAndHasOneStopReason(t *testing.T) {
	rng := rand.New(rand.NewSource(38))
	reasons := []string{envelope.ReviewStopNoFindings, envelope.ReviewStopNoSeriousFindings,
		envelope.ReviewStopRoundsExhausted, envelope.ReviewStopIncomplete}
	for range 500 {
		limit := 1 + rng.Intn(5)
		reviews, revisions := outcomeSequence(rng, limit)
		rv := &scriptedReviewer{t: t, reviews: reviews, revisions: revisions}
		e := roundEnv(t, rv, limit)
		_, review, err := e.runRounds(context.Background(), roundPRD(), "", nil)
		if err != nil {
			t.Fatal(err)
		}
		nReviews, nRevisions := rv.counts()
		if nReviews > limit || review.RoundsRun > limit {
			t.Fatalf("limit %d: %d reviews", limit, nReviews)
		}
		n := 0
		for _, r := range reasons {
			if review.StopReason == r {
				n++
			}
		}
		if n != 1 {
			t.Fatalf("stop reason %q", review.StopReason)
		}

		failed := false
		for i := range nReviews {
			if reviews[i].err != nil {
				failed = true
			}
		}
		for i := range nRevisions {
			if revisions[i].err != nil {
				failed = true
			}
		}
		if failed != (review.StopReason == envelope.ReviewStopIncomplete) {
			t.Fatalf("a phase failed %v, stop reason %q", failed, review.StopReason)
		}
		if failed {
			continue
		}
		last := reviews[nReviews-1]
		serious := false
		for _, s := range last.severities {
			serious = serious || s != "minor"
		}
		var want string
		switch {
		case len(last.severities) == 0:
			want = envelope.ReviewStopNoFindings
		case !serious:
			want = envelope.ReviewStopNoSeriousFindings
		default:
			want = envelope.ReviewStopRoundsExhausted
		}
		if review.StopReason != want {
			t.Fatalf("limit %d, last review %v: stop %q, want %q", limit, last.severities, review.StopReason, want)
		}
	}
}

// TS-09-52 (property): a review's totals agree with its rounds whatever the
// findings and their dispositions.
//
// Verifies: 09-REQ-9.3
func TestTS09_52_ReviewTotalsAgreeWithTheRounds(t *testing.T) {
	rng := rand.New(rand.NewSource(52))
	dispositions := []string{envelope.DispositionAccepted, envelope.DispositionRejected, envelope.DispositionUnanswered}
	for range 1000 {
		var rounds []ReviewRound
		total := 0
		for r := range rng.Intn(6) {
			round := ReviewRound{Round: r + 1, Findings: []Finding{}}
			for n := range rng.Intn(5) {
				round.Findings = append(round.Findings, Finding{ID: fmt.Sprintf("R%d-F%d", r+1, n+1),
					Disposition: dispositions[rng.Intn(3)]})
			}
			total += len(round.Findings)
			rounds = append(rounds, round)
		}
		review := buildReview(rounds, 5, envelope.ReviewStopIncomplete)
		if review.FindingsCount != total ||
			review.FindingsCount != review.AcceptedCount+review.RejectedCount+review.UnansweredCount ||
			review.RoundsRun != len(review.Rounds) || review.RoundsRun != len(rounds) {
			t.Fatalf("review = %+v for %d findings in %d rounds", review, total, len(rounds))
		}
	}
}
