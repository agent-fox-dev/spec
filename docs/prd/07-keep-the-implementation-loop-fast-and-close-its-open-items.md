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

This plan replaces the runner script with a Go command, `specrun`, built in
this repository on `afspec` and `aflib`. It also changes the af-impl skill,
the af-spec skill, the steering template and `speca`:

- the loop's cost grows with the size of a change, not the size of the
  repository;
- every open item has a kind, an owner and a way to close;
- the loop reads the open items back, closes the ones it resolves, and stops
  or repairs before it builds on a serious defect;
- the work that needs no judgement is done by a program, not by a model:
  running gates, keeping labels consistent, choosing what to repair, and
  scheduling.

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
- **The gate is a command, not instructions.** `speca --gate` runs the gate,
  reuses passing results, measures the run and checks the budget, and it
  returns one JSON verdict. The model calls one command and does none of the
  bookkeeping.
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
- **Every open item has a path to closed.**
  - The driver stops, or runs a repair, after a package opens a defect at or
    above a configured severity.
  - Every few packages, and at the end, it reconciles every open item
    against the base branch.
  - The other defects and the untested criteria are repaired in batches at
    each reconcile.
  - A person settles a deviation with one label, and the next reconcile acts
    on it.
  - Issues opened before this change are adopted into the same lifecycle.
- **Cross-package wiring is declared.** A package that leaves an extension
  point for another package to fill declares it as a handoff. `speca` refuses
  a handoff to a package that lands earlier, and warns when the receiving
  package does not take it.
- **A driver that can be tested and resumed.**
  - `specrun` replaces `scripts/impl-specs.sh`.
  - Its decisions are covered by tests that need no model and no forge.
  - It resumes after a crash where it stopped.
  - It kills a session that has stopped making progress.
- **Observable runs.** The driver keeps the full session log of every package
  and a ledger of durations, so a slowdown shows up before it reaches an hour.

## Non-goals

- **A recipe for fixing a project's test suite.** This plan gives the
  steering a test-cost directive, measures the cost and files a finding when
  the suite grows past its budget. That finding is repaired like any minor
  defect. How a given suite is made parallel, or its fixtures cheap, is the
  project's own decision, made in that repair or by a person.
- **Weakening the test-first gate.** Every task's new tests are still seen
  failing first, and every task still lands only on a passing gate. Only the
  scope of the between-task gate changes.
- **Running packages in parallel.** The loop stays sequential in dependency
  order.
- **Another model runtime.** Every piece of work that needs a model runs in a
  Claude Code session started with `claude -p`, with the skills of this
  repository. `specrun` starts, watches and checks those sessions; it never
  calls a model API itself.
- **Dependencies beyond this module and `aflib`.** `specrun` is built from
  the spec module's own packages, `aflib` and the Go standard library. The
  module's `agentkit-go` dependency, which the `spec` generator uses, is not
  one of them (5.1).
- **Repairing security defects without a person.** A repair session may fix
  one, but the default for `security` is to stop (decision 9).
- **Any other change to the v3 format.** `handoffs`, `takes_handoffs` and
  `test_commands.affected_tests` are the format's only additions, and all
  three are optional.

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

### The runner is a shell script

`impl-specs.sh` is about 100 lines of bash: a loop over `speca --next`, a
`claude -p` call, and four checks that the package landed. That is the right
size for bash. What this plan adds is not:

- stop and repair decisions;
- a repair queue with an order and a cap;
- a reconcile every few packages;
- label rules;
- a ledger, and parsing a trailer;
- version checks;
- resuming after a crash.

Together these are a state machine. In bash its decisions could only be
tested by a real run of several hours, and every JSON answer would go
through `jq`.

The script also keeps little. It saves only the session's final message,
about 4 KB per package. Durations, gate results and what the session did are
only in Claude's own transcripts, outside the project. The installed af-impl
skill differed from the source in `skills/af-impl` (`speca --list` against
`speca --next`), and nothing in the log said which version ran.

Much of what the loop needs is already Go in this repository and in `aflib`:

- `afspec` answers the dependency graph, the next package and a package's
  state;
