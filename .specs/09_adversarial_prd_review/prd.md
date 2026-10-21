---
spec_id: "09"
spec_name: "adversarial_prd_review"
title: "Review the PRD adversarially before generating the package"
status: "active"
created_at: "2026-10-21T13:59:29.966011Z"
updated_at: "2026-10-21T13:59:29.966011Z"
intent_hash: "5ed88d651afae618b44d3ffc0310d5b3a618e56b8d827c0e410bc1d66ba2a0a2"
schema_version: 2
source: "docs/prd/01-review-the-prd-adversarially.md"
---
## Intent

A PRD that `spec` writes in one unattended pass has been checked only by the agent that wrote it, so that agent's blind spots carry into every artifact generated from it. This spec adds an opt-in adversarial review. With `--adversarial`, a fresh agent that did not write the PRD looks for its flaws. Another fresh agent then revises the PRD to answer each flaw. Only after that are the requirements, tests and tasks generated, so the package is built on a PRD that has stood up to a critic.

## Goals

- With `--adversarial`, every PRD the run writes is reviewed at least once before its requirements phase starts, and the package is generated from the last complete PRD. This covers the PRD of an undivided input and the PRD of each split scope written in this run.
- `--adversarial-rounds N` (1 to 5, default 1) sets the most review-and-revision rounds a PRD gets. The rounds stop after the first review that raises no `critical` or `major` finding.
- Without `--adversarial`, a run has the same phases, prompts, result, summary sentence, files on disk and per-phase usage as before this feature.
- Each package in the result carries a `review`. It lists every finding per round with what became of it: `accepted`, `rejected` with the reviser's reason, or `unanswered`. The summary sentence adds a clause that counts them.
- A review or revision that cannot finish never fails the run and never changes its exit code. The package is generated from the last complete PRD, and a `high` `prd_review_incomplete` warning names the scope, the round, the failed phase and the reason.
- `--preflight` counts the review phases in `estimate.phases` and lists their models in the `phase_models` check. `--phase-model` accepts `prd_review` and `prd_revision`.

## Non-goals

- Adversarial review of `requirements.json`, `test_spec.json`, `tasks.json` or `architecture.md`. Only the PRD is reviewed.
- Asking a person anything. The run stays unattended.
- More than one reviewer per round, or reviewers debating each other.
- A command or mode that reviews a PRD without generating a package.
- Reviewing a PRD whose package is already on disk. That includes the scopes a resumed split wrote in an earlier run.
- Writing the review into the package on disk. The package keeps exactly the files the format defines.
- Re-planning the split. A reviewer may say the PRD is more than one spec's worth of work, but the revision does not change the split, the spec name or the title.
- Changing what `--comment` posts beyond posting the final PRD. The comment gains no review section.
- Adding review counts to `result.split[]` entries, or showing the follow-on packages' reviews under `--detail summary`. Those appear under `--detail full`.
- Any new environment variable or configuration key.

## Background

A run today is one PRD phase (`specgen/prd.go`, `agentAuthor.WritePRD` in `specgen/phases.go`), then three generation phases for `requirements.json`, `test_spec.json` and `tasks.json`, plus an optional architecture phase (`runEnv.buildPackage` in `specgen/pipeline.go`). Nobody is waiting to answer questions, so the PRD phase settles every open question itself. It records each answer in `## Design Decisions` and reports the ones it is least sure of as `result.open_questions`. It also hands on `codebase_notes` (bounded to 12000 bytes by `boundNotes`) to every later phase (ADR 04).

Validation checks the structure of the generated artifacts: every criterion is tested, every test is owned by a task, and the test commands match the project's language. The substance of the PRD is never checked. A PRD can miss an error path, misread the code, propose a component that already exists, or drop something the input asked for. Its package still validates, but it specifies the wrong thing, and three generation phases are built on the flaw without questioning it.

The model per phase is chosen by tier and by `--phase-model` (ADR 02, `internal/agentrun/models.go`: `Phases`, `tierTable`, `ResolvePlan`). A phase kind that a tier does not list runs on the tier's base entry, which is exactly the PRD phase's model and effort. The scopes of a split are pipelined (ADR 03, `specgen/split_run.go`: `writeSplit`). PRD phases run in order in the coordinator. Each scope's package is generated on its own goroutine as soon as its PRD exists, and the next scope's PRD phase is shown the earlier scopes' PRDs through `splitContext.Drafts` and `Notes`.

