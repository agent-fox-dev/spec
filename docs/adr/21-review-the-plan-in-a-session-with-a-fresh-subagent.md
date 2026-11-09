# 21. Review the plan in a session with a fresh subagent

**Status:** accepted
**Date:** 2026-11-09
**Related:** [ADR 12](12-render-the-skills-for-every-agent-from-one-source.md), [ADR 16](16-triage-every-review-finding-four-ways.md), [ADR 17](17-review-the-plan-adversarially.md)

## Context

`spec --adversarial-plan` (ADR 17) reviews a generated plan before the
package is written. Its strength comes from two things that the in-session
`af-spec` skill lacked:

- **The reviewer starts fresh.** Each phase of `spec` is a new conversation
  that sees only the PRD and the artifacts. The skill writes the PRD, the
  requirements and the tasks in one context, and a gap the author filled in
  their head does not read as a gap to that same context.
- **The reviewer is someone else.** One agent finds flaws against written
  rubrics, and another answers each finding four ways (ADR 16).

A person using the skill had no equivalent short of running `spec`, which
writes a second package beside theirs.

## Decision

- **`af-spec` gains an optional review**, run when the person asks, after a
  clean verdict and before activation. Activation closes the intent, so
  this is the last point at which a change is cheap.
- **The reviewer is a subagent with a fresh context.** It is briefed with the
  rendered package, the PRD, the steering, the repository root and the
  grounding facts (whether each `touches` path exists, computed from the
  file system), and nothing the author decided along the way. It reads and
  does not write.
- **Where the agent cannot start one, the review is skipped and the report
  says so.** A review in the author's own context is the bias this step
  exists to remove, so it does not stand in for one.
- **The rubrics, severities and answers are `spec`'s.** The rubrics are
  `zero_decision`, `grounding` and `steering`, with the same severities. The
  author answers every finding `fixed`, `contested`, `escalated` or
  `declined`, under ADR 16's rules. An escalated finding is put to the person,
  which is the skill's one new stop.
- **Rounds:** another round runs when a round raised a `critical` or `major`
  finding that was fixed, to at most three, or as many as the person asked
  for. After any change the verdict is taken again before activation.
- **The instructions live in `skills/af-spec/references/review.md`.** They
  load only when the review runs, and they keep the skill's body under the
  24 KB ceiling of ADR 12.

## Consequences

- A session can get most of what `--adversarial-plan` gives: a fresh reader
  and a triaged answer to every finding. It still has no budget, no
  machine-checked dispositions and no JSON record of the rounds; those stay
  `spec`'s.
- The review depends on the agent: Claude Code starts subagents, and an
  agent without them gets no review. Its report says so.
- The rubric text exists twice, in `specgen/templates/plan_review_system.md`
  and in `review.md`. A change to one is made to the other in the same
  commit.
- The PRD review (`--adversarial`) has no session form; `af-prd` writes the
  PRD with a person, who is its reviewer.