- `aflib/issuex` lists, labels, comments on and closes issues on GitHub and
  GitLab, and has a no-op client for tests;
- `aflib/envelope` holds the JSON answer the `issue` command writes, as Go
  types.

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
contains the placeholder `{base}` exactly once. Before running it,
`speca --gate` (1.8) replaces `{base}` with the id of the commit the task
started from, so the command runs the tests that the diff from that commit
can affect. An example is `make test-affected BASE={base}`. A command
without `{base}`, or with it more than once, fails validation. The project's
tool works out the changed paths itself, new and untracked files included.
The project decides what "affected" means: reverse dependencies, a
test-impact tool, or a directory map.

When `affected_tests` is absent, the task gate runs `all_tests`, as today.
`spec` and af-spec write `affected_tests` only when the project already has
such a command. The af-spec survey looks for one.

1.3 **A tree that passed is not checked again.**

`speca --gate` (1.8) records every gate result in a file outside the
repository, under the user's state directory, keyed by four things:

- the tree id of the working tree as it is, tracked and untracked files
  included (written through a temporary index, so the user's index is not
  touched);
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
every check. The project template's "Quality Commands" section
(`docs/templates/AGENTS.md`) is reworded to match: "the thorough target
before committing a merge, the spec's commands between tasks".

1.5 **Gate cost is measured and reported.**

`speca --gate` (1.8) records the duration of every gate run. Where it
recognises the tool's output (Go packages first; pytest files and Jest suites
as later additions), it also keeps the five slowest units of each full gate.
For an output it does not recognise, the list is empty. The merge commit and
the report hold, from `speca <package> --gate-report`:

- the full gate's duration at the baseline and on the merged tree;
- those slowest units;
- the number of full and task gate runs, and how many were reused.

1.6 **A gate budget.**

The steering may set `gate_budget_seconds` for the full gate, default 120.
`speca --gate` compares each full gate with the budget, and with the full
gate recorded for the base branch's tree. Its verdict says whether the run
exceeded the budget, and by how much it grew. When the full gate on the
merged tree exceeds the budget, or grew by more than half over this branch,
the conformance pass records a `test-cost` finding. It is filed as a
`defect` of severity `minor` (requirement 3), so it reaches someone instead
of disappearing into a commit message. The repair queue (5.3) then picks it
up like any other minor defect.

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

1.8 **`speca` runs the gate.**

`speca <package> --gate task|full [--clean-env] [--base <commit>]` runs one
gate of the package and answers in speca's JSON envelope.

- **The commands.**
  - `full` runs `linter`, then `all_tests`.
  - `task` runs `linter`, then `affected_tests` with `{base}` replaced by
    `--base` (default `HEAD`, the commit the uncommitted task work sits on).
    It falls back to `all_tests` when the package has no `affected_tests`.
  - Each command runs without a shell, as `test_commands` already requires,
    and stops at the first command that fails.
- **The clean environment.** `--clean-env` runs the commands with:
  - an empty temporary `HOME`;
  - no global or system git configuration;
  - `GIT_TERMINAL_PROMPT=0`.

  This is the run the conformance pass asks for today.
- **Reuse.** Before running anything, it looks up the key of 1.3. A recorded
  pass is returned as the verdict, with `reused` naming the commit and time
  of the run it came from.
- **Output.** Each command's full output goes to a log file outside the
  repository. The envelope names the file, and holds the last 40 lines of a
  failing command, so the model reads a failure without reading a log.
- **The verdict** (`result`):
  - `level`;
  - `verdict` (`pass`, `fail` or `not_run`);
  - `reused`;
  - per command: its name, exit code, seconds, and the path of its log;
  - total seconds;
  - the slowest units (1.5);
  - for a full gate, the budget, whether it was exceeded and the growth over
    the base branch (1.6).

  `not_run` means a command could not start: it is missing, or the
  package's `test_commands` are invalid.
- **What it writes.** It writes nothing into the repository or the package.
  It is not a task move and is allowed in any package status. The exit code
  follows speca's conventions: `0` when the gate ran, whatever its verdict.
  The verdict is in the JSON.
