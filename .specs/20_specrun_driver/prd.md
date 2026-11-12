---
spec_id: "20"
spec_name: "specrun_driver"
title: "Drive the implementation loop with specrun"
status: "active"
created_at: "2026-11-12T17:40:00Z"
intent_hash: "bacb8688e8c0edc3f6254c0994d9e37de2f20c32e133fddea2059ab454c12bb1"
schema_version: 3
source: "docs/prd/07-keep-the-implementation-loop-fast-and-close-its-open-items.md"
---

# Drive the implementation loop with specrun
## Intent

The implementation loop runs `/af-impl` on one package after another, each in
a fresh headless Claude Code session, until every package is done. Today a
shell script does it, and what the loop must do next (stop and repair
decisions, a reconcile, label rules, a ledger) is a state machine a script
cannot hold: its decisions could only be tested by a real run of several
hours. `specrun` is that loop as a Go command in this repository. It chooses
the next package from the same code `speca` uses, starts and watches the
session, checks from the repository that the package landed instead of
trusting the session's account, survives a crash, ends a session that has
stopped making progress, and keeps the full log and a ledger of durations.
Its decisions are tested in seconds, with no model and no forge.

## Goals

- **`specrun` does everything the script does.** It runs one step at a
  time: switch to the base branch, pull it, choose the next package in
  dependency order, run `/af-impl` on it in a `claude -p` session, and check
  that the package landed. A step that did not land stops the loop.
- **It resumes where it stopped.** Its state is one JSON file outside the
  repository; started again after a crash, a kill or a reboot, it checks the
  running step against the repository and continues.
- **A session that stops making progress is ended.** No event for 30 minutes
  (`--stall`) ends the session and stops the loop.
- **Runs are observable.** The full session stream and the final message of
  every step are kept, and a ledger holds one line per step with its
  duration and outcome. A package that took more than twice the median of
  the ones before it is called out.
- **It says what it runs.** Its own version, the af-impl skill it will run
  with the hash of its `SKILL.md`, and the versions of `claude` and `issue`.
  A skill that differs from the one it was built with is a warning, and with
  `--strict-skills` a refusal.
- **Its decisions are tested without a model.** Table-driven tests run it
  against a fake `claude` and a fixture repository.
- **The script is retired.** `scripts/impl-specs.sh` is deleted in the change
  that makes `specrun` do everything it does.

## Non-goals

This package is one scope of
`docs/prd/07-keep-the-implementation-loop-fast-and-close-its-open-items.md`.
Left to the packages after it:

- **Anything on the forge.** Labels, the checks on issues, the `AF-Run:`
  trailer and its ledger fields (`open_item_labels`); relabelling stale
  handoffs and stopping on serious defects (`open_items_read_back`); the
  reconcile (`specrun_reconcile`); repairs (`specrun_repair`). This package
  does not import `aflib/issuex` and needs no token.
- **`--strict-handoffs`** (`package_handoffs`) and **`--unattended`**
  (`unattended_mode`).
- **Running packages in parallel.** The loop stays sequential, in dependency
  order.
- **Another model runtime.** Every session is `claude -p`; `specrun` never
  calls a model API, and imports no package that depends on `agentkit-go`.
- **A `--schema` self-description.** It can be added when a program needs it.

## Background

`scripts/impl-specs.sh` is about 100 lines of bash: a loop over
`speca --next`, a `claude -p "/af-impl <spec>"` call with
`--permission-mode bypassPermissions` and
`--disallowedTools "ScheduleWakeup,CronCreate"`, and four checks that the
package landed (the base branch is checked out, the tree is clean, `HEAD`
moved, no task is pending or in progress). It sets
`CLAUDE_CODE_PRINT_BG_WAIT_CEILING_MS` so that a session waits 30 minutes for
its subagents. It saves only the session's final message, hardcodes the
branch `main`, and has no test.

The choice of the next package is in `cmd/speca/next.go`: the packages under
the spec root ordered by their dependencies, ties broken by number, the
first open one whose dependencies are finished. `cmd/speca` and
`internal/specpkg` import `internal/cli`, which links the model stack through
`agentkit-go`. `afspec` and `envelope` do not.

