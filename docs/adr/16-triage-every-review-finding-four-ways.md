# 16. Triage every review finding four ways and converge on two clean rounds

**Status:** accepted
**Date:** 2026-11-06
**Replaces:** the dispositions, the stopping rule and the record of [ADR 09](09-review-the-prd-adversarially.md)
**Related:** [ADR 15](15-version-the-steering.md)

## Context

The adversarial PRD review of ADR 09 answered a finding two ways, `accepted`
or `rejected` with a reason. A finding the reviser could refute, one it would
not act on, and one that needed a trade-off a person should settle were all
"rejected with a reason". And one review that raised nothing serious stopped
the rounds, so a single sample decided that a PRD had no serious flaw left.
The reviewer was handed the steering, but nothing asked it to check the PRD
against it. `docs/spec-kit-comparison.md` §1.4, §4.9, §5 and §7.1 item 1
recommended EGM's triage and convergence rules.

## Decision

- **Four dispositions, each with what makes it checkable.** `fixed` changes
  the PRD. `contested` shows counter-evidence the run finds — a quote that
  appears in the PRD under review or the input, whitespace aside, or a path
  that resolves inside the repository and exists — with a reason; a quote that
  is not there, or a path that is not, is turned back. `escalated` carries a
  question, two or three options, the decision taken and why it is uncertain,
  and the decision must appear in the revised body, under `## Design
  Decisions` opening with "Provisional:", so the package stays complete.
  `declined` gives a reason: out of scope, or the later package that carries
  it. The run sets `unanswered` on a finding whose revision did not finish.
  The reviser's contract has no field for a finding's severity or rubric.
- **An escalation is decided provisionally and handed on.** `spec` runs
  unattended, so nobody can answer it during the run. Once the rounds stop,
  the run adds every escalation to the package's open questions, in round
  order, skipping one already there — after the rounds, so a later revision
  that replaces the open questions cannot drop an earlier round's.
- **What a round settled stays settled.** A contested finding may be raised
  again only through `reopens`, with evidence the contest did not answer; a
  declined or escalated one may not be raised again; only a contested finding
  can be reopened. A flaw a fixed finding named that the PRD still has is a
  new finding.
- **Two consecutive clean rounds converge.** A round is clean when its review
  raised nothing critical or major. Its minor findings are still revised; a
  review with nothing has no revision. The rounds stop as `converged` on two
  consecutive clean rounds, `rounds_exhausted` at the limit, or `incomplete`,
  which wins over both. The default stays one round, which never converges:
  raising it would raise the cost of every `--adversarial` run, and the result
  says honestly that one review is one sample. The documentation recommends
  `--adversarial-rounds 3`.
- **The steering is a rubric that cannot be declined.** Every finding carries
  a rubric, `prd` or `steering`. A steering finding quotes the directive it
  contradicts, verbatim in the steering body (ADR 15), and is critical or
  major. It is fixed, contested (the directive does not apply here) or
  escalated (the input asks for what the directive forbids, so a person
  changes the input or amends the steering); it cannot be declined. With no
  steering, nothing checks alignment.
- **The record breaks rather than carrying both vocabularies.**
  `envelope.Review` replaces `accepted`, `rejected`, `accepted_count`,
  `rejected_count`, `no_findings` and `no_serious_findings` with the four
  dispositions, `rubric` and the answer's fields per finding, `clean` per
  round, the six totals `findings_count`, `fixed_count`, `contested_count`,
  `escalated_count`, `declined_count` and `unanswered_count`, and the three
  stop reasons. `accepted` maps onto `fixed`, but `rejected` splits into
  `contested` and `declined`, and nothing could fill either from the old
  value. The envelope had no tagged release that carried the review and no
  external consumer of it, so this is the cheapest time to change it.

## Consequences

- A program that read `accepted_count`, `rejected_count`, `accepted`,
  `rejected`, `no_findings` or `no_serious_findings` from `result.review` must
  read the new fields. This is a breaking change to the envelope.
- The summary clause counts the steering findings apart and the fixed ones:
  `review: 2 rounds, 9 findings (1 steering), 7 fixed`.
- A run with the default single round bills the phases it always billed and
  reports `rounds_exhausted` or `incomplete`; convergence costs at least a
  second review.
- The vocabulary of spec 09's glossary (`disposition`, `finding`,
  `stop_reason`) is replaced here and not in that package, which keeps one
  definition per term across packages; this ADR is where a reader of spec 09
  is sent.