- **The report.** `speca <package> --gate-report [--since <commit>]` sums
  this package's recorded gate runs since the branch left the base branch:
  - the full and task runs, their seconds and how many were reused;
  - the full gate's duration at the baseline and on the last tree;
  - the slowest units of the last full gate.

  af-impl copies this report into the merge commit and its `AF-Run:`
  trailer (5.10).

af-impl compares the verdict with the baseline using the table of §5, as it
does today. Only the running and the bookkeeping move into `speca`. A repair
(5.5) runs the full gate through the same command.

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

Without the argument, af-impl runs as today, for a person watching.
`specrun` passes `--unattended`.

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

3.2 **Labels.** The issue for an open item carries these labels:

- `af:<kind>`;
- `af:from-<NN>`, the package that opened it;
- for `awaits`, `af:awaits-<NN>`;
- for `defect`, `af:severity-<s>`.

`specrun` creates every label of the scheme before its first session (5.3).
af-impl run on its own creates a missing label on first use, with
`issue --create-label`.

The issue body's "What closes it" section stays, and for `awaits` it names
the receiving package by id.

3.3 **No `awaits` toward a landed package.**

Before filing an `awaits` item, af-impl asks `speca <NN>` about the receiving
package. If that package is sealed, or its last task is `done`, the item is
not `awaits`. It is a `defect` of severity `correctness`, with the note
"handoff to a package that has already landed". This is the wiring nobody
owns, and it is filed as such. `specrun` checks the same rule again after
every landing (5.3).

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
an issue labelled `af:*` whose closing condition this branch meets. A
reconcile may also label the issues it adopts (5.6). af-impl still never
closes, edits or comments on any other issue.

3.6 **Nothing is left awaiting a package that is done.**

An `af:awaits-<this package>` item that the branch did not resolve is passed
on by af-impl after the merge, because choosing the new owner takes
judgement. It is either:

- relabelled to the later package that now owns it, with a comment saying
  why; or
- relabelled `af:defect` with severity `correctness`, with a comment saying
  that no package owns it.

`specrun` then enforces the result (5.3). Any open issue that still carries
`af:awaits-<this package>` when the landing ends is relabelled as a
`correctness` defect by `specrun`, with a comment that the session left it.

3.7 **The report and the merge commit count items by kind.**

They say how many open items the package opened, closed, passed on and
reclassified, per kind and severity, with the issue links.

3.8 **A person settles a deviation with a label.**

A deviation waits for a person. To settle it, the person adds one label to
the issue:

- `af:accepted`: the deviation stands. `specrun` closes the issue at the next
  reconcile, with a comment naming the ADR that the issue cites. The spec is
  not edited. The ADR and the closed issue are the record.
- `af:rejected`: the code must do what the spec says. `specrun` relabels the
  issue `af:defect` with severity `correctness` at the next reconcile, with a
  comment saying a person rejected the deviation. It is then repaired like
  any other defect (5.5).

A deviation with neither label stays open, and every reconcile report lists
it, oldest first.

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
`speca --validate` on the receiving package warns for each inbound handoff
(found by scanning the other packages under the spec root) that no task
takes. The warning names the sending package and the handoff. By default a
warning does not make the package invalid.

`speca --validate --strict-handoffs` turns each such warning into an error.
The flag stores nothing; the caller decides each time. CI, a person, af-spec
when it writes a package, or `specrun` (5.12) can each pass the flag.

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

### 5. `specrun` drives the loop

5.1 **A Go command in this repository.**

`cmd/specrun` builds `specrun`. It is built from:

- the spec module's own packages: `afspec` for the dependency graph, the next
  package, a package's state and its validation, in process, with no
  subprocess;
- `aflib/issuex` for every forge operation: list, label, comment, close and
  create labels, on GitHub and GitLab;
- `aflib/envelope` for its JSON answer;
- the Go standard library.

It starts Claude Code sessions with `claude -p` for the work that needs a
model:

- `/af-impl <package> --unattended`;
- `/af-impl --reconcile`;
- `/af-impl --repair <issue>`.

