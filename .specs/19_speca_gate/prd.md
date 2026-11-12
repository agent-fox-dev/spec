---
spec_id: "19"
spec_name: "speca_gate"
title: "Run a package's gate with speca and reuse a result that passed"
status: "active"
created_at: "2026-11-12T16:05:29Z"
intent_hash: "8d9ee73fdc7fed9ef2d3482942795cd906f41526642fc1bf1b25a6aec0bf8100"
schema_version: 3
source: "docs/prd/07-keep-the-implementation-loop-fast-and-close-its-open-items.md"
---

# Run a package's gate with speca and reuse a result that passed

## Intent

The gate that decides whether a task or a branch may land is the package's
own `linter` and `all_tests` commands. Today a model runs them by hand, after
every task, and keeps the results in its head: it runs trees that were already
checked, nothing measures what a run costs, and the bookkeeping differs from
run to run. `speca` runs the gate instead. One call runs the commands, reuses
a passing result for a tree that was already checked, keeps the output out of
the model's context, measures the run, and answers with one JSON verdict. A
second call sums a package's runs for the merge commit. The gate is the same
for a person, an unattended run and a repair, and its cost is on record.

## Goals

- **The gate is a command.** `speca <package> --gate task|full` runs the
  package's `linter`, then its `all_tests`, without a shell, stops at the
  first command that fails, and answers with one verdict: `pass`, `fail` or
  `not_run`.
- **A tree that passed is not checked again.** A gate whose working tree,
  level, commands and environment match a recorded pass returns that pass
  without running anything, and says which commit and time it came from. A
  failure is never reused.
- **The clean-environment run is a flag.** `--clean-env` runs the gate with
  an empty `HOME`, no global or system git configuration and
  `GIT_TERMINAL_PROMPT=0`.
- **A failure is readable without a log.** Every command's full output goes
  to a log file outside the repository; the verdict names the file and holds
  the last 40 lines of a failing command.
- **Gate cost is measured.** Every run records its seconds, and a full gate
  whose output is Go's keeps its five slowest test packages.
- **One report for the merge commit.** `speca <package> --gate-report` sums
  the package's recorded runs since a commit: full and task runs, their
  seconds, how many were reused, the full gate's duration at the baseline and
  on the last tree, and the slowest units of the last full gate.
- **af-impl calls the command.** af-impl runs every gate through
  `speca --gate`, runs the full gate only at the baseline, after the
  integration task, after the conformance fix commit and on the merged tree,
  and runs a more thorough repository target once, on the merged tree.

## Non-goals

This package is the first of nine scopes of
`docs/prd/07-keep-the-implementation-loop-fast-and-close-its-open-items.md`.
Each of the others is a package of its own:

- **`specrun_driver`**: the Go driver that replaces `scripts/impl-specs.sh`
  (PRD 5.1, 5.2, 5.7 to 5.9, 5.11 to 5.14).
- **`open_item_kinds`**: kinds, labels and counts of open items, and closing
  what a branch resolved (PRD 3.1, 3.2, 3.5, 3.7).
- **`open_items_read_back`**: no handoff to a landed package, the survey
  reading open items, passing them on, and the `AF-Run:` trailer (PRD 3.3,
  3.4, 3.6, 5.10). Until then af-impl copies the gate report into the merge
  commit's body as prose.
- **`specrun_label_rules`** and **`specrun_repair_reconcile`** (PRD 5.3 to
  5.6, 3.8).
- **`gate_affected_tests_budget`**: `test_commands.affected_tests` with its
  `{base}` placeholder and the `--base` flag, the gate budget and the growth
  over the base branch, and the test-cost directive (PRD 1.2, 1.6, 1.7).
  Until then the task gate runs `all_tests`, and the verdict carries no
  budget.
- **`package_handoffs`** (PRD 4.1 to 4.5) and **`unattended_mode`** (PRD 2.1,
  2.2).

Also out:

- **Weakening the test-first gate.** Every task's new tests are still seen
  failing first, and every task still lands only on a passing gate.
- **Slowest units for other tools.** Only Go's test output is recognised.
  pytest files and Jest suites are later additions; for any other output the
  list is empty.
- **A change to the format.** No schema changes in this package.
- **Deciding the verdict against the baseline.** af-impl still compares the
  gate's verdict with the baseline using its own table. Only the running and
  the bookkeeping move into `speca`.

## Background

af-impl's gate is `linter`, then `all_tests`, from the package's
`test_commands`. `skills/af-impl/SKILL.md` has the model run both at the
baseline (§2), after every task and after the integration task (§5), in a
clean environment and after the conformance fixes (§6,
`references/conformance.md`), and on the merged tree (§7,
`references/landing.md`). That is 14 to 16 runs for a ten-task package.

Measured over 25 packages on one repository, the time in the gate grew from 3
to 44 minutes per package while the packages stayed the same size. Two of the
causes are in this package's reach:

- **Some runs repeat a tree that was already checked.** The baseline of
  package N+1 is the merged tree of package N. When the base branch did not
  move, the merged tree of a `--no-ff` merge is the branch tip's tree.
