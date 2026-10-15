# 04. Hand on what a phase learned, and correct instead of regenerating

**Status:** accepted
**Date:** 2026-10-13
**Related:** [ADR 02](02-choose-the-model-per-phase.md),
[ADR 03](03-pipeline-the-scopes-of-a-split.md)

## Context

A package costs one PRD phase and three generation phases, and three kinds
of work were being paid for more than once:

- **Reading the code.** The PRD phase reads the modules the work touches. Each
  generation phase is a fresh agent and read them again to write criteria,
  tests and tasks against the same signatures.
- **Regenerating an artifact.** A rejection names a handful of fields, and the
  only way to answer it was to submit the whole artifact again. For a tasks
  artifact that is tens of thousands of output tokens, the slowest part of a
  phase, spent to change a few lines.
- **Sending the same context.** The repository map, the steering and the
  language block were placed in each phase's task, after the parts that differ,
  so no two phases shared them as a cached prefix.

Separately, the validator's warnings (vague wording, missing contracts, scope
limits) were reported after the run and never reached the model that could fix
them.

A constraint shapes all of it: every tool is declared strict, and the API
allows at most 16 union parameters across a request's strict tools. The strict
rewrite turns each optional property into a union, so a request's tools have
little room to spare.

## Decision

- **The PRD phase writes `codebase_notes`**: files, real signatures and
  conventions, with paths, bounded to 12000 bytes. Every later phase of the
  package gets them, marked as notes rather than instructions, and a later
  scope of a split gets the notes its earlier scopes wrote in this run. A
  phase reads a file only for what the notes do not answer.
- **Each generation step has a patch tool**, `patch_<step>`: RFC 6902 `add`,
  `remove` and `replace` on the submission the step last turned back, with
  each value as JSON text in a string, so the schema has no union. The patched
  artifact goes through exactly the checks a submission does, and a rejected
  patch becomes the submission the next patch applies to.
- **Warnings are sent back once.** An artifact that passes every rule but
  carries warnings on its own file is turned back with them. The model fixes
  what it agrees with, or keeps the artifact with an empty patch, and the next
  submission is accepted.
- **The prompts are laid out for the cache.** What is the same for the whole
  run (language, steering, repository map) ends the system prompt. The PRD
  phase's input is its cached prefix. What differs per package and per step
  is the task.
- **The tools stay per step.** Giving every generation phase the same tools
  would let the steps of one package share a cached prefix. But it would put
  three artifact schemas in one request, past the union limit, and the
  artifacts would lose strict mode, the API's guarantee that a submission has
  the artifact's shape.
- **The artifact schemas stay strict-expressible.** The identity fields the
  handler writes are no longer declared, and the glossary, which is a
  dictionary object, is asked for as a list of entries and stored as the
  object.

## Consequences

- A rejection usually costs a short patch instead of a full regeneration.
  The rejection message says so.
- Phases that run the same step on the same model, the packages of a split,
  read the run context from the cache. Inside one package, the steps do not
  share a prefix, because their tools differ.
- A requirements phase with vague wording takes one more turn. The model may
  keep the wording, and the warnings then appear in the result as before.
- Notes can be wrong, and a phase that trusts them cites a signature it did
  not read. The prompts ask for a file read before a signature is cited, and
  the PRD phase marks what it inferred.
- The fix for the strict limits themselves is in agentkit-go: tools are
  declared strict in the order they are sent, while they fit, and the rest are
  sent without the flag. Until spec depends on an agentkit-go with that fix,
  a request with the read-only file tools exceeds the limit. The API's
  grammar-size limit is not counted there; see
  [ADR 07](07-keep-the-builtin-tools-out-of-strict-mode.md).