`--total-budget` is checked today only before each scope's PRD phase after the first. For an undivided input the shell folds it into the per-phase ceiling (`App.SinglePhase`). A failed optional phase is already handled as a warning rather than a failed run: `architecture_not_written` in `buildPackage`. Phase tools are strict-mode schemas, the API limits union parameters across a request's strict tools (ADR 04, ADR 07), and each optional property costs one.

All existing specs under `.specs/` are archived, so this spec has no active spec dependencies. It extends the decisions of ADR 02, ADR 03 and ADR 04.

## Requirements

### Flags and validation

`spec` gains two flags of its own:

- `--adversarial`: a boolean, off by default.
- `--adversarial-rounds N`: an integer, 1 to 5. When it is not given, the round limit is 1.

The following are usage errors (exit 2), raised before anything is fetched or any model is called, with or without `--preflight`:

- `--adversarial-rounds` given with a value outside 1 to 5, including an explicit `0`.
- `--adversarial-rounds` given while `--adversarial` is off.
- A non-integer value. The flag parser already rejects it.

`--dry-run` runs the review and revision phases like every other phase and writes nothing. A dry run's result reports the review exactly as a real run would.

### The review phase (`prd_review`)

The review is a fresh conversation. It is handed:

- the PRD as it stands: spec name, title, body, open questions and relevant files.
- the original input and any `--context`.
- the current `codebase_notes`, explicitly labelled as the author's claims to be checked against the code, not as facts.
- the run context every phase has: the language block, the steering, the repository map, the landscape of existing specs, and the read-only file tools (plus `code_search` when the index exists).

When the PRD is one scope of a split, the review is also told the split: every scope's name and one-line scope, which scope this PRD covers, and what the earlier scopes deliver as the PRD phase was shown it. It is told that what belongs to another scope is not a dropped ask.

It is instructed to find flaws, not to approve, and to look for:

- behaviours without an error path
- goals that cannot be verified
- claims about the code, in the PRD or the notes, that the code contradicts
- a proposed component that already exists under another name
- scope the input did not ask for, and asks of the input that the PRD dropped
- wording that two readers would turn into different requirements
- sections that contradict each other
- Design Decisions whose reason does not hold
- internal design (package layout, types, signatures) that leaked into the PRD

The review ends by submitting a list of findings, which may be empty. Each finding has:

- a severity: `critical` (the package would specify the wrong thing), `major` (a requirement would be missing or untestable) or `minor` (wording, clarity).
- the PRD section it concerns, as free text.
- the flaw, in one or two sentences.
- the evidence: a quote from the PRD or the input, or a file path and what it shows.
- a suggested remedy.
- `reopens`: the id of an earlier finding it raises again, or empty.

The submission is turned back to the reviewer, naming the offending findings, when:

- it has more than 30 findings.
- any finding has an empty severity, section, flaw, evidence or remedy. A finding without evidence must be supported or withdrawn.
- in round 1, any finding has a non-empty `reopens`.
- in a later round, a `reopens` names no earlier finding of this PRD.
- in a later round, a finding reopens an earlier `rejected` finding with evidence identical to that finding's evidence, compared after whitespace normalisation.
- in a later round, a finding has an empty `reopens` and the same section and flaw text as an earlier `rejected` finding.

The run assigns each accepted finding a stable id `R<round>-F<n>` (for example `R2-F3`) in the order submitted.

A review with no findings ends the rounds, and the PRD goes on unchanged.

### The revision phase (`prd_revision`)

The revision is another fresh conversation, separate from both the PRD phase and the review. It is handed:

- the PRD as it stands.
- the findings with their ids.
- the original input and `--context`.
- the current `codebase_notes`.
- the same split context, run context and read-only tools as the review.

It must answer every finding with a disposition:

- `accepted`: the PRD is changed to remedy it.
- `rejected`: with a reason that says why the finding does not hold, not merely that the author disagrees.

It is instructed to record in `## Design Decisions` what an accepted remedy settled.

The revision submits:

- the complete revised body (required).
- the dispositions (required).
- optionally, replacement open questions, relevant files and `codebase_notes`.

For each optional field, an omitted field keeps the earlier value and a supplied field replaces it. An empty list clears open questions or relevant files. An empty `codebase_notes` keeps the earlier notes. Replacement notes are bounded to 12000 bytes exactly as the PRD phase's are.

