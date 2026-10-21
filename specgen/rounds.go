package specgen

import (
	"context"
	"fmt"
	"slices"

	"github.com/agent-fox-dev/spec/envelope"
	"github.com/agent-fox-dev/spec/internal/agentrun"
	"github.com/agent-fox-dev/spec/internal/cli"
	"github.com/agent-fox-dev/spec/internal/project"
)

// runRounds reviews prd adversarially before its package is generated, and
// returns the last complete PRD and the record of how it got there.
//
// A round is one review and, when the review raised anything, one revision
// of its findings. Another round follows only when the round's review raised
// a critical or major finding and the limit has not been reached: a review
// with nothing serious does not justify paying for another. Each round
// reviews the PRD the previous revision left, and every round after the
// first is shown the earlier findings with what became of them.
//
// A review or revision that does not finish, or a --total-budget already
// spent before one starts, stops the rounds with a warning: the PRD returned
// is then the last complete one. An interrupt of the run is not that; it
// fails the run as aborted, at the phase it stopped.
//
// label names the scope in a split and is empty for an undivided input;
// split is the scope's split context, nil for an undivided input.
func (e *runEnv) runRounds(ctx context.Context, prd PRD, label string, split *splitContext) (PRD, *Review, error) {
	o := e.o
	limit := max(1, o.AdversarialRounds)
	var rounds []ReviewRound
	var earlier []Finding
	stop := envelope.ReviewStopIncomplete

	for round := 1; round <= limit; round++ {
		if reason, spent := e.overTotalBudget(); spent {
			e.warnIncomplete(prd, round, agentrun.PhasePRDReview, reason)
			break
		}
		req := e.reviewRequest(prd, split, round, limit, earlier)
		done := o.Progress.Begin("%sreviewing the PRD (round %d of %d)", prefixed(label), round, limit)
		findings, stats, err := e.reviewer.Review(ctx, req)
		recordPhase(o.Run, stats, prd.SpecName)
		done(cli.PhaseSummary(stats))
		if err != nil {
			if ctx.Err() != nil {
				return PRD{}, nil, scopedIf(label, fail(agentrun.PhasePRDReview, agentrun.CategoryAborted, err))
			}
			e.warnIncomplete(prd, round, agentrun.PhasePRDReview, failureReason(err))
			break
		}
		if findings == nil {
			findings = []Finding{}
		}
		rounds = append(rounds, ReviewRound{Round: round, Findings: findings})
		if len(findings) == 0 {
			stop = envelope.ReviewStopNoFindings
			break
		}

		answer(findings, nil)
		if reason, spent := e.overTotalBudget(); spent {
			e.warnIncomplete(prd, round, agentrun.PhasePRDRevision, reason)
			break
		}
		done = o.Progress.Begin("%srevising the PRD (round %d of %d)", prefixed(label), round, limit)
		revised, dispositions, stats, err := e.reviewer.Revise(ctx, revisionRequest{reviewRequest: req, Findings: findings})
		recordPhase(o.Run, stats, prd.SpecName)
		done(cli.PhaseSummary(stats))
		if err != nil {
			if ctx.Err() != nil {
				return PRD{}, nil, scopedIf(label, fail(agentrun.PhasePRDRevision, agentrun.CategoryAborted, err))
			}
			e.warnIncomplete(prd, round, agentrun.PhasePRDRevision, failureReason(err))
			break
		}
		answer(findings, dispositions)
		prd = revised
		earlier = append(earlier, findings...)

		switch {
		case !serious(findings):
			stop = envelope.ReviewStopNoSeriousFindings
		case round == limit:
			stop = envelope.ReviewStopRoundsExhausted
		default:
			continue
		}
		break
	}
	return prd, buildReview(rounds, limit, stop), nil
}

// warnIncomplete records why the rounds of prd stopped before they
// finished. The review only improves a PRD that is already complete, so a
// phase that did not finish costs the run the rest of the review, never the
// package: the run goes on from the last complete PRD, and says so.
func (e *runEnv) warnIncomplete(prd PRD, round int, phase, reason string) {
	e.o.Run.Warn(cli.WarnPRDReviewIncomplete, "high",
		"the review of %s stopped in round %d: %s did not finish (%s); the package is generated from "+
			"the last complete PRD", prd.SpecName, round, phase, reason)
}

// overTotalBudget reports whether the run has spent its --total-budget, and
// the reason to give. A review is never started past the ceiling: the
// rounds stop instead, and the package is still generated.
func (e *runEnv) overTotalBudget() (string, bool) {
	o := e.o
	spent := o.Run.CostUSD()
	if o.TotalBudgetUSD <= 0 || spent < o.TotalBudgetUSD {
		return "", false
	}
	return fmt.Sprintf("the run has spent $%.2f of its $%.2f total budget", spent, o.TotalBudgetUSD), true
}

// failureReason is why a phase stopped, for the warning: its category and
// what it said.
func failureReason(err error) string {
	return fmt.Sprintf("%s: %v", agentrun.CategoryOf(err), err)
}

// reviewRequest is what a review of prd in round round is sent: what the PRD
// phase was shown, and the PRD as it stands.
func (e *runEnv) reviewRequest(prd PRD, split *splitContext, round, limit int, earlier []Finding) reviewRequest {
	o := e.o
	return reviewRequest{
		Root:         e.root,
		SourceKind:   o.Input.Kind.String(),
		SourceOrigin: o.Input.Origin,
		Input:        o.Input.Body,
		Context:      o.Input.Context,
		Profile:      e.profile,
		Landscape:    e.landscapeNow(),
		SpecRoot:     relativeTo(e.root, e.specsDir),
		Steering:     project.Steering(e.specsDir),
		RepoMap:      e.repoMap,
		PRD:          prd,
		Split:        split,
		Round:        round,
		Limit:        limit,
		Earlier:      slices.Clone(earlier),
	}
}

// answer writes each finding's disposition, and its reason when rejected.
// A finding with no disposition to take is unanswered: its round's revision
// did not finish.
func answer(findings []Finding, dispositions []Disposition) {
	for i := range findings {
		f := &findings[i]
		f.Disposition, f.Reason = envelope.DispositionUnanswered, ""
		for _, d := range dispositions {
			if d.ID != f.ID {
				continue
			}
			f.Disposition = d.Disposition
			if d.Disposition == envelope.DispositionRejected {
				f.Reason = d.Reason
			}
		}
	}
}

// serious reports whether any finding is critical or major.
func serious(findings []Finding) bool {
	return slices.ContainsFunc(findings, func(f Finding) bool {
		return f.Severity == envelope.SeverityCritical || f.Severity == envelope.SeverityMajor
	})
}

// buildReview is the record of a PRD's rounds. Every count is derived from
// the rounds, so the totals cannot disagree with them.
func buildReview(rounds []ReviewRound, limit int, stop string) *Review {
	r := &Review{RoundsRun: len(rounds), RoundsAllowed: limit, StopReason: stop, Rounds: rounds}
	for _, round := range rounds {
		for _, f := range round.Findings {
			r.FindingsCount++
			switch f.Disposition {
			case envelope.DispositionAccepted:
				r.AcceptedCount++
			case envelope.DispositionRejected:
				r.RejectedCount++
			default:
				r.UnansweredCount++
			}
		}
	}
	return r
}
