# Keep the implementation loop fast and close its open items

## Intent

`scripts/impl-specs.sh` runs `/af-impl` on one package after another, each in
a fresh headless session, until every package is done. It works, but it
degrades in two ways as the project grows.

First, each package takes longer to land than the one before. The cause is not
package size. It is the cost of the checks af-impl repeats for every task, and
the context the session carries.

Second, the issues af-impl opens for its open items are only ever written,
never read. Gaps meant for a later package are not handed to it. Gaps that a
later package closes stay open. Real defects pile up unfixed, and later
packages build on top of them or copy code around them.

This plan changes the runner, the af-impl skill, the af-spec skill, the
steering template and `speca`:

- the loop's cost grows with the size of a change, not the size of the
  repository;
- every open item has a kind, an owner and a way to close;
- the loop reads the open items back, closes the ones it resolves, and stops
  or repairs before it builds on a serious defect.

The evidence comes from one long run, on the `parley` repository. The remedies
are general: they hold for any project and any language the format supports.

## Goals

- **Fewer full-suite runs.** A package's run executes the full test suite
  (`all_tests`) at most three times: after the integration task, after the
  conformance fixes when there are any, and on the merged tree. A tree whose
  gate already passed, with the same commands and environment, is not
  checked again. Between tasks, the gate runs the linter and the tests the
  task's change can affect.
- **Measured gate cost.** The report and the merge commit record how long each
  full-suite run took, and its slowest units where the tool reports them. A
  gate over the project's budget is a finding.
- **Classified open items.** Every open item is filed with a kind (`awaits`,
  `defect`, `untested`, `deviation`), the package that opened it, and, for
  `awaits`, the package that must close it. A `defect` also carries a
  severity.
- **No dead handoffs.** An `awaits` item never names a package that has
  already landed.
- **Open items are read back.**
  - A package's survey lists the open items that await it, and the open
    defects in the files it touches.
  - Its landing closes the items it resolved, each with the commit as
    evidence.
  - Items that await it but are still open after its landing are passed on
    or reclassified, never left pointing at a package that is done.
- **Defects stop or repair the loop.** The runner stops, or runs a repair,
  after a package opens a defect at or above a configured severity. Every few
  packages, and at the end, it reconciles every open item against the base
  branch.
- **Cross-package wiring is declared.** A package that leaves an extension
  point for another package to fill declares it as a handoff. `speca` refuses
  a handoff to a package that lands earlier, and warns when the receiving
  package does not take it.
- **Observable runs.** The runner keeps the full session log of every package
  and a ledger of durations, so a slowdown shows up before it reaches an hour.

## Non-goals

- **Fixing a project's test suite.** This plan gives the steering a test-cost
  directive, measures the cost and reports it. Making a given suite parallel
  or its fixtures cheap is that project's own work.
- **Weakening the test-first gate.** Every task's new tests are still seen
  failing first, and every task still lands only on a passing gate. Only the
  scope of the between-task gate changes.
- **Running packages in parallel.** The loop stays sequential in dependency
  order (`speca --next`).
- **Repairing defects without a person when they are about security.** A
  repair session may fix one, but the runner's default for `security` is to
  stop (decision 9).
- **Any other change to the v3 format.** The `handoffs` field (requirement 4)
  is the one format change, and it is optional.

## Background

### Where a package's time goes

The figures come from the session transcripts of 25 packages implemented by
`impl-specs.sh` on `parley`, a Go repository. Every package is about the same
size: 7 to 12 tasks and 46 to 70 criteria. Wall time still roughly
quadrupled:

| Package | Total | Time in the task gate | One gate run | Model time | Largest context |
|---|---|---|---|---|---|
| 05 | 28 min | 3 min | ~10 s | 8 min | 351k tokens |
| 07 | 22 min | 2 min | ~10 s | 8 min | 244k tokens |
| 15 | 68 min | 23 min | ~50 s | 24 min | 457k tokens |
| 20 | 104 min | 35 min | ~65 s | 33 min | 585k tokens |
| 24 | 109 min | 44 min | ~170 s | 23 min | 494k tokens |
| 25 | 85+ min | 40 min (`make check`) | ~160 s | 19 min | 551k tokens |