The revision is not asked for the spec name, the title or the split. The revised PRD carries over those of the PRD it revises, so they cannot change.

The submission is turned back, naming the problem, when:

- a finding has no disposition, a finding has two, or a disposition names an id that is not one of this round's findings.
- a `rejected` disposition has an empty reason.
- at least one finding is `accepted` and the body, open questions, relevant files and notes are all identical to the PRD under revision.
- it fails any check the PRD phase applies to the same fields: the body has no `## Intent` section by afspec's rule, the body starts with YAML frontmatter, or a relevant file is not a regular file in the workspace.

`## Intent` may change, because it is not hashed until the package is activated.

### Rounds

A round is one review, followed by one revision when the review raised at least one finding. After a round whose revision finished, another round runs only when both of these hold:

- that round's review raised at least one `critical` or `major` finding.
- fewer rounds than the limit have run.

A review whose findings are all `minor` is still revised, and that round is the last.

Each later round reviews the PRD as the previous revision left it. A later round's reviewer and reviser are both handed every earlier round's findings with their ids, dispositions and reasons. The reviewer is told that it may raise a rejected finding again only with evidence the rejection did not answer, and only by naming it in `reopens`.

The rounds stop for exactly one reason, reported as `stop_reason`:

- `no_findings`: the last review raised none.
- `no_serious_findings`: the last review raised only `minor` findings and its revision finished.
- `rounds_exhausted`: the last review raised a `critical` or `major` finding, its revision finished, and the round limit was reached.
- `incomplete`: a review or revision did not finish, or the budget stopped the rounds. This takes precedence over the others.

### When a round cannot finish

A review or revision can end without an accepted submission because of its budget, its turn limit, `--phase-timeout`, a refusal, or a model, API or internal error. When that happens:

- The rounds stop.
- The package is generated from the last complete PRD: the PRD phase's own when no revision finished, otherwise the last finished revision's.
- The run records a `high` warning `prd_review_incomplete` (stage `prd_review`) naming the scope (the spec name), the round, the phase that failed (`prd_review` or `prd_revision`) and why it failed.
- If the review finished but its revision did not, that round's findings are reported with the disposition `unanswered`.
- The failure does not fail the run and does not change its exit code.

When `--total-budget` is set, the run's spend so far is checked before each review and before each revision. When the spend has reached the ceiling, the rounds stop exactly as above. The warning names the total budget as the reason, with the spend and the ceiling. A budget stop between a review and its revision leaves that review's findings `unanswered`. The existing check before each split scope's PRD phase is unchanged.

