# Erratum: spec 09, adversarial PRD review

Where the implementation of `.specs/09_adversarial_prd_review` differs from
the spec, and why. The decisions are numbered as in the implementation's
survey.

## D-2: the review phases' footer shows cost

**Spec:** Design Decision 24 and TS-09-57 say the footer of a review or
revision phase shows turns and tokens, "not cost", because every other phase
does.

**Code:** every phase's footer is `cli.PhaseSummary`
(`internal/cli/envelope.go`, `PhaseSummary`), and it prints the turns, the
tokens in and out, and the cost (`· $%.2f`). The review phases print the same
footer (`specgen/rounds.go`, `runRounds`). That follows the decision's intent,
the same footer as every other phase, rather than its wrong account of that
footer.

**Test of the delivered behaviour:** `TestTS09_57_TheReviewIsReportedAsPhases`
(`specgen/pipeline_review_test.go`) asserts that the footer equals
`cli.PhaseSummary`'s.

## D-3: a split scope that fails before its write carries no review

**Spec:** 09-REQ-9.1 and TS-09-50 have every package in `follow_on_specs`
carry a review, "including a package reported unwritten".

**Code:** `writeSplit` (`specgen/split_run.go`) reports no package for a scope
that fails before its package is written. That was the case before this spec
and is tested by `TestAScopeThatFailsBeforeItIsWrittenHasNoPackageInTheResult`.
So `follow_on_specs` never holds an unwritten package, and `envelope.Package`
has no `written` field. The one package reported unwritten is an undivided
input's whose generation failed (`unwritten` in `specgen/pipeline.go`), and it
carries its review. Reporting a split's unwritten scopes would change the
split's result beyond this spec.

**Test of the delivered behaviour:**
`TestTS09_50_EveryReportedPackageCarriesItsReview`
(`specgen/split_review_test.go`) asserts that every reported package of a
split carries its review, and that the unwritten package of an undivided
input does.

## D-5: a phase's freshness is asserted on the wire

**Spec:** TS-09-7 and TS-09-20 assert `ph.History == nil`.

**Code:** `agentrun.Phase` has no `History` field. `Runner.Run` builds a new
agent for every phase, so every phase is a fresh conversation by
construction.

**Test of the delivered behaviour:** the two tests assert that the phase's
first request carries one message, as `cmd/spec/e2e_test.go` does for the
generation phases.

## D-6: the landscape is in the review's user prompt

**Spec:** TS-09-7 expects the landscape of existing specs in the review's
system prompt.

**Code:** every phase sends the landscape in its task (`landscapeBlock` in
`specgen/prompts.go`). The system prompt ends with the run context, which is
the same for the whole run and is cached: the language, the steering and the
repository map. The review follows the same layout. Its system prompt carries
the run context (`reviewPhase` in `specgen/phases.go`), and the landscape is
in its user prompt.

**Test of the delivered behaviour:**
`TestTS09_7_TheReviewIsSentEverythingTheAuthorSaw`
(`specgen/review_test.go`).