It runs `git` for the repository. Its answer is one envelope on stdout when
it ends. Progress goes to stderr, one line per step.

`specrun` imports no package that depends on `agentkit-go`. The module's
packages built on it (`specgen`, `internal/agentrun`, `internal/cli`,
`internal/repomap`, `internal/skillgen`) are not used. What `specrun` needs
from them, such as flag parsing or envelope output, it gets from the
standard library and `aflib`, or from a package of this module that is free
of `agentkit-go`.

`make build` installs it with `spec`, `specr` and `speca`, and
`docs/cli.md` documents it.

5.2 **The loop.**

`specrun` runs one step at a time, until every package is done, a stop rule
fires (5.4), or `--max` packages have landed. Before every step it switches
to the base branch and pulls it with `--ff-only`. A step is one of:

- **implement the next package**: the package `afspec` names next in
  dependency order;
- **reconcile**: after every `--reconcile-every` landed packages, and once
  more when every package is done (5.6);
- **repair one issue**: from the repair queue (5.3, 5.5).

After an implement step, `specrun` checks that the package landed, as the
script does today:

- the base branch is checked out;
- the working tree is clean;
- `HEAD` moved;
- the package has no task `pending` or `in_progress`.

A step that fails these checks stops the loop. When no package can start,
because the open packages wait on something no run can finish or depend on
one another in a cycle, `specrun` reports why and stops.

5.3 **What `specrun` does without a model.**

These rules need no judgement, so `specrun` applies them itself, through
`issuex`, instead of asking a session to:

- **Labels.** Before the first step, it creates every label of the scheme
  (3.2, 3.8, and `af:repair-failed`) that the repository lacks.
- **After every landing:**
  - It relabels any open `af:awaits-<NN>` whose package `NN` is now done as a
    `correctness` defect, with a comment (3.3, 3.6).
  - It checks that every issue the session opened carries a kind, its
    origin and, for a defect, a severity. An issue that lacks one gets
    `af:deviation` and a comment, so a person decides it.
- **Deviations.** At each reconcile, it closes the `af:accepted` deviations
  and turns the `af:rejected` ones into defects (3.8).
- **The repair queue.** At each reconcile, it builds the queue from labels:
  - every open `af:defect` whose severity is in `--repair-on` (default
    `security,data-loss,correctness,minor`);
  - every open `af:untested` item, when `--repair-on` holds `untested` (it
    does by default).

  It leaves out every issue labelled `af:repair-failed`, and every
  `security` defect unless `--repair-security` is set. It orders the queue
  most severe first, then oldest first, and takes at most `--repair-max`
  (default 5) per reconcile.

Every change `specrun` makes to an issue carries a comment that says which
rule made it and on which commit of the base branch.

5.4 **Stop on serious defects.**

After each package lands, `specrun` lists the open `af:defect` issues
labelled `af:from-<NN>`. If any carries a severity in `--stop-on` (default
`security,data-loss`), it stops and names them, unless `--repair` is set
(5.5). The next package is not started on top of them.

A `deviation` never stops `specrun`. The ledger line and the merge commit
count it, and every reconcile report lists the open deviations, so a person
can settle them in a batch (3.8).

`--stop-on` decides only when `specrun` halts. What it repairs is decided
separately, by `--repair` and `--repair-on` (5.3, 5.5).

5.5 **Repair as its own unit of work.**

`specrun` runs a repair session at two moments:

- **Instead of stopping.** With `--repair`, after a package that opened
  defects whose severity is in `--stop-on`, it repairs each of them, most
  severe first, before the next package.
- **At every reconcile.** After the reconcile, it repairs the issues of the
  repair queue (5.3), one session each, in queue order. Issues not repaired
  this time stay for the next reconcile. This is how `correctness` and
  `minor` defects, `test-cost` findings and untested criteria get closed
  without stopping the loop.

A `security` defect is repaired only when `--repair-security` is also set,
whichever of the two moments it comes up at.

A repair of a defect:

- branches `fix/<issue>-<slug>` from the base branch;
- writes the regression test first and runs it on the unchanged code;
- when the test fails, fixes the code until the test passes;
- when the test passes on the unchanged code, does not fix anything: the
  defect is already gone, probably fixed in passing by a later package. The
  repair commits only the test, with a `test:` commit that says so, and
  closes the issue with that test as evidence;
- passes the full gate (`speca --gate full`) and, when the issue's package
  has `test_commands`, that package's commands;
- merges `--no-ff` and closes the issue with the merge commit as evidence.

A repair of an `untested` item is a test-only repair. It writes the test the
criterion is missing, on a `fix/<issue>-<slug>` branch, and runs it on the
unchanged code:

- when the test passes, the behaviour was right. The repair commits the test
  (`test:`), passes the full gate, merges and closes the issue with the test
  as evidence;
- when the test fails, the behaviour was wrong. The repair relabels the issue
  `af:defect` with severity `correctness`, with a comment holding the
  failure, and goes on as a repair of that defect, in the same session.

A repair touches only what the fix needs. It is not a package task and moves
no task state. It may edit a landed package's code, because the defect is its
scope.

A repair can fail in two ways:

- its test cannot reproduce the issue as it is described, for example
  because the issue names code that no longer exists, and the repair does
  not guess;
- it cannot fix the defect in two attempts.

Either way, the repair comments on the issue with what it tried, adds the
label `af:repair-failed` and leaves the issue open. `specrun` then checks
what the session left:

- it is back on a clean base branch;
- the issue is either closed or labelled `af:repair-failed`.

It stops when the issue's severity is in `--stop-on`. Otherwise it goes on,
and later queues leave the issue out until a person removes the label.

5.6 **Reconcile: the judgement part.**

A reconcile session (`/af-impl --reconcile`) does only what needs a model.
`specrun` does the rest before and after it (5.3).

**Adopting older issues.** An open issue with no `af:` label is adopted when
its title starts with `<NN_name>:` and `NN_name` is a package under the spec
root. That is the title af-impl has always given an open item. `specrun`
selects these issues by title and hands them to the session. The session
reads each body, which already says what is missing, why, the evidence and
what closes it, and assigns:

- its kind;
- `af:from-<NN>`;
- `af:awaits-<NN>` when it awaits a package;
- a severity when it is a defect.

It adds those labels with a comment saying the issue was adopted and why it
got that kind. An issue whose kind the body does not settle is labelled
`af:deviation`, so a person decides it (3.8). An issue whose title does not
match is never handed to the session. Adoption runs at every reconcile, so
issues opened by an older af-impl during a mixed run are picked up too.

**Checking closing conditions.** For every open `af:*` issue, the session
decides whether the condition in its "What closes it" holds on the base
branch. It closes those that hold, with evidence (`file:line`, a test). It
changes no code.

The session writes its report to the log directory as Markdown. The report
lists what it adopted, what it closed, and the rest by kind and severity,
deviations oldest first. `specrun` then applies 5.3 and builds the repair
queue from the labels as they now stand.

5.7 **State and resume.**

`specrun` keeps its state in one JSON file under the user's state directory,
named after the repository: the steps done, the step running with its start
time, the packages since the last reconcile and the repair queue. It writes
the file atomically after every change.

Started again after a crash, a kill or a reboot, it reads the file and
resumes:

- A step recorded as running is checked against the repository first.
  - A package that landed counts as done.
  - A package branch that exists is continued, as af-impl already does with
    an existing branch.
  - A repair whose issue is closed counts as done.
- A step that cannot be checked is run again.

`specrun --reset-state` discards the file.

5.8 **A session that stops making progress is ended.**

`specrun` reads each session's `stream-json` output as it arrives. When no
event arrives for `--stall` (default 30 minutes), it ends the session,
records the step as stalled in the ledger and stops the loop. The timeout of
a single gate stays below the stall time, so a long gate is not mistaken for
a stall.

5.9 **Full logs and a ledger.**