Split by phase (from commit times):

| Package | Survey | Tasks, total (per task) | Conformance | Landing |
|---|---|---|---|---|
| 07 | 6 min | 9 min (1.5 min) | 6 min | 1 min |
| 15 | 9 min | 42 min (4 min) | 15 min | 2 min |
| 24 | 13 min | 70 min (6.4 min) | 20 min | 5 min |

Three causes stand out.

**1. The gate runs the full suite after every task, and the full suite grows
with every package.**

The gate is `linter`, then `all_tests`. af-impl runs it at the baseline,
after each task, after the integration task, in the clean environment, after
the conformance fixes and on the merged tree. That is 14 to 16 runs for a
10-task package, and the developing agent also runs whole packages of tests
while it works.

The linter is cached and costs seconds; the tests are the cost. The suite's
wall time is close to that of its slowest test package. Several consecutive
packages extend the same areas, so the slowest package keeps growing. In
`parley`, the change area's tests took 226 s by package 24: 171 tests, one
after another, each building its own database and git fixtures in 0.2 to
1.9 s. No test in the repository runs in parallel. Nothing in the steering,
the skills or the conformance pass asks about test cost, so nothing noticed.

**2. Some gate runs repeat a tree that was already checked.**

- The baseline of package N+1 is the merged tree of package N, checked
  minutes earlier.
- When the base branch did not move during the run, the merged tree of a
  `--no-ff` merge is the branch tip's tree, checked by the last gate.

**3. The quality command was widened per task.**

The project instructions say "where a target such as `check` is more
thorough than the `test_commands`, run that target instead". When the
repository gained a `Makefile`, every task gate became `make check`: format,
tidy, vet, lint and tests, about 160 s each.

Model time grew too, from 8 to between 19 and 33 minutes. The session reads
more existing code at every step, and its context reached 550k to 650k
tokens. Each turn over a context that size is slower. The conformance review,
which runs in a fresh subagent, stayed between 4 and 10 minutes and is not
the problem.

### What happens to open items

Over 25 packages, af-impl opened 46 issues for open items. None was ever
closed:

- the runner never reads them;
- af-impl's survey never reads them;
- af-impl's rules forbid it to "close, edit or comment on an issue this run
  did not open";
- no commit references an issue.

Checked against the base branch after package 24, they fall into five kinds:

| Kind | Count | What it means |
|---|---|---|
| Resolved, still open | 3 in full, 15 in part | A later package added the missing caller; nobody closed the issue. |
| Awaits a later package | 10 | An extension point with no production caller yet. |
| Orphaned | 2 issues, plus parts of 4 others | The package meant to wire it landed without doing so; no later package mentions it. |
| Defect | 13 | Delivered code that is wrong: 1 security, 2 data loss, 3 correctness, the rest minor. No later package addresses any of them. |
| Untested | 3 | A criterion with no test. |

This does damage in four ways.

- **Defects persist.** Each run's scope rules ("outside this spec") stop it
  from touching an earlier package's code. A later package that meets a
  defect works around it, sometimes with an erratum, and leaves it in place.
- **Code gets copied instead of wired.** Two checks one package exported
  were never called. Four later packages each rewrote the same logic. The
  issue that tracks them can no longer be closed as written, and the four
  copies can drift apart.
- **Handoffs point the wrong way.** A package's `tasks.json` says "package 24
  registers its landing entry point, package 16 its fork activity hook". Both
  landed before it, so neither could register into a list that did not exist
  yet. On the base branch nothing would ever wire them. Another package does
  the same to a hook of package 16, which is then unwired for good.
  Dependencies in the format point only backwards, and a handoff forwards
  exists only as prose that no tool checks.
- **The list stops working as a signal.** With about 40 % of it out of date,
  the security and data-loss items are buried among "no production caller
  yet".

### The integration task and the unmet list disagree

`test-first.md` says that in the integration task, "a path that is not live
in production code fails this task, and a recorded decision or a deferral does
not satisfy it". Yet packages legitimately offer extension points that only a
later package fills. In practice the run lands anyway, through the unmet list,
with an issue that says "lacks production callers". The rule is stricter than
the format allows, so it gets bypassed instead of followed. A spec has no way
to say "this part is unwired on purpose, and package N wires it".