An interrupt of the run itself (Ctrl-C or SIGTERM, which cancels the run's context) is not a review failure. If it arrives during a review or revision, the run stops as an interrupted run does in any other phase: stage `prd_review` or `prd_revision`, category `aborted`, exit 1. A phase that stopped at `--phase-timeout` while the run's context is still live is a review failure as described above.

### Splits

In a split, each scope's PRD goes through its rounds in the coordinator, after its PRD phase (and after the planned name replaces any name the model chose). Only then is its package number reserved and its package started. The first scope's rounds run after the split plan is recorded, like every other scope's.

The next scope's PRD phase is shown the revised PRD and the revised notes, not the originals. A resumed split reviews only the PRDs of the scopes it writes in this run.

### The output

When `--adversarial` is on, each package in the result carries `review`. That covers the first package and every package in `follow_on_specs`, including a package whose generation later failed and which is reported unwritten. `review` has these fields:

- `rounds_run`: rounds started, counting a review that raised no findings. It is 0 when the budget stopped the run before the first review.
- `rounds_allowed`: the round limit.
- `stop_reason`
- `findings_count`, `accepted_count`, `rejected_count`, `unanswered_count`: totals over all rounds.
- `rounds`: per round, its number and its findings. Each finding has `id`, `severity`, `section`, `flaw`, `evidence`, `remedy`, `reopens` (omitted when empty), `disposition` and, only when rejected, `reason`.

Without `--adversarial`, `review` is absent.

The written `prd.md`, the PRD posted by `--comment`, `result.open_questions` and `result.relevant_files` are all those of the final PRD. The generation and architecture phases receive the final PRD body, its relevant files and its notes.

The summary sentence gains one clause when any package of the run carries a review. The clause has the form `review: <R> round(s), <F> finding(s), <A> accepted`, totalled over every package of the run, with `, <A> accepted` left out when F is 0. For example: `spec: wrote .specs/07_widget_cache; 2 open questions; review: 2 rounds, 9 findings, 7 accepted`. The existing 200-character cap and the high-warning clause still apply.

Under `--detail summary`, the first package's `review` is kept with every field except `rounds`.

### Progress and usage

Each review and revision is reported on stderr as a phase, through the same begin/done lines and footer (turns and tokens) as every other phase. Its label carries the scope prefix in a split and the round of the limit, for example `scope 2 of 3 (issuex_github): reviewing the PRD (round 1 of 2)` and `revising the PRD (round 1 of 2)`. After a review, a progress line gives the number of findings by severity. After a revision, a progress line gives the number accepted and rejected.

Each review and revision is recorded in `usage.phases[]` under the name `prd_review` or `prd_revision`, with its scope, model, effort, turns and cost. It counts toward the run's spend.

### Preflight

With `--adversarial`, `estimate.phases` adds two phases per allowed round for every package it counts. That is `(1 + 3 [+1 with --architecture] + 2 × rounds) × packages`, and `max_total_usd` follows from it. The `phase_models` check detail lists `prd_review` and `prd_revision`, after `prd`, with their model and effort. Without `--adversarial`, neither the estimate nor `phase_models` changes.

### Models

`prd_review` and `prd_revision` are phase kinds that `--phase-model` accepts and that each tier resolves:

| Tier | `prd_review`, `prd_revision` |
|---|---|
| `SIMPLE` | `claude-sonnet-5-5` `medium` |
| `STANDARD` | `claude-sonnet-5-5` `high` |
| `ADVANCED` | `claude-opus-5-5` `xhigh` |

A model id given to `--model`, and `--effort` (or `$AF_MODEL_EFFORT`), apply to them as to every phase. A `--phase-model prd=…` does not move them. `--budget`, `--max-turns`, `--phase-timeout` and `--max-tokens` bound each review and each revision as a phase of its own.

### Documentation

Per the project steering, the implementation updates the following and runs `make doc` and `make check`:

- `docs/cli.md`: the flags, the tier table, `--phase-model` phases, preflight, `review` in the result, the summary clause, the warning code and the stages.
- `docs/api.md`: the new envelope types and fields.
- `docs/architecture.md`: a run, a phase, and what each phase is sent.
- A new ADR recording this decision.

## Design Decisions

1. **The reviser is a fresh agent, not the PRD's author.** The author would be inclined to defend its own choices, and a third agent weighs each finding on its evidence. The `codebase_notes` soften the cost of a third read of the code.
2. **The reviewer sees the author's `codebase_notes`, labelled as claims.** Without them it would re-read the code ADR 04 stopped re-reading, and the label keeps it checking instead of trusting.
3. **The round count is a separate flag, and `--adversarial` stays a plain switch.** The common case is `--adversarial` alone, which gives one round. A round count with nothing to count is refused rather than silently ignored, and an explicit `0` is refused rather than read as "not given".
4. **At most 5 rounds, stopping once nothing serious is found.** Each round is two full phases, and a review with only `minor` points does not justify paying for another.
5. **Later rounds see the earlier findings, dispositions and reasons.** A clean-context reviewer would otherwise raise the same rejected point every round.
6. **Repeats are detected mechanically through `reopens` and an evidence comparison, and the prompt does the rest.** "Evidence the rejection did not answer" cannot be judged in Go, so the run catches the verbatim repeat (same section and flaw, or same evidence for a declared reopen) and the prompt holds the reviewer to the rule beyond that.
7. **Findings get run-assigned ids `R<round>-F<n>`.** The reviser's dispositions and later rounds' `reopens` need a stable reference that the model cannot get wrong.
8. **A failed review or revision is a warning, not a failed run.** The review only improves a PRD that is already complete, and losing the package because the critic ran out of budget would be worse. The same handling already applies to a failed `--architecture` phase.
9. **An interrupt of the run is not a review failure.** A cancelled run context would fail the next generation phase anyway. Treating it as a warning would only delay the stop and misreport its cause.
10. **The revision cannot change the spec name, the title or the split, because its tool does not ask for them.** The input proposed turning back a revision that changes them. Leaving those fields out of the schema guarantees the same outcome at no repair turn and with fewer union parameters in a strict tool. The split plan and package numbers depend on the PRD phase's decision.
11. **The revision's optional fields follow "absent keeps, present replaces".** A reviser that forgets to repeat the open questions, relevant files or notes would otherwise silently drop them from the final PRD.
12. **An `accepted` disposition requires a changed PRD.** "Accepted" means the PRD was changed, and a revision that accepts findings and submits an identical PRD has not remedied anything.
13. **All five descriptive fields of a finding are required, and there are at most 30 findings per review.** A finding with no evidence costs a revision without giving it anything it can check, and the cap keeps a review from listing every stylistic preference.
14. **The review phases resolve to the tier's base entry, the PRD phase's model and effort, and `--phase-model prd` does not move them.** Judging a PRD takes the judgment that writing one does. `ResolvePlan` already lets each `--phase-model` choice decide its own phase alone.
15. **`model.phases`, the `--phase-model` help and the `effort_clamped` warning list the two new phases even without `--adversarial`.** They already list `architecture` without `--architecture`, because they describe the resolved plan rather than the phases that ran. Filtering them would need the shell to learn spec's flags. The result, summary, files, phases run and usage stay unchanged.
16. **The reviewer and reviser also get the spec landscape and, in a split, the split context.** Without the split context, a reviewer of scope 1 would report the other scopes' work as "dropped asks". Without the landscape, it could not judge "already exists under another name" against existing specs.
17. **A review with no findings counts as a round run and is recorded as a round with no findings.** `rounds_run` then says how many reviews happened, and the record shows that the PRD was reviewed and passed.
18. **The summary view keeps the first package's `review` counts and drops `rounds`.** A caller sees at a glance whether the review was complete and how much it changed. The `--detail summary` result still decodes into `envelope.SpecResult`, because the counts are fields of the same type.
19. **The summary clause totals over every package of the run.** It is one clause in a sentence capped at 200 characters, and per-package detail is in the result.
20. **The first scope of a split is reviewed after the split plan is recorded, inside the split loop.** Every scope is then reviewed the same way with the same split context. A run killed during those rounds resumes from scope 1 like any run that stops before its first package.
21. **The preflight estimate counts the maximum rounds.** The input called the figure an upper bound, which is only true of the review phases. For a fresh input the package count is still the one-package lower bound, and the docs say both.
22. **The budget check before a review or revision stops the rounds instead of failing the run.** This follows decision 8. The existing check before each split scope's PRD phase still fails the run as today, so a split over budget stops at its next scope.
23. **Usage records the phases by name and scope, without a round field.** `usage.phases[]` keeps its shape, rounds appear in order, and `result…review` carries the per-round detail.
24. **The progress footer is the same as every other phase's (turns and tokens), not cost.** The input said "cost and turns, like every other phase", but every other phase prints turns and tokens (`cli.PhaseSummary`), and cost is in `--verbose` and in usage.

## Verified External API

The work uses only agentkit-go symbols the repository already calls. The module source is outside the repository (the replace directive pins `github.com/agent-fox-dev/agentkit-go` to `v0.0.0-20261021121346-32eb7cdca451`), so the signatures below are those implied by existing call sites and are **unverified** against the source.

| Symbol | Signature as used | Call site |
|---|---|---|
| `core.Tool` | struct `{Name, Description string; InputSchema *schema.Schema; Terminating bool; PromptGuidelines []string; Execute func(context.Context, json.RawMessage) core.ToolResult}` | `specgen/prd.go` `submitPRDTool` |
| `core.ErrResult` | `func(code, message string) core.ToolResult` | `specgen/prd.go` |
| `core.OKResult` | `func(v any) core.ToolResult`; result has field `Terminate bool` | `specgen/prd.go` |
| `schema.Object` | `func(props ...) *schema.Schema` | `specgen/prd.go` `prdSchema` |
| `schema.Prop` / `schema.Opt` | `func(name string, s *schema.Schema)`, required and optional property | `specgen/prd.go` |
| `schema.String` | `func(description string) *schema.Schema` | `specgen/prd.go` |
| `schema.Array` | `func(items *schema.Schema, description string) *schema.Schema`; field `MaxItems *int` | `specgen/prd.go` `relevantFilesArraySchema` |
| `schema.Enum` | `func(description string, values ...string) *schema.Schema` | `specgen/patch.go` |