- `specrun` saves each session's full stream as `<step>.jsonl` in the log
  directory (`--log-dir`, default under the user's cache directory), beside
  the final message in `<step>.log`.
- It appends one JSON line per step to `ledger.jsonl`. Each line holds:
  - the step's kind and subject (a package, a reconcile, a repair's issue);
  - start and end time, and wall minutes;
  - the outcome: landed, stopped, stalled; for a repair: fixed, already
    fixed, failed;
  - for a package: the full gate runs and seconds, the reused gates, and
    the task gate runs and seconds, read from the merge commit's trailer
    (5.10);
  - the open items opened, closed and passed on.
- After each step it prints the line in short form. When a package's wall
  time is more than twice the median of the packages before it, it also
  prints a warning.

`specrun --ledger` prints the ledger as a table.

5.10 **A machine-readable trailer.**

The merge commit ends with an `AF-Run:` trailer: one line of JSON with the
counts of 1.5 and 3.7. `specrun` and the ledger read it, rather than parsing
prose.

5.11 **`specrun` says what it runs.**

At start, `specrun` logs:

- its own version;
- the af-impl skill it will run: its path and the hash of its `SKILL.md`;
- the versions of `claude` and `issue`.

`specrun` is built from the same repository as the skills. It embeds the
hash of the af-impl skill it was built with. When the installed skill's hash
differs, it warns, and with `--strict-skills` it refuses to start. That is
the mismatch the background describes: the installed skill was older than
the source.

5.12 **Configuration and a dry run.**

Every setting is a flag:

- `--max`, `--stop-on`, `--repair`, `--repair-on`, `--repair-max`,
  `--repair-security`, `--reconcile-every` (default 5), `--stall`,
  `--log-dir`, `--strict-skills`;
- `--strict-handoffs`, which `specrun` passes to every validation it runs.

An environment variable of the same name in upper case, prefixed `SPECRUN_`,
sets a flag's default.

`specrun --dry-run` changes nothing and starts no session. It prints:

- the next step;
- the package `afspec` would choose, or why none can start;
- the repair queue as the labels now stand;
- what 5.3 would change.

5.13 **The driver is tested without a model or a forge.**

`specrun`'s decisions are tested by table-driven tests that run with:

- a fake `claude`: a test binary that replays a recorded stream and changes
  the repository as a session would;
- the no-op or an in-memory `issuex` client;
- a fixture repository.

The tests cover:

- choosing the step;
- the landing checks;
- every rule of 5.3;
- the stop rules;
- the queue order and cap;
- resuming from every state of 5.7;
- the stall timeout.

5.14 **The script is retired.**

`scripts/impl-specs.sh` stays as it is until `specrun` does everything it
does: the loop, the landing checks, the stop on a package that did not land.
In the change that reaches that point, the script is deleted, and the docs
and the README name `specrun` instead.

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
   would have to rebuild it from prose. Labels let `specrun` and the next
   survey filter without reading bodies.
7. **The run that resolves an item closes it.** Only the run that wired the
   caller can cite the caller. A periodic reconcile catches what slips
   through, but it should not be the main way issues close. The "Never"
   rule exists to keep af-impl away from a person's issues, and the `af:`
   label marks the ones that are its own.
8. **A handoff is format data, checked by `speca`.** The wrong-direction
   handoffs were all in prose, and no tool reads prose. The check that
   matters, "the receiver lands later", is a graph question `afspec`
   already answers for `--next`. An `x-` key would carry the data, but the
   format gives `x-` keys no meaning, so the validator could not check them.
9. **Repairs are separate merges, and `security` stops by default.** A
   repair edits code another package landed. Doing it inside a package's
   branch would break that package's scope and its traceability. A security
   fix is the kind a person should see before the loop builds further on
   it.
10. **Data-loss defects stop the loop by default, correctness defects do
    not.** A correctness gap is usually local, and the next package rarely
    depends on it. A lost write or a leaked object is the kind of defect a
    later package silently builds on.
11. **`affected_tests` takes the base commit, as `{base}`.** One argument
    that cannot get long. The project's tool computes the changed paths from
    git, so it handles new, untracked, deleted and renamed files in one place
    instead of af-impl deciding what counts as changed. A placeholder rather
    than an appended argument lets the commit go wherever the command needs
    it (`BASE={base}`, `--since {base}`).