### The runner keeps little

`impl-specs.sh` saves only the session's final message, about 4 KB per
package. Durations, gate results and what the session did are only in Claude's
own transcripts, outside the project. The installed af-impl skill also differed
from the source in `skills/af-impl` (`speca --list` against `speca --next`),
and nothing in the log said which version ran.

## Requirements

### 1. The gate scales with the change

1.1 **Two gate levels.**

- The **task gate** runs after every implement task: `linter`, then the
  tests the task's change can affect.
- The **full gate** runs `linter`, then `all_tests`:
  - at the baseline (unless 1.3 reuses a result);
  - after the integration task;
  - after the conformance fix commit, when there is one;
  - on the merged tree.

The clean-environment run of the conformance pass stays, as a full gate in
the clean environment. The verdict table of af-impl §5 applies to both
levels.

1.2 **The affected-tests command is the project's.**

`tasks.json` `test_commands` gets an optional `affected_tests`: a command that
takes the base commit as its one argument and runs the tests the diff from
that commit can affect. Examples are a Makefile target such as
`make test-affected BASE=<sha>`, or a script. The project decides what
"affected" means: reverse dependencies, a test-impact tool, or a directory
map.

When `affected_tests` is absent, the task gate runs `all_tests`, as today.
`spec` and af-spec write `affected_tests` only when the project already has
such a command. The af-spec survey looks for one.

1.3 **A tree that passed is not checked again.**

af-impl records every gate result in a file outside the repository, under the
user's state directory, keyed by four things:

- the tree id (`git rev-parse HEAD^{tree}`, or `git write-tree` for an
  uncommitted merge);
- the gate level;
- the exact commands;
- whether the environment was clean.

A gate whose key matches a recorded passing result reuses it. The report
says so: "reused the gate of `<commit>`, `<date>`". A failing result is never
reused.

1.4 **The gate runs the spec's commands.**

The task and full gates run the commands in `test_commands`. A more thorough
repository target, such as `make check`, runs once, on the merged tree,
before the merge is committed. af-impl's text says this order takes
precedence over a project instruction to substitute the thorough target for
every check. The project template's "Quality Commands" section is reworded to
match: "the thorough target before committing a merge, the spec's commands
between tasks".

1.5 **Gate cost is measured and reported.**

Every gate run records its duration. Where the tool reports units (Go
packages, pytest files, Jest suites), af-impl also keeps the five slowest
units of the full gate on the merged tree. The merge commit and the report
hold:

- the full gate's duration at the baseline and on the merged tree;
- those slowest units;
- the number of full and task gate runs, and how many were reused.

1.6 **A gate budget.**

The steering may set `gate_budget_seconds` for the full gate, default 120.
When the full gate on the merged tree exceeds the budget, or grew by more
than half over this branch, the conformance pass records a `test-cost`
finding. It is filed as a `defect` of severity `minor` (requirement 3), so it
reaches someone instead of disappearing into a commit message.

1.7 **A test-cost directive in the steering template.**

`docs/templates/steering.md` gains a language-neutral "Test Suite Cost"
section:

- tests are independent and parallel-safe unless they say why not (in Go
  `t.Parallel()`, in Python `pytest-xdist`-safe);
- an expensive fixture (a migrated database, a repository, a key pair) is
  built once per process and copied or cloned per test, not rebuilt;
- the full suite stays within the gate budget.

The conformance "Tests" checklist adds: "a new test that runs serially
without a stated reason, or rebuilds a fixture the shared helpers already
build once".

### 2. The session's context stays bounded

2.1 **An unattended mode with a fresh context per task.**

af-impl accepts the argument `--unattended`. In this mode each implement task
runs in a subagent. The subagent receives:

- the survey brief;
- the task's scoped render;
- the files the task touches;
- the progress lines so far.

It returns the task's progress line, the commit, the gate verdict and the
criterion-to-test map. The orchestrating session claims, records and commits
as today. The commit discipline and the state machine do not change, and only
where the work happens moves.

Without the argument, af-impl runs as today, for a person watching. The
runner passes `--unattended`.

2.2 **The brief is the hand-off.**