- **The quality command was widened per task.** The project template
  (`docs/templates/AGENTS.md`, "Quality Commands") says "where a target such
  as `check` is more thorough than the `test_commands`, run that target
  instead", so every task gate became `make check`.

Nothing records what a gate run cost, so the slowdown was seen only in wall
time, hours later.

`speca` (`cmd/speca`) already reads `test_commands` through `afspec` and is
the command af-impl calls for every other mechanical step. It has eight
forms, chosen in `cmd/speca/forms.go`, each answering in the envelope of
`envelope.AdminResult`. Today no form starts a process, and nothing in the
module writes under the user's state directory.

## Requirements

### 1. Running a gate

`speca <package> --gate task|full [--clean-env] [--timeout <duration>]` runs
one gate of the package and answers in speca's JSON envelope.

- **The commands.** Both levels run `linter`, then `all_tests`, from the
  package's `test_commands`, in the repository `--dir` names. The level is
  part of the answer and of the reuse key. (The task level gets its own
  command in `gate_affected_tests_budget`.)
- **No shell.** Each command string is split into a program and its
  arguments on white space, with single and double quotes keeping a quoted
  stretch together, and the program is started directly. A pipe, `&&`, a
  redirection or `$VAR` reaches the program as a literal argument.
- **First failure stops.** A command that exits non-zero ends the gate with
  `fail`; the commands after it do not run.
- **`not_run`.** A command that cannot start, because its program is not
  found or the package's `test_commands` name no command for it, ends the
  gate with `not_run`, with the reason.
- **A timeout.** A command still running after `--timeout` (default 20
  minutes) is ended together with the processes it started, and the gate
  ends with `fail`, marked as timed out.
- **Any status, no writes.** The gate is not a task move. It runs for a
  package in any lifecycle status and writes nothing into the repository or
  the package.
- **Exit code.** `0` when the gate ran, whatever its verdict; the verdict is
  in the JSON. `2` for a level that is neither `task` nor `full`, for a gate
  combined with another form, and for a gate flag without `--gate`.

The verdict (`result.gate`) holds: `level`; `verdict`; `clean_env`; `reused`
when the result was reused; per command its `name`, `exit_code`, `seconds`,
the path of its `log`, and for a failing command its `tail`; the total
`seconds`; and for a full gate the `slowest` units.

### 2. The clean environment

`--clean-env` runs every command of the gate with an empty temporary
directory as `HOME`, no global and no system git configuration, and
`GIT_TERMINAL_PROMPT=0`. Everything else in the environment is inherited.
The temporary directory is removed when the gate ends. Without the flag the
commands inherit speca's environment unchanged.

### 3. Reusing a result that passed

Every gate that ends in `pass` is recorded in a file outside the repository,
under the user's state directory, keyed by four things:

- the tree id of the working tree as it is, tracked and untracked files
  included and ignored files left out, computed through a temporary index so
  that the user's index is not touched;
- the gate level;
- the exact `linter` and `all_tests` command strings;
- whether the environment was clean.

A gate whose key matches a recorded pass runs nothing and returns `pass`,
with `reused` naming the commit that was checked out and the time of the run
it came from, and that run's seconds. A `fail` or a `not_run` is never
recorded for reuse.

Two things can keep a result from being recorded, and neither stops the
gate: `--dir` is not inside a git work tree, so there is no tree id; or the
state directory cannot be written. The gate runs, answers with its verdict,
and raises a `gate_not_recorded` warning that says why.

### 4. Output and logs

Each command's full output, stdout and stderr together, goes to a log file
outside the repository, under the user's state directory. The verdict names
the file. For a failing command the verdict also holds the last 40 lines of
its output, or all of it when it is shorter, so the model reads a failure
without reading a log. None of a command's output reaches speca's stdout,
which stays one JSON object.

### 5. Measuring a gate

Every command's wall-clock seconds are recorded, and the gate's total is
their sum. For a full gate, speca reads the `all_tests` output for Go's
per-package result lines (`ok <package> <n>s`, `FAIL <package> <n>s`) and
keeps the five with the most seconds, slowest first. Lines without a
duration (`(cached)`, `[no test files]`) are not units. For an output it
does not recognise, the list is empty. A task gate carries no list.

### 6. The gate report

`speca <package> --gate-report [--since <commit>]` sums the gate runs
recorded for this package and writes nothing. A run counts when the commit
that was checked out when it ran is `--since` itself or a descendant of it
that is reachable from `HEAD` or, while a merge is uncommitted, from
`MERGE_HEAD`. Without `--since`, the commit is the merge base of `HEAD` and
the base branch: the branch `origin/HEAD` names, else `main`.

The report (`result.gate_report`) holds:

- for the full level and for the task level: the number of runs, their
  seconds, and how many were reused;
- the full gate's duration at the baseline, the earliest full run counted,
  and on the last tree, the latest one; a reused run's duration is that of
  the run it came from;
- the slowest units of the last full gate.

With no run to count, every number is zero and the list is empty. A
`--since` that names no commit, or no `--since` and no base branch to take
it from, is a usage error.