`aflib` (`github.com/agent-fox-dev/aflib`) is not yet a dependency of the
spec module. Its `envelope` package holds the JSON envelope the agent-fox
commands write, generic over the command's result.

The installed af-impl skill once differed from the source in
`skills/af-impl`, and nothing in the log said which version ran.
`internal/skillgen` renders the skills for each agent and installs them; for
Claude Code the user's copy is `~/.claude/skills/af-impl/SKILL.md`.

## Requirements

### 1. The loop

`specrun` runs one step at a time until every package is done, a step fails
its checks, or `--max` packages have landed. In this package the only step is
**implement the next package**.

Before every step it switches to the base branch and pulls it with
`--ff-only`. The base branch is the branch `refs/remotes/origin/HEAD` names,
else `main`. It then asks `afspec` for the next package, the answer
`speca --next` gives, and starts
`claude -p "/af-impl <spec_dir>" --output-format stream-json --verbose
--permission-mode bypassPermissions --disallowedTools
ScheduleWakeup,CronCreate` in the repository, with
`CLAUDE_CODE_PRINT_BG_WAIT_CEILING_MS` set to the stall time.

When the session ends, whatever its exit code, `specrun` checks that the
package landed:

- the base branch is checked out;
- the working tree is clean;
- `HEAD` moved;
- the package has no task `pending` or `in_progress`.

A step that fails a check stops the loop with exit code 1, naming the check.
When no package can start, because the open packages wait on something no
run can finish or depend on one another in a cycle, `specrun` reports why and
stops with exit code 1. When every package is done, or `--max` have landed,
it ends with exit code 0. A base branch that cannot be switched to or pulled,
and a missing `claude` or `git`, stop it before any session.

Its answer is one `aflib/envelope` envelope on stdout when it ends. Progress
goes to stderr, one line per step.

### 2. State and resume

`specrun` keeps its state in one JSON file under the user's state directory
(`$XDG_STATE_HOME`, else `~/.local/state`, in `agent-fox/specrun`), named
after the repository: the steps done, the step running with its start time
and the process id of `specrun`. It writes the file atomically after every
change.

Started again, it reads the file. A step recorded as running is checked
against the repository first: a package that landed counts as done; a
package whose branch `feature/<NN>-<spec_name>` exists is run again, and
af-impl continues the branch; a step that cannot be checked is run again. A
state file that cannot be parsed, and one whose recorded `specrun` process
is still alive, refuse the start. `specrun --reset-state` discards the file
and does nothing else.

### 3. Watching a session

`specrun` reads the session's `stream-json` output as it arrives. When no
event arrives for `--stall` (default 30 minutes), it ends the session and the
processes it started, records the step as stalled and stops the loop with
exit code 1. The session's exit code decides nothing: the landing checks do.

### 4. Logs and the ledger