12. **An untaken handoff is a warning, and an error only on request.** Spec
    roots written before handoffs existed must still validate. A flag rather
    than a stored setting keeps the format free of a new configuration
    place: whoever needs the strict answer (CI, a gate before activation)
    asks for it.
13. **Deviations are listed, not stopped on.** A deviation is decided,
    recorded in an ADR, and leaves the code consistent. It needs a person's
    acceptance, not an immediate one, and stopping on each would halt the
    loop after most packages. A person reviews them in batches from the
    reconcile report.
14. **A repair proves the defect before it fixes it.** Writing the
    regression test first is the test-first gate applied to a repair. The
    same run also answers whether a later package already fixed the defect.
    A test that passes on the unchanged code is kept as the evidence, and
    nothing else changes.
15. **Defaults: reconcile every 5 packages, a 120-second budget.** In the
    measured run, five packages took 4 to 6 hours, so a reconcile every 5
    keeps the issue list at most a few hours out of date. The full suite
    crossed 120 s around the fifteenth package, when the slowdown first
    showed in wall time. Both can be set by the project; neither is
    switched off by default.
16. **The gate is a `speca` command, not instructions to the model.**
    Recording results by tree, timing runs, parsing per-unit times,
    comparing with a budget and summing runs for the trailer are all
    mechanical. Written as skill instructions, the model would run
    `git write-tree`, time commands and edit a state file by hand on every
    gate. That is fragile, differs from run to run, and adds model turns to
    the path this plan is trying to make cheaper. `speca` already reads
    `test_commands`, and it is the command af-impl calls for every other
    mechanical step. One call per gate, with a JSON verdict, also gives
    interactive runs, unattended runs and repairs the same gate.
17. **Stopping and repairing are configured separately.** What is dangerous
    to build on (`--stop-on`) and what the loop may fix by itself
    (`--repair-on`) are different questions. Tying them together left every
    `correctness` and `minor` defect open forever, which is the backlog this
    plan exists to end. Batching repairs at reconciles, with a cap, keeps the
    time they take bounded and visible in the ledger.
18. **An untested criterion is closed by a test.** The cheapest way to
    settle "believed to work" is to write the missing test. Either it
    passes, and the issue closes with the test as evidence, or it fails,
    and the item was a defect all along and is repaired as one in the same
    session.
19. **A person settles a deviation where it is tracked, with a label.**
    Labels are the one thing `specrun`, the sessions and the forge's own
    filters all read. Accepting needs no spec edit, because the ADR already
    records the rule the code follows. Rejecting turns the item into an
    ordinary defect, so the repair path needs nothing new.
20. **Older issues are adopted by their title, and only by their title.**
    af-impl has always titled an open item `<NN_name>: <what is missing>`,
    and its body already holds what a classification needs. Matching the
    title against the packages in the spec root keeps the reconcile away
    from every issue a person opened. When the body does not settle the
    kind, `deviation` puts the decision in front of a person instead of
    guessing.
