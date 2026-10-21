package specgen

import (
	"context"
	"slices"

	"github.com/agent-fox-dev/spec/envelope"
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
// label names the scope in a split and is empty for an undivided input;
// split is the scope's split context, nil for an undivided input.
func (e *runEnv) runRounds(ctx context.Context, prd PRD, label string, split *splitContext) (PRD, *Review, error) {
	limit := max(1, e.o.AdversarialRounds)
	var rounds []ReviewRound
	var earlier []Finding
	stop := envelope.ReviewStopIncomplete

	for round := 1; round <= limit; round++ {
		req := e.reviewRequest(prd, split, round, limit, earlier)
		findings, _, err := e.reviewer.Review(ctx, req)
		if err != nil {
			stop = e.roundIncomplete()
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

		revised, dispositions, _, err := e.reviewer.Revise(ctx, revisionRequest{reviewRequest: req, Findings: findings})
		if err != nil {
			answer(findings, nil)
			stop = e.roundIncomplete()
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

// roundIncomplete is where a review or revision that did not finish stops
// the rounds: the PRD the round loop holds is the last complete one.
func (e *runEnv) roundIncomplete() string { return envelope.ReviewStopIncomplete }

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
