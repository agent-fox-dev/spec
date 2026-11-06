package specgen

import (
	"context"
	"fmt"
	"slices"
	"strings"

	"github.com/agent-fox-dev/spec/envelope"
	"github.com/agent-fox-dev/spec/internal/agentrun"
	"github.com/agent-fox-dev/spec/internal/cli"
)

// runRounds reviews prd adversarially before its package is generated, and
// returns the last complete PRD and the record of how it got there.
//
// A round is one review and, when the review raised anything, one revision
// of its findings: the minor findings of a clean round are revised too, and a
// review with nothing has nothing to answer. A round is clean when its review
// raised nothing critical or major. The rounds stop as converged once two
// consecutive rounds were clean — one clean review is a single sample — and
// as exhausted at the limit, so a limit of one never converges (ADR 16). Each
// round reviews the PRD the previous revision left, and every round after the
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
	cleanBefore := false

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
		clean := !serious(findings)
		rounds = append(rounds, ReviewRound{Round: round, Clean: clean, Findings: findings})
		state := "not clean"
		if clean {
			state = "clean"
		}
		o.Progress.Step(agentrun.PhasePRDReview, "%sthe review raised %s; the round is %s", prefixed(label), bySeverity(findings), state)

		if len(findings) > 0 {
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
			o.Progress.Step(agentrun.PhasePRDRevision, "%sthe revision answered: %s", prefixed(label), byDisposition(findings))
			prd = revised
			earlier = append(earlier, findings...)
		}

		if clean && cleanBefore {
			stop = envelope.ReviewStopConverged
			break
		}
		cleanBefore = clean
		if round == limit {
			stop = envelope.ReviewStopRoundsExhausted
		}
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
		Root:            e.root,
		SourceKind:      o.Input.Kind.String(),
		SourceOrigin:    o.Input.Origin,
		Input:           o.Input.Body,
		Context:         o.Input.Context,
		Profile:         e.profile,
		Landscape:       e.landscapeNow(),
		SpecRoot:        relativeTo(e.root, e.specsDir),
		Steering:        e.steering.Body,
		SteeringVersion: e.steering.Version,
		RepoMap:         e.repoMap,
		PRD:             prd,
		Split:           split,
		Round:           round,
		Limit:           limit,
		Earlier:         slices.Clone(earlier),
	}
}

// answer writes each finding's disposition and the fields that disposition
// carries, and clears the others. A finding with no disposition to take is
// unanswered: its round's revision did not finish. Its severity and rubric
// are the reviewer's, and no answer touches them.
func answer(findings []Finding, answers []Disposition) {
	for i := range findings {
		f := &findings[i]
		f.Disposition, f.Reason, f.CounterEvidence, f.Question, f.Options, f.Decision =
			envelope.DispositionUnanswered, "", "", "", nil, ""
		for _, d := range answers {
			if d.ID != f.ID {
				continue
			}
			d.keep()
			f.Disposition, f.Reason, f.CounterEvidence = d.Disposition, d.Reason, d.CounterEvidence
			f.Question, f.Options, f.Decision = d.Question, slices.Clone(d.Options), d.Decision
		}
	}
}

// withEscalations is questions with every escalation of review added, as an
// open question carrying its decision and why it is uncertain: in round
// order, then in the order the reviser answered, skipping a question already
// there, whitespace aside. It runs once the rounds have stopped, so a later
// round's revision that replaced the open questions cannot drop an earlier
// round's escalation.
func withEscalations(questions []OpenQuestion, review *Review) []OpenQuestion {
	if review == nil {
		return questions
	}
	out := slices.Clone(questions)
	for _, round := range review.Rounds {
		for _, f := range round.Findings {
			if f.Disposition != envelope.DispositionEscalated {
				continue
			}
			if slices.ContainsFunc(out, func(q OpenQuestion) bool { return sameEvidence(q.Question, f.Question) }) {
				continue
			}
			out = append(out, OpenQuestion{Question: f.Question, Decision: f.Decision, Why: f.Reason})
		}
	}
	return out
}

// bySeverity is a review's findings counted by severity, most serious
// first: "1 critical, 2 minor", or "no findings".
func bySeverity(findings []Finding) string {
	if len(findings) == 0 {
		return "no findings"
	}
	var parts []string
	for _, sev := range severities {
		n := 0
		for _, f := range findings {
			if f.Severity == sev {
				n++
			}
		}
		if n > 0 {
			parts = append(parts, fmt.Sprintf("%d %s", n, sev))
		}
	}
	return strings.Join(parts, ", ")
}

// byDisposition is a revision's answers counted, in the order of the four:
// "2 fixed, 1 contested".
func byDisposition(findings []Finding) string {
	var parts []string
	for _, d := range dispositions {
		n := 0
		for _, f := range findings {
			if f.Disposition == d {
				n++
			}
		}
		if n > 0 {
			parts = append(parts, fmt.Sprintf("%d %s", n, d))
		}
	}
	if len(parts) == 0 {
		return "nothing"
	}
	return strings.Join(parts, ", ")
}

// serious reports whether any finding is critical or major.
func serious(findings []Finding) bool {
	return slices.ContainsFunc(findings, func(f Finding) bool {
		return f.Severity == envelope.SeverityCritical || f.Severity == envelope.SeverityMajor
	})
}

// buildReview is the record of a PRD's rounds. Whether a round was clean and
// every count are derived from the rounds, so neither can disagree with them.
func buildReview(rounds []ReviewRound, limit int, stop string) *Review {
	r := &Review{RoundsRun: len(rounds), RoundsAllowed: limit, StopReason: stop, Rounds: rounds}
	for i := range rounds {
		rounds[i].Clean = !serious(rounds[i].Findings)
		for _, f := range rounds[i].Findings {
			r.FindingsCount++
			switch f.Disposition {
			case envelope.DispositionFixed:
				r.FixedCount++
			case envelope.DispositionContested:
				r.ContestedCount++
			case envelope.DispositionEscalated:
				r.EscalatedCount++
			case envelope.DispositionDeclined:
				r.DeclinedCount++
			default:
				r.UnansweredCount++
			}
		}
	}
	return r
}