In unattended mode the survey brief is written to a file outside the
repository and stays under a size limit: 600 lines. A subagent reads the
brief, not the orchestrator's transcript. What a later task needs to know
from an earlier one goes into that task's commit body, where af-impl already
puts it.

### 3. Every open item has a kind, an owner and a way to close

3.1 **Kinds.** The conformance pass assigns each entry of the `unmet` list
exactly one kind:

- `awaits`: the branch offers something that a named later package must use
  or wire. It is not a fault of this package.
- `defect`: code this branch delivered behaves differently from what its
  spec asks, or loses data, races, or exposes something. It carries a
  severity: `security`, `data-loss`, `correctness` or `minor`.
- `untested`: the behaviour is believed to work, but a criterion has no test
  that asserts it.
- `deviation`: the code deliberately does something other than the spec,
  recorded in an ADR, and a person must accept it or change the spec.

3.2 **Labels.** The issue for an open item carries these labels, created on
first use with `issue --create-label`:

- `af:<kind>`;
- `af:from-<NN>`, the package that opened it;
- for `awaits`, `af:awaits-<NN>`;
- for `defect`, `af:severity-<s>`.

The issue body's "What closes it" section stays, and for `awaits` it names
the receiving package by id.

3.3 **No `awaits` toward a landed package.**

Before filing an `awaits` item, af-impl asks `speca <NN>` about the receiving
package. If that package is sealed, or its last task is `done`, the item is
not `awaits`. It is a `defect` of severity `correctness`, with the note
"handoff to a package that has already landed". This is the wiring nobody
owns, and it is filed as such.

The same check runs when the survey reads a spec's prose about wiring
("package N registers …"). Prose that hands work to a landed package is
recorded as a survey decision. The decision is either "this package wires
it" or "filed as a defect".

3.4 **The survey reads the open items back.**

The survey lists, with `issue --list --state open`:

- every item labelled `af:awaits-<this package>`;
- every open `af:defect` and `af:untested` item whose evidence names a file
  that this package's tasks touch.

The brief gets a section "Open items for this package". Each `awaits` item is
either mapped to the task that will satisfy it, or recorded as a numbered
decision with the reason it will not be. Each defect in the touched area is
listed as a known fault, so the plan does not build on it unknowingly or copy
the code around it. An exported entry point that an `awaits` item names is
used, not re-implemented.

3.5 **Landing closes what the branch resolved.**

Before the merge, for each item listed in 3.4, af-impl decides whether the
branch closes it, with evidence: the production caller (`file:line`) and the
test that exercises it. After the merge is pushed, it closes each resolved
item with `issue <ref> --close --body`. The comment names the merge commit,
the evidence and this package.

af-impl's "Never" rule changes accordingly. af-impl may comment on and close
an issue labelled `af:*` whose closing condition this branch meets. It still
never closes, edits or comments on any other issue.

3.6 **Nothing is left awaiting a package that is done.**

An `af:awaits-<this package>` item that the branch did not resolve is, after
the merge:

- relabelled to the later package that now owns it, with a comment saying
  why; or
- relabelled `af:defect` with severity `correctness`, with a comment saying
  that no package owns it.

When this package's landing ends, no open issue carries
`af:awaits-<this package>`.

3.7 **The report and the merge commit count items by kind.**

They say how many open items the package opened, closed, passed on and
reclassified, per kind and severity, with the issue links.

### 4. Handoffs between packages are declared and checked

4.1 **A `handoffs` field.**

`requirements.json` gets an optional `handoffs` array. Each entry names:

- `what`: the extension point or entry point that is left for another
  package (`internal/land` after-landing hooks, `change.Stacks.OnLanded`);
- `to`: the receiving package's id;
- `criteria`: the criteria of this package that depend on the handoff being
  wired.

af-spec and the `spec` generator write a handoff wherever the PRD says that
another package will call, register or fill something this package offers.

4.2 **A handoff goes to a package that lands later.**

`speca --validate` refuses a package with a handoff whose `to` is unknown,
is the package itself, or does not depend, directly or transitively, on this
package. In dependency order such a package can land first, and then nothing
is left to wire it. This is the one rule that would have caught every
wrong-direction handoff seen so far.

4.3 **The receiving package takes the handoff.**

