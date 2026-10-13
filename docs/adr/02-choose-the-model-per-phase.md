# 02. Choose the model per phase

**Status:** accepted
**Date:** 2026-10-13
**Related:** [ADR 01](01-run-the-spec-command-on-the-agentkit-driver.md)

## Context

A run builds every package in four or five phases — PRD, requirements, test
spec, tasks, and optionally architecture — and until now every phase ran on
the one model and effort `--model` resolved to. A multi-spec run repeats those
phases once per scope, and it is slow.

The phases do not need the same model. The PRD decides what the work is and
how it splits, and the requirements decide what each criterion demands; a
mistake there carries into everything generated after it. The test spec, the
tasks and the architecture turn the requirements into structure the format's
validator checks rule by rule, and a phase that gets one wrong is told which
rule and repairs it. Effort is paid on every turn of a phase, including the
many turns that only read a file, so running a structural phase at the effort
the judgment phases need spends time and tokens for little.

## Decision

- **A tier names a model and an effort per phase.** `SIMPLE` runs the PRD and
  the requirements on `claude-sonnet-5-5` at `medium` and the rest at `low`;
  `STANDARD` at `high` and `medium`; `ADVANCED` runs the PRD on
  `claude-opus-5-5` at `xhigh`, the requirements on it at `high`, and the rest
  on `claude-sonnet-5-5` at `high`.
- **`--phase-model PHASE=MODEL[:EFFORT]`** (repeatable) moves one phase to
  another tier or catalog model, and effort. The effort suffix is split off
  only when it names an effort.
- **The existing flags keep their meaning for every phase.** A model id given
  to `--model` runs every phase; `--effort` (or `$AF_MODEL_EFFORT`) sets every
  phase's effort except where a `--phase-model` names its own.
- **The run says what it used.** `model.phases` in the envelope,
  `usage.phases[].model` and `.effort` per phase, the `phase_models`
  preflight check, and one `effort_clamped` warning per model and effort
  naming the phases it applies to.

Haiku is not a tier default. It is one `--phase-model` away for a caller who
wants it, and becomes a default only once the specs it produces have been
compared with Sonnet's.

## Consequences

- The tiers produce different runs than before: under `STANDARD` the test
  spec, tasks and architecture phases think less, and under `ADVANCED` they
  run on Sonnet. A caller who wants the old behaviour passes `--effort` (one
  effort everywhere) or a model id to `--model`.
- `model.id` and `model.effort` in the envelope now describe the default
  (what `--model` resolved to), not necessarily every phase; `model.phases`
  is authoritative per phase.
