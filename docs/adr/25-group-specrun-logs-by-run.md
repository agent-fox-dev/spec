# 25. Group specrun's logs by run

## Status

Accepted

## Context

[ADR 23](23-drive-the-implementation-loop-with-specrun.md) keeps every
session's stream, final message and stderr in one log directory per
repository, with a ledger line per step. Every call of `specrun` wrote into the
same flat directory. Step numbers count across calls, so files never collided,
but nothing tied a file or a ledger line to the call that wrote it. Someone
auditing a repository that `specrun` had been started on several times (after
a stop, a stall, a reboot, or just with `--max 1`) could not tell where one
call's work ended and the next began. They also could not find a session
again in Claude Code, or tell which settings a call had run with.

## Decision

- **Every loop is a run with a run ID**: the UTC time it started, to the
  second, then four hex digits drawn at random (`20261112T180047Z-7f3a`). The
  IDs sort by start, a person can read the time off one, and no two runs share
  a directory: one that exists already is never reused. A dry run, `--ledger`
  and `--reset-state` are no run.
- **A run's files go in `runs/<run ID>/`** under the log directory: its
  sessions' `<step>.jsonl`, `.log` and `.stderr`, and a reconcile's adopt file
  and report. Step numbers still count across runs, so a step's file name
  matches its ledger line wherever it is.
- **The ledger stays one file per repository**, and each line carries the
  `run_id` that wrote it. The slow-package median, the reconcile that is due,
  and the stop a restart repeats are read from every run's lines, so splitting
  the ledger per run would break them. `specrun --ledger` shows a `RUN` column,
  and `--ledger --run <ID>` shows one run's steps.
- **Each run writes `run.json`** when it starts (ID, start, pid, the resolved
  settings, the versions) and again when it ends, with the envelope it wrote
  to stdout. Before this, the envelope was kept nowhere. A `run.json` with no
  `ended_at` belongs to a run that never ended.
- **The state file names runs**: the step running records the run that
  started it, and the owner its run, which a refusal names. A line written
  for a landing or repair found on restart carries `resumed_from`, the run
  whose directory holds that step's session.
- **Each step's claude `session_id`** (from the stream's `init` event) goes
  into its ledger line as `claude_session_id`, so that `claude --resume`
  can open the session again.
- **Sessions get `SPECRUN_RUN_ID`** in their environment, so that what they
  write can name the run they belong to. Nothing reads it yet.

## Consequences

- Logs written before this change stay in the log directory itself, and their
  ledger lines have no `run_id` (`-` in the table). Nothing is migrated, and
  nothing that reads the ledger needs the field.
- Finding a step's files takes its `run_id` as well as its step name.
- The `AF-Run:` trailer af-impl writes does not carry the run ID. Adding it
  would change the skill, and `SPECRUN_RUN_ID` makes that possible later.