A task may list the handoffs it takes, in `takes_handoffs` (`"24:H-1"`).
`speca --validate` on the receiving package warns, without failing, for each
inbound handoff (found by scanning the other packages under the spec root)
that no task takes. The warning names the sending package and the handoff.
`specr <package> --task <N>` renders a task's handoffs with the sending
package's text, so the coder sees what to wire.

4.4 **The integration task tells a declared handoff from a gap.**

The integration task's rule "a path that is not live in production code
fails this task" gets one exception: a step that is unwired only because a
declared outbound handoff is not yet taken. Such a step:

- does not fail the task;
- is filed automatically as `awaits`, labelled with the handoff's `to`;
- has the criteria it lists covered by a test that calls the entry point
  directly, as today.

Every other path that is not live still fails the task, as today.

4.5 **The integration task of the receiver verifies inbound handoffs.**

The `## Contracts` section of the receiving package's integration render
lists each inbound handoff. The integration task confirms that production
code wires each one, by `file:line`. A handoff the package takes but does not
wire fails the task.

### 5. The runner stops, repairs and reconciles

5.1 **Stop on serious defects.**

After each package lands, the runner lists open issues labelled
`af:from-<NN>` and `af:defect`. If any carries a severity listed in `STOP_ON`
(default `security,data-loss`), the runner stops and names them. The next
package is not started on top of them.

5.2 **Repair as its own unit of work.**

With `REPAIR=1`, the runner runs `/af-impl --repair <issue>` instead of
stopping, for each such defect, in severity order.

A repair:

- branches `fix/<issue>-<slug>` from the base branch;
- writes a regression test that fails on the base branch, and fixes the
  code;
- passes the full gate and, when the issue's package has `test_commands`,
  that package's commands;
- merges `--no-ff` and closes the issue with the merge commit as evidence.

A repair touches only what the fix needs. It is not a package task and moves
no task state. It may edit a landed package's code, because the defect is its
scope. If the repair cannot fix the defect in two attempts, it leaves the
issue open with a comment and the runner stops. `security` defects are
repaired only when `REPAIR_SECURITY=1` is also set.

5.3 **Reconcile periodically.**

Every `RECONCILE_EVERY` packages (default 5), and once more when every
package is done, the runner runs `/af-impl --reconcile`. That session checks
every open `af:*` issue against the base branch:

- it closes those whose closing condition holds, with evidence;
- it relabels an `awaits` whose receiver has landed (3.6);
- it lists the rest by kind and severity.

It changes no code. Its report goes to the runner's log directory.

5.4 **Full logs and a ledger.**

- The runner saves each session's full stream
  (`claude -p … --output-format stream-json`) as `<package>.jsonl` beside the
  final message in `<package>.log`.
