---
spec_id: "25"
spec_name: "gate_affected_tests_budget"
title: "Gate a task on the tests its change can affect and hold the full gate to a budget"
status: "active"
created_at: "2026-11-12T19:25:00Z"
intent_hash: "23f3a29b7a2a31eb730418b6a12ee231951d869e95fc04e6e3597250a75b7042"
schema_version: 3
source: "docs/prd/07-keep-the-implementation-loop-fast-and-close-its-open-items.md"
---

# Gate a task on the tests its change can affect and hold the full gate to a budget
## Intent

The gate between tasks runs the full test suite, and the full suite grows
with every package, so each package takes longer to land than the one
before although it is no larger. The cost of the loop should grow with the
size of a change, not with the size of the repository. Between tasks the
gate runs the linter and the tests the task's change can affect, through a
command the project provides; the full suite still runs before anything
lands. And because a suite that grows unnoticed is how the loop got slow,
the full gate is held to a budget: a run over it, or one that grew by more
than half on a branch, becomes a finding that reaches someone.

## Goals

- **The task gate scales with the change.** `tasks.json` may name an
  `affected_tests` command with the placeholder `{base}`; the task gate runs
  it with the commit the task started from. Without it, nothing changes.
- **"Affected" is the project's definition.** The format names the command
  and its one argument; the project's tool decides what the diff can affect.
- **A gate budget.** The steering may set `gate_budget_seconds` (default
  120). Every full gate's verdict says whether it exceeded the budget, and
  by how much it grew over the full gate of the base branch's tree.
- **A slow suite becomes a finding.** When the full gate on the merged tree
  exceeds the budget, or grew by more than half over the branch, the
  conformance pass files a `test-cost` finding as a minor defect.
- **A test-cost directive.** The steering template tells every project to
  keep tests parallel-safe and expensive fixtures built once.

## Non-goals

- **A recipe for fixing a project's test suite.** How a suite is made
  parallel, or its fixtures cheap, is the project's decision, made in the
  repair of the finding or by a person.
- **Weakening the end gates.** The full suite still runs at the baseline,
  after the integration task, after the conformance fixes and on the merged
  tree.
- **A default affected-tests command.** `spec` and af-spec write
  `affected_tests` only when the project already has such a command.
- **Any other change to the format.** `affected_tests` is optional, and a
  package without it validates as before.

## Background

`19_speca_gate` gave `speca <package> --gate task|full`. Both levels run
`linter`, then `all_tests`; the task level exists, is keyed and reported,
and runs the full suite. On the measured repository one gate run grew from
10 to 170 seconds over 25 packages of the same size, because several
consecutive packages extended the same areas: 171 tests ran one after
another, each building its own database and git fixtures, and no test in the
repository ran in parallel. Nothing in the steering, the skills or the
conformance pass asked about test cost.

`test_commands` in `tasks.json` holds `all_tests`, `linter` and an optional
`spec_tests` (`afspec/schemas/tasks.v3.json`, with its canonical copy under
`specification/schemas/`). The steering file `.specs/steering.md` may open
with a frontmatter of two keys, `version` and `amendments`
(`internal/project/steering.go`). `21_open_item_labels` gives a finding a
kind and a severity.

## Requirements

### 1. The affected-tests command is the project's

`tasks.json` `test_commands` gets an optional `affected_tests`: a command
that contains the placeholder `{base}` exactly once, for example
`make test-affected BASE={base}`. A command without `{base}`, or with it
more than once, fails validation. The project's tool works out the changed
paths itself, new and untracked files included, and decides what "affected"
means: reverse dependencies, a test-impact tool, or a directory map.
`spec` and af-spec write `affected_tests` only when the project already has
such a command; the af-spec survey looks for one.

### 2. The task gate runs it

`speca <package> --gate task [--base <commit>]` runs `linter`, then
`affected_tests` with `{base}` replaced by the full id of the commit
`--base` names (default `HEAD`, the commit the uncommitted task work sits
on). When the package has no `affected_tests` it runs `all_tests`, as
before. The command is split after the replacement and runs without a
shell. The command as it was run is part of the reuse key. A `--base` that
names no commit, and `--base` with the full level, are usage errors. af-impl
passes the commit the task started from.

### 3. A gate budget

The steering's frontmatter may set `gate_budget_seconds`, a whole number
greater than zero; the default is 120. For every full gate, run or reused,
`speca --gate` reports:

- the budget, whether the gate's seconds exceeded it and by how many;
- the growth: the gate's seconds over those of the full gate recorded for
  the base branch's tree (the tree of the merge base of `HEAD` and the base
  branch) with the same commands and environment, when one is recorded.

`speca <package> --gate-report` reports both for the last full gate, and
says that there is a test-cost finding when that gate exceeded the budget or
grew by more than half. The conformance pass then records a `test-cost`
finding, filed as a `defect` of severity `minor`, so it reaches someone
instead of disappearing into a commit message.

### 4. A test-cost directive (text)

`docs/templates/steering.md` gains a language-neutral "Test Suite Cost"
section: tests are independent and parallel-safe unless they say why not (in
Go `t.Parallel()`, in Python `pytest-xdist`-safe); an expensive fixture (a
migrated database, a repository, a key pair) is built once per process and
copied or cloned per test, not rebuilt; the full suite stays within the gate
budget. The conformance "Tests" checklist adds: "a new test that runs
serially without a stated reason, or rebuilds a fixture the shared helpers
already build once".

## Design Decisions

1. **Narrow the between-task gate, not the end gates** (PRD decision 1). A
   regression outside the affected set is caught at the latest after the
   integration task, on the same branch, before any merge.
2. **"Affected" is the project's definition, behind one command** (PRD
   decision 2). Test impact is ecosystem-specific.
3. **`affected_tests` takes the base commit, as `{base}`** (PRD decision
   11): one argument that cannot get long, placed wherever the command
   needs it.
4. **A 120-second budget by default** (PRD decision 15): the measured suite
   crossed it around the fifteenth package, when the slowdown first showed.
5. **The `{base}` rule is the library's, not the schema's.** "Exactly once"
   is awkward as a pattern and reads badly as a schema error; the rule is
   named `affected_tests_base` and says how many it found.
6. **The budget is a key of the steering's frontmatter.** The steering is
   where the PRD puts it, and the frontmatter is its only machine-read part.
   A value that is no whole number above zero is reported like any other
   malformed frontmatter, and the default applies.
7. **Growth is measured against the base branch's tree, found by its tree
   id.** That gate is the baseline of the branch; no extra state is needed.
   With no such record, growth is absent and only the budget can raise the
   finding.
8. **`--gate-report` says whether there is a finding.** "Over the budget or
   grew by more than half" is arithmetic; the conformance pass reads one
   field and files the issue.
9. **A full gate that failed is measured too.** A suite that is over budget
   is over budget whatever its verdict.

## Dependencies

- `19_speca_gate` (active): the gate, its record, its report and the
  af-impl text this package extends.
- `21_open_item_labels` (active): the `defect` kind and `minor` severity a
  test-cost finding is filed with.
- The v3 format (ADR 19): `test_commands.affected_tests` is an addition to
  `tasks.v3.json` and to `specification/spec-format-v3.md`.
- The steering template `docs/templates/steering.md`, ADR 15 for the
  steering's frontmatter, and the af-impl and af-spec skills.

## Glossary

- **affected tests**: the tests a project's own tool selects for the diff
  from a base commit.
- **gate budget**: the number of seconds a full gate may take before it is a
  finding.
- **growth**: how much longer the full gate takes than it did on the base
  branch's tree, as a fraction of that.