### 7. Describing the gate

`speca --schema` and the help describe the two forms and their flags. The
envelope's summary says the level, the verdict and the seconds of a gate, or
that it was reused, and for the report the runs it counted.

### 8. af-impl and the project template

These are changes to text, made in the same change as the command:

- **Two gate levels.** af-impl runs the task gate (`speca <package> --gate
  task`) after every implement task, and the full gate (`--gate full`) at
  the baseline, after the integration task, after the conformance fix commit
  when there is one, and on the merged tree. The clean-environment run of
  the conformance pass is `--gate full --clean-env`. The verdict table of
  af-impl §5 applies to both levels.
- **The gate runs the spec's commands.** A more thorough repository target,
  such as `make check`, runs once, on the merged tree, before the merge is
  committed. af-impl's text says this order takes precedence over a project
  instruction to substitute the thorough target for every check.
- **The report.** af-impl notes the commit of the baseline, and copies
  `speca <package> --gate-report --since <that commit>` into the merge
  commit's body and its own report: the full gate's duration at the baseline
  and on the merged tree, the slowest units, and the runs with how many were
  reused. A reused gate is reported as "reused the gate of `<commit>`,
  `<date>`".
- **The template.** The "Quality Commands" section of
  `docs/templates/AGENTS.md` is reworded: the thorough target before
  committing a merge, the spec's commands between tasks.

## Design Decisions

1. **The gate is a `speca` command, not instructions to the model** (PRD
   decision 16). Recording results by tree, timing runs, parsing per-unit
   times and summing runs are mechanical; as skill instructions they are
   fragile and add model turns to the path this plan makes cheaper.
2. **Reuse a gate by tree id, never by commit** (PRD decision 3). Two
   commits with the same tree are the same code. The key includes the
   commands and the environment, so a clean-environment run is never
   replaced by a normal one. Only passes are cached: a failure is cheap to
   run again and must be seen.
3. **The thorough repository target runs once, before the merge commit**
   (PRD decision 4). Formatting and tidiness cannot change a test's result,
   and the gate on the merged tree decides what lands.
4. **Both levels run `all_tests` in this package.** The PRD's sequencing
   puts `affected_tests` in a later step; the level is still recorded and
   keyed, so the later package changes one command and no caller.
   `--base` is left to that package, because without `{base}` it would do
   nothing.
5. **The budget fields are left out of the verdict.** The budget and the
   growth over the base branch belong to `gate_affected_tests_budget`; a
   field that is always absent would only mislead.
6. **A command is split on white space with quotes, and nothing else.**
   `test_commands` must need no shell features, and af-impl already checks
   that. Splitting with quotes covers `make test` and
   `go test ./... -run 'A|B'`; anything more would be a shell.
7. **A gate command times out after 20 minutes by default.** The PRD's
   driver ends a session after 30 minutes without an event and requires a
   single gate to stay below that; a hung test must end the gate, not the
   session.
8. **A gate that cannot be recorded still runs.** Outside a git work tree,
   or with a state directory that cannot be written, the verdict is still
   true; only the reuse and the report lose a run, and a warning says so.
9. **The report selects runs by ancestry, not by time.** The run log holds
   the commit checked out at each run. Counting the runs at `--since` and at
   its descendants on this branch needs no clock and survives a base branch
   that moved; `MERGE_HEAD` is included because the gate on the merged tree
   runs before the merge is committed.
10. **af-impl passes `--since`.** It knows the commit its baseline ran on.
    The default, the merge base with the base branch, is for a person.
11. **The state lives under `$XDG_STATE_HOME`, else `~/.local/state`, in
    `agent-fox/speca`.** Linux and macOS are the target platforms, and one
    directory for both keeps the docs and the tests the same.
12. **Only the template's "Quality Commands" is reworded.** The PRD names
    `docs/templates/AGENTS.md`. This repository's own `AGENTS.md` and
    `CLAUDE.md` are left as they are; af-impl's text takes precedence over
    them for the gate.
13. **The skill and template texts are task steps, not criteria.** The
    project's steering forbids a test of what a document says, so they carry
    no check; `make skills-check` holds the skill to its render rules.

## Dependencies

- `speca`'s JSON envelope and exit-code conventions (`docs/speca.md`,
  ADR 10), which both forms follow.
- `afspec`, for the package and its `test_commands`.
- `git`, for the tree id, the commit checked out and the ancestry of the
  report. No other package under the spec root: every earlier package is
  archived.
- The af-impl skill (`skills/af-impl`) and the project template
  (`docs/templates/AGENTS.md`), whose text changes.

## Glossary

- **gate**: the package's `linter` command followed by its `all_tests`
  command, run to decide whether work may land.
- **task gate**, **full gate**: the gate after an implement task, and the
  gate at the baseline, after the integration task, after the conformance
  fixes and on the merged tree.
- **baseline**: the gate on the unchanged tree, before a package's first
  task.
- **tree id**: git's hash of a directory tree's content; two trees with the
  same id hold the same files.
- **unit**: what a test tool reports a duration for; for Go, a test package.