21. **The driver is Go, in this repository.** The loop becomes a state
    machine with rules that must hold across hours of unattended work.
    - In Go they are tested in seconds against a fake session and a fake
      forge; in bash only a real run tests them.
    - The facts the loop decides on (the dependency graph, the next package,
      a package's state) come from `afspec` in process, the same code
      `speca` uses, rather than from parsing `speca`'s output.
    - The forge comes through `issuex`, the same code the `issue` command
      the sessions use is built on.

    It lives here because it is built with the format, the skills and
    `speca`. The hash of the skill it was built with (5.11) is only
    meaningful when the two come from one tree. It stays off `agentkit-go`,
    which the module carries for the `spec` generator, so that the driver
    has no model runtime of its own (decision 23).
22. **A model runs only where judgement is needed.** Choosing a step,
    checking a landing, keeping labels consistent, ordering the repair queue
    and settling labelled deviations follow from labels, the dependency
    graph and git. Each is a rule `specrun` applies the same way every
    time, at no model cost. A session is started only to write code, to
    classify an issue from its prose, or to decide whether a closing
    condition holds. This is decision 16 applied to the loop.
23. **Claude Code stays the model runtime, through `claude -p`.** The skills
    are written for Claude Code sessions: their subagents, the tools they
    expect and their run-to-the-end rules. `specrun` starts, watches and
    checks those sessions, which keeps one way of running a model rather
    than two.
24. **State is one JSON file, not a database.** `specrun` runs one step at a
    time in one process. What it must survive is a crash between steps or
    within one. The repository records most of the truth (branches, merges,
    task states, labels), and the file holds only the rest: the running
    step, the counter and the queue. A durable job queue, with retries,
    serialization and an SQLite store, would solve problems this loop does
    not have, at the price of a new dependency for the spec module.
25. **`specrun` checks its sessions and does not trust them.** A session's
    final message is the model's account of what happened. The landing
    checks, the label rules and the repair outcome checks read the
    repository and the forge instead, so a session that ends early,
    stalls or claims too much is caught by the same code every time.

## Sequencing

Three steps. Each lands on its own and is useful without the next.

1. **Cheap and largest gain.**
   - `speca --gate` and `--gate-report` (1.8), with the gate levels, the
     reused-gate cache and the gate's commands (1.1, 1.3, 1.4), first
     without `affected_tests`: the task gate is then the full suite, but
     baselines and identical merged trees are not run again.
   - Gate measurement (1.5).
   - `specrun` doing what the script does (5.1, 5.2), with state and resume
     (5.7), the stall timeout (5.8), logs and ledger (5.9), versions (5.11),
     configuration and dry run (5.12), and its tests (5.13). The script is
     retired at the end of this step (5.14).
   - Kinds and labels (3.1, 3.2, 3.7).
   - Closing what the branch resolved (3.5).
2. **The loop reads back.**
   - The no-dead-handoffs check (3.3), the survey reading open items (3.4)
     and passing on what remains (3.6).
   - `specrun`'s rules (5.3), stop and repair (5.4, 5.5), and reconcile
     with adoption (5.6).
   - Settling deviations by label (3.8) and the trailer (5.10).
   - `affected_tests` (1.2), the budget (1.6) and the steering directive
     (1.7).
3. **Format and context.**
   - Handoffs (4.1–4.5), with a format revision and the af-spec and `spec`
     generator changes.
   - Unattended mode (2.1, 2.2). It comes last on purpose: the ledger of
     step 1 measures model time per package after the gate changes. If the
     remaining model time no longer grows with the repository, unattended
     mode can be dropped from this plan.

## Dependencies

- `afspec`: the dependency graph and `speca --next` (`feature/speca-next`),
  for 3.3, 4.2 and 5.2; package state and validation for 5.2 and 5.12.
- `speca`'s JSON envelope and exit-code conventions (`docs/speca.md`), which
  `--gate` and `--gate-report` follow.
- `aflib`, a new dependency of the spec module:
  - `issuex` (`Client`: `ListIssues`, `ReadIssue`, `AddLabels`,
    `RemoveLabel`, `CreateLabel`, `AddComment`, `CloseIssue`, and the no-op
    client) for 5.3 to 5.6 and 5.13;
  - `envelope` for `specrun`'s answer.

  The module's own `internal/issuex` covers only reading an issue and its
  comments and adding a comment, which is what the `spec` CLI needs. It
  cannot list, label or close an issue. `specrun` uses `aflib/issuex`
  rather than growing a second copy of the same client. Moving the `spec`
  CLI onto `aflib/issuex` and deleting `internal/issuex` is a separate
  change.
- The `issue` command from `aflib`, which the sessions use. It already
  supports `--list --label`, `--create-label`,
  `--edit --add-label/--remove-label`, `--comment` and `--close`.
- The v3 format (PRD 06, ADR 19). `handoffs`, `takes_handoffs` and
  `test_commands.affected_tests` are additions to its schemas.
- The steering template `docs/templates/steering.md` and the "Quality
  Commands" section of the project template `docs/templates/AGENTS.md`.
- Claude Code's `claude -p`, with `--output-format stream-json`,
  `--permission-mode` and `--disallowedTools`, for 5.1, 5.8 and 5.9.
