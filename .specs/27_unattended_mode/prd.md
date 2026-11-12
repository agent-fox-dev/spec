---
spec_id: "27"
spec_name: "unattended_mode"
title: "Give each task a fresh context when af-impl runs unattended"
status: "active"
created_at: "2026-11-12T20:00:00Z"
intent_hash: "676fd93c4b51cfbea836da58aebf73af9f4a0ff5b8521fb4f438fad38f2eea7a"
schema_version: 3
profile: "lite"
source: "docs/prd/07-keep-the-implementation-loop-fast-and-close-its-open-items.md"
---

# Give each task a fresh context when af-impl runs unattended
## Intent

A package's implementation runs in one session, and that session's context
grows with every task: it reads more existing code at every step, and each
turn over a context of half a million tokens is slower than the last. In
the measured run, model time per package grew from 8 minutes to between 19
and 33 while the packages stayed the same size. A person watching wants one
continuous session; an unattended run wants each turn cheap. In an
unattended run each implement task works in a fresh context, handed only
what the task needs, and the orchestrating session keeps the claim, the
record and the commit. Only where the work happens moves.

## Goals

- **A fresh context per task, when nobody is watching.** af-impl accepts
  `--unattended`; in that mode each implement task runs in a subagent that
  receives the survey brief, the task's scoped render, the files the task
  touches and the progress lines so far, and returns the task's progress
  line, the commit, the gate verdict and the criterion-to-test map.
- **The brief is the hand-off.** In unattended mode the survey brief is
  written to a file outside the repository and stays within 600 lines. A
  subagent reads the brief, not the orchestrator's transcript.
- **`specrun` asks for it.** `specrun` starts every package session with
  `--unattended`.

## Non-goals

- **Changing the attended run.** Without the argument af-impl runs as
  today, in one session, for a person watching.
- **Changing the commit discipline or the state machine.** The
  orchestrating session claims each task, records it done and commits, as
  today; a task still lands only on a passing gate and tests seen failing
  first.
- **The reconcile and repair sessions.** Each is one bounded unit of work
  already; they are started as before.
- **Running tasks in parallel.** Tasks stay sequential.

## Background

`skills/af-impl/SKILL.md` runs the survey, every task, the conformance pass
and the landing in one session. Only the conformance review runs in a fresh
subagent, and it stayed between 4 and 10 minutes over 25 packages while the
rest of the model time tripled. The session's context reached 550k to 650k
tokens. af-impl already writes what a task needs to know for a session
resumed after a gap: the survey brief, and each task's commit body.

`specrun` (`20_specrun_driver`) starts a package's session with the prompt
`/af-impl <spec_dir>`. Its ledger records each package's wall minutes, so it
shows whether model time still grows with the repository once the gate
changes of `19_speca_gate` and `25_gate_affected_tests_budget` are in.

## Requirements

### 1. `specrun` asks for the unattended mode

`specrun` starts the session of an implement step with the prompt
`/af-impl <spec_dir> --unattended`. The prompts of its other steps do not
change.

### 2. An unattended mode with a fresh context per task (af-impl text)

af-impl accepts the argument `--unattended`. In this mode each implement
task runs in a subagent. The subagent receives the survey brief, the task's
scoped render, the files the task touches and the progress lines so far. It
returns the task's progress line, the commit, the gate verdict and the
criterion-to-test map. The orchestrating session claims, records and commits
as today. Without the argument, af-impl runs as today.

### 3. The brief is the hand-off (af-impl text)

In unattended mode the survey brief is written to a file outside the
repository and stays under a size limit: 600 lines. A subagent reads the
brief, not the orchestrator's transcript. What a later task needs to know
from an earlier one goes into that task's commit body, where af-impl already
puts it.

## Design Decisions

1. **Bound the context by task, in unattended mode only** (PRD decision 5).
   The brief and the commit bodies already carry what a task needs, because
   af-impl writes them for a session resumed after a gap.
2. **This package comes last on purpose.** The PRD says that if the ledger
   shows model time no longer growing with the repository after the gate
   changes, unattended mode can be dropped. Whether to implement this
   package is its owner's call, made on that evidence.
3. **Profile: lite.** The one behaviour a program can check is the prompt
   `specrun` sends; the rest is skill text, which the project's steering
   forbids testing. That is one requirement and no flow across components.
4. **No flag turns it off in `specrun`.** The PRD says "`specrun` passes
   `--unattended`" and lists no setting for it; a person who wants the
   continuous session runs `/af-impl` themselves.
5. **The subagent does not commit.** It returns the commit's content and
   message to the orchestrating session, which records the task done and
   commits, so the claim, the state and the commit stay in one place.

## Dependencies

- `20_specrun_driver` (active): the session `specrun` starts for a package.
- `23_specrun_reconcile` and `24_specrun_repair` (active): their sessions'
  prompts, which this package leaves unchanged and checks.
- The af-impl skill (`skills/af-impl`), which gains the mode.

## Glossary

- **unattended mode**: af-impl run with `--unattended`, each implement task
  in a subagent.
- **orchestrating session**: the af-impl session that surveys, claims,
  records, commits, runs the conformance pass and lands.
- **brief**: the survey's written result, which every task works from.
