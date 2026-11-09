# 20. Report the authoring hints beside the verdict

**Status:** accepted
**Date:** 2026-11-09
**Related:** [ADR 04](04-hand-on-what-a-phase-learned.md), [ADR 10](10-report-and-change-the-state-of-a-package-with-its-own-command.md), [ADR 18](18-allow-extension-keys-and-move-word-checks-out-of-the-validator.md)

## Context

A package is written in one of two ways: by `spec`, in an unattended model
run, or by the `af-spec` skill, in a session. Both are held to the same
verdict, `speca --validate`. `spec` also applies three checks that are not
the format's rules:

- `vague_language`: a vague word in a criterion (ADR 18 took it out of the
  validator);
- `line_reference`: a step or `touches` entry that names a line of a file;
- the test-command audit: a `test_commands` entry whose program belongs to
  another ecosystem (`pytest` in a Go repository), which `spec` refuses.

They lived in `specgen` and `internal/project`, where only a generation phase
could reach them. A package written in a session never met them; the skill
asked the agent to read for the same words and lines itself, which is the
chore the audit was written to remove.

## Decision

- **The checks move to `internal/authoring`**, one definition that `specgen`
  and `speca` both call. `internal/project` gains `Profile.ForeignCommands`,
  which `AuditTasks` and the hint share, so the refusal and the hint cannot
  disagree about what is foreign.
- **`speca --validate` reports them in `result.hints`**, a list beside
  `validation` in the shape of a validation item (`check`, `artifact`,
  `entity`, `message`), `[]` when there are none, and counts them in the
  summary. The repository is `--dir`, whose manifest decides the ecosystem.
- **A hint is not a finding of the verdict.** It is never in
  `validation.warnings`, never changes `valid`, and never blocks an
  activation. ADR 18's rule stands: the format's validation matches no
  words.
- **`spec` behaves as before**: the vague word and the line reference are
  sent back once, the foreign command is refused.

## Consequences

- The skill gets the same mechanical checks as the pipeline: `af-spec` reads
  `result.hints` after each verdict instead of rereading its own text for
  them.
- `AdminResult` gains `Hints`; a program that decodes it strictly sees a new
  key under `--validate` only.
- `speca --validate` now reads the repository's manifests as well as the spec
  root. A repository of no detected language gets no
  `foreign_test_command` hint.
- The word list and the line pattern are still heuristics: a hint can be
  wrong, which is why it is reported and not ruled on.