- It appends one line per package to `ledger.tsv`, with these columns:
  - the package;
  - start and end time;
  - wall minutes;
  - landed or stopped;
  - full gate runs and seconds, reused gates, task gate runs and seconds
    (read from the merge commit's trailer, see 5.5);
  - open items opened, closed and passed on.
- After each package it prints that line. When a package's wall time is more
  than twice the median of the packages before it, it also prints a warning.

5.5 **A machine-readable trailer.**

The merge commit ends with an `AF-Run:` trailer: one line of JSON with the
counts of 1.5 and 3.7. The runner and the ledger read it rather than parsing
prose.

5.6 **The runner says what it runs.**

At start, the runner logs:

- the af-impl skill's version, or the hash of its `SKILL.md`, and its path;
- the versions of `speca`, `specr` and `issue`.

When the installed skill's hash differs from the source in the `spec`
repository, where that is known through `AF_SKILLS_SRC`, the runner warns.

## Design Decisions

1. **Narrow the between-task gate, not the end gates.** The full suite still
   runs before anything lands, on the exact tree that lands. A regression a
   task causes outside the affected set is caught at the latest after the
   integration task, within the same branch, before any merge. The cost of
   finding it later is one failed task attempt, compared with 10 to 14 full
   runs saved per package.
2. **"Affected" is the project's definition, behind one command.** Test
   impact is ecosystem-specific: Go reverse dependencies, pytest test
   selection, Nx or Turborepo graphs. The format names the command and its
   argument, and leaves the meaning to the project. Without the command,
   nothing changes.
3. **Reuse a gate by tree id, never by commit.** Two commits with the same
   tree are the same code, whatever their messages. The key includes the
   commands and the environment, so a clean-environment run is never
   replaced by a normal one. Only passes are cached: a failure is cheap to
   run again and must be seen.
4. **The thorough repository target runs once, before the merge commit.**
   Formatting and tidiness cannot change a test's result, and the gate on
   the merged tree is the one that decides what lands. Running `make check`
   per task paid for format, tidy and vet 14 times to learn the same thing
   once.
5. **Bound the context by task, in unattended mode only.** A person watching
   wants one continuous session. An unattended run wants each turn cheap. The
   brief and the commit bodies already carry what a task needs, because
   af-impl writes them for a session resumed after a gap.
6. **Classify at creation, by the run that knows.** The conformance reviewer
   has the evidence when it builds the unmet list. A later classification
   would have to rebuild it from prose. Labels let the runner and the next
   survey filter without reading bodies.
7. **The run that resolves an item closes it.** Only the run that wired the
   caller can cite the caller. A periodic reconcile catches what slips
   through, but it should not be the main way issues close. The "Never"
   rule exists to keep af-impl away from a person's issues, and the `af:`
   label marks the ones that are its own.
8. **A handoff is format data, checked by `speca`.** The wrong-direction
   handoffs were all in prose, and no tool reads prose. The check that
   matters, "the receiver lands later", is a graph question `speca` already
   answers for `--next`. An `x-` key would carry the data, but the format
   gives `x-` keys no meaning, so the validator could not check them.
9. **Repairs are separate merges, and `security` stops by default.** A
   repair edits code another package landed. Doing it inside a package's
   branch would break that package's scope and its traceability. A security
   fix is the kind a person should see before the loop builds further on
   it.
10. **Data-loss defects stop the runner by default, correctness defects do
    not.** A correctness gap is usually local, and the next package rarely
    depends on it. A lost write or a leaked object is the kind of defect a
    later package silently builds on.

## Sequencing

Three steps. Each lands on its own and is useful without the next.

1. **Cheap and largest gain.**
   - Gate levels, the reused-gate cache and the gate's commands (1.1, 1.3,
     1.4), first without `affected_tests`: the task gate is then the full
     suite, but baselines and identical merged trees are not run again.
   - Gate measurement (1.5).
   - Runner logs, ledger and versions (5.4, 5.6).
   - Kinds and labels (3.1, 3.2, 3.7).
   - Closing what the branch resolved (3.5).
2. **The loop reads back.**
   - The no-dead-handoffs check (3.3), the survey reading open items (3.4)
     and passing on what remains (3.6).
   - Stop, repair and reconcile (5.1–5.3) and the trailer (5.5).
   - `affected_tests` (1.2), the budget (1.6) and the steering directive
     (1.7).
3. **Format and context.**
   - Handoffs (4.1–4.5), with a format revision and the af-spec and `spec`
     generator changes.
   - Unattended mode (2.1, 2.2).

## Dependencies

- `speca --next` and the dependency graph (`feature/speca-next`), for 3.3 and
  4.2.
- `issue` from `aflib`, which already supports `--list --label`,
  `--create-label`, `--edit --add-label/--remove-label`, `--comment` and
  `--close`.
- The v3 format (PRD 06, ADR 19). `handoffs`, `takes_handoffs` and
  `test_commands.affected_tests` are additions to its schemas.
- The steering template `docs/templates/steering.md` and the project
  template's "Quality Commands" section.
- `claude -p --output-format stream-json`, for 5.4.

## Open Questions

1. Should `affected_tests` take the base commit, or the list of changed
   paths? A commit is one argument and lets the tool compute paths itself.
   A list of paths suits tools that do not read git.
2. Should 4.3, "an inbound handoff no task takes", be an error rather than a
   warning once every package in a spec root declares its handoffs? A
   warning lets old packages validate.
3. Who accepts a `deviation`? It needs a person by definition. Should the
   runner stop on it as it does on `security`, or only list it in the
   ledger?
4. Should a repair also check, before it is attempted, that the defect still
   reproduces on the base branch? This applies when a later package may have
   fixed it in passing.