- Each session's full stream is saved as `<step>.jsonl` in the log
  directory (`--log-dir`, default `agent-fox/specrun/<repository>` under the
  user's cache directory), beside its final message in `<step>.log`. A log
  directory inside the repository's working tree is refused, since af-impl
  refuses a dirty tree.
- One JSON line per step is appended to `ledger.jsonl`: the step's kind and
  subject, its start and end time, its wall minutes and its outcome
  (`landed`, `stopped`, `stalled`).
- After each step the line is printed in short form on stderr. When a
  package's wall minutes are more than twice the median of the packages that
  landed before it, a warning line follows.
- `specrun --ledger` prints the ledger as a table and starts nothing.

### 5. What it runs

At start `specrun` logs, and puts in its result: its own version; the
af-impl skill a session in this repository loads, by path
(`<repository>/.claude/skills/af-impl/SKILL.md` when it exists, else
`~/.claude/skills/af-impl/SKILL.md`) and the SHA-256 of that file; and the
versions `claude --version` and `issue --version` print. `make build`
embeds the SHA-256 of the af-impl `SKILL.md` as rendered for Claude Code
from this tree. When the installed skill's hash differs from the embedded
one, or no skill is installed, it warns; with `--strict-skills` it refuses to
start. A missing `issue` command is reported as not installed and stops
nothing.

### 6. Configuration and a dry run

The settings are flags: `--max`, `--stall`, `--log-dir`, `--strict-skills`.
An environment variable of the same name in upper case with `-` as `_`,
prefixed `SPECRUN_`, sets a flag's default; the flag wins.

`specrun --dry-run` changes nothing and starts no session: no branch is
switched, nothing is pulled and no state is written. It prints the next
step and the package `afspec` would choose, or why none can start.

### 7. Build, documentation and the script

`make build` installs `specrun` with `spec`, `specr` and `speca`.
`docs/specrun.md` documents it in the style of `docs/speca.md`, linked from
`docs/cli.md` and the README. `scripts/impl-specs.sh` is deleted in the same
change, and the docs and the README name `specrun` instead.

## Design Decisions

1. **The driver is Go, in this repository** (PRD decision 21). Its rules are
   tested in seconds against a fake session; the facts it decides on come
   from `afspec` in process; the hash of the skill it was built with only
   means something when both come from one tree.
2. **Claude Code stays the model runtime, through `claude -p`** (PRD
   decision 23), and `specrun` stays off `agentkit-go`.
3. **`specrun` checks its sessions and does not trust them** (PRD decision
   25). The landing checks read the repository, so a session that ends
   early, stalls or claims too much is caught the same way every time.
4. **State is one JSON file, not a database** (PRD decision 24). The
   repository records most of the truth; the file holds the running step.
5. **The choice of the next package moves into `afspec`.** `specrun` may not
   import `cmd/speca` or `internal/specpkg`, which link `agentkit-go`, and a
   second copy of the rule would drift. `speca --next` calls the same
   function and keeps its output.
6. **The base branch is `origin/HEAD`'s, else `main`, with no flag.** The
   script hardcoded `main`; the PRD lists the flags and names none for this.
7. **The fake session is a `claude` first on `PATH`.** No flag names the
   program, so a test and a real run start it the same way.
8. **`CLAUDE_CODE_PRINT_BG_WAIT_CEILING_MS` is the stall time.** The script
   set 30 minutes for both; one setting keeps a session from waiting on its
   subagents longer than `specrun` waits on the session.
9. **A second `specrun` on the same repository is refused.** Two loops would
   switch branches under each other; the state file records the process id.
10. **A log directory inside the working tree is refused.** af-impl refuses
    a dirty tree, so the first log would stop the first package.
11. **`docs/specrun.md` documents it.** `docs/cli.md` is the `spec`
    command's page; `speca` and `specr` each have their own, and `specrun`
    follows them, with a link from `docs/cli.md`.
12. **The embedded hash is the rendered skill's.** What is installed is the
    render for Claude Code, not the source file, so that is what is hashed.
    A `specrun` built without `make build` has no embedded hash and says so.
13. **The ledger's gate and open-item fields come later**, with the
    `AF-Run:` trailer they are read from (`open_item_labels`).

## Dependencies

- `afspec`: the packages, their dependency order and their task states.
- `aflib/envelope` (`github.com/agent-fox-dev/aflib`), a new dependency of
  the spec module, for the envelope `specrun` writes.
- Claude Code's `claude -p` with `--output-format stream-json`, `--verbose`,
  `--permission-mode` and `--disallowedTools`.
- `git`.
- No package under the spec root: `specrun` runs af-impl as it is, with or
  without `19_speca_gate`.

## Verified External API

- `github.com/agent-fox-dev/aflib/envelope`, read with `go doc` in a
  checkout at commit `6b70ef7`: `Envelope[R any]` with `Tool`, `Version`,
  `OK`, `Status`, `ExitCode`, `Summary`, `Error`, `Warnings`, `Result`,
  `Next`, `DurationMS`, `StartedAt`; `Encode(v any) ([]byte, error)`;
  `StatusFor(code int) string`; `ErrorInfo`, `Warning`. `aflib` has no
  release tag, so `go.mod` takes a pseudo-version.
- `claude` 2.1.322: the flags above are in `claude --help`. The shape of a
  `stream-json` event (one JSON object per line, the last of `type`
  `result` holding the final message in `result`) is **unverified**.

## Glossary

- **step**: one unit of the loop's work; here, implementing one package.
- **session**: one `claude -p` process running a skill to its end.
- **landed**: the package's branch is merged into the base branch and none
  of its tasks is open, as the four landing checks establish.
- **ledger**: the file of one JSON line per step.
