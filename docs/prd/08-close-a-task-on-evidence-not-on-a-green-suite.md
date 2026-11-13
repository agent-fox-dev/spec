# Close a task on evidence, not on a green suite

## Intent

A task is recorded done when the gate passes, and the gate is the linter and
the whole test suite. So "done" says that nothing is broken; it does not say
that each criterion the task owns has a test, that the test ever failed
without the implementation, or that the criteria are still the ones the
package was activated with. That is where an implementation drifts from its
spec while it is being written: a criterion is left untested, a test asserts
less than its check, or the spec is edited to match the code. A task closes
on evidence instead: for every criterion it owns, a named test that `speca`
itself saw fail before the implementation and pass after it, recorded in
`progress.json`; and the contract files of an active package are frozen, as
its intent already is.

## Goals

- **The contract is frozen at activation.** `speca <package> --status active`
  records a hash of `requirements.json` and `tasks.json`. While either file
  differs from it, every task write and the sealing of the package are
  refused, as they are today for a drifted `## Intent`.
- **A criterion has a named test.** A task of such a package cannot be moved
  to `done` until every criterion it owns is mapped to at least one test,
  and `speca` ran each mapped test and saw it pass.
- **The test was seen failing first.** Each mapped test has a recorded run in
  which it failed, made by `speca` before the close, or a declared exemption
  with its reason. A test that could not be built or collected is not a
  failing test.
- **Any test runner.** A Go repository needs no configuration. Any other
  repository names one command in `tasks.json`, `evidence_tests`, that runs
  the tests it is given and prints one result line per test.
- **The evidence can be checked again.** `speca <package> --verify-evidence`
  runs every recorded test of every done task on the tree as it is and
  answers with one verdict, writing nothing.
- **Nothing in flight changes.** A package activated before this change
  carries no contract hash, and its tasks close as they do today.

## Non-goals

- **Judging whether a test asserts what its check says.** `speca` proves that
  a test exists, failed and passes; whether its assertions match
  `check.pseudocode` is a review by a fresh subagent, in a later PRD.
- **Proving the implementation was absent at the failing run.** `speca`
  records the tree the test failed on and the tree it passed on; it does not
  inspect what changed between them.
- **Detecting a new package that changes behaviour an archived one
  established** (existing tests edited or deleted). A later PRD.
- **Mutation testing**, per criterion or otherwise.
- **Editing the contract of an active package.** There is no call that
  accepts a changed contract. A package whose criteria must change is
  superseded by a new one; a divergence found while implementing stays a
  deviation recorded in an ADR, as `af-impl` does today.
- **Evidence for packages already active, and for dropped tasks.** A task
  moved to `dropped` needs none.
- **Requiring `--verify-evidence` to seal.** The caller decides when to run
  it; `af-impl` runs it on the merged tree.
- **Tying the gate's recorded passes to the evidence.** The gate and its
  reuse of a pass are unchanged.
- **Spec or criterion ids in test code.** The map lives in `progress.json`;
  the steering's rule on test names stands.
- **A new format version.** Every addition is optional in version 3.

## Background

`speca <package> --task N --state done` (`cmd/speca/task.go`) moves a task
through the state machine and writes `progress.json`, which holds one
`{id, state}` entry per task and `updated_at`
(`specification/schemas/progress.v3.json`). Nothing else is checked at the
close.

`speca <package> --gate task|full` (`19_speca_gate`, ADR 22) runs the
package's `linter` and `all_tests` commands from `test_commands` in
`tasks.json`, without a shell, each under `--timeout`, and records a pass
under the tree id of the working tree. Its output is the suite's own; it
cannot say which test covers which criterion.

`skills/af-impl/SKILL.md` asks the implementer to run a task's new tests and
watch them fail before any implementation file changes, to declare a
"test-first deviation" when a failing first run is impossible, and to list
every criterion against its test in the commit body. All three are prose a
model reports about itself; no tool checks them.

Activation records `intent_hash` in the frontmatter of `prd.md`
(`afspec/lifecycle.go`), and `Save` and the task form refuse an active
package whose `## Intent` no longer matches it. `requirements.json` and
`tasks.json` have no such protection: `docs/speca.md` says the contract
files change "by a lifecycle move and by nothing else after activation", and
`af-impl` says "never edit the spec package", but an edit to a criterion, a
check or a test command goes unnoticed.

`25_gate_affected_tests_budget` adds an optional `affected_tests` command to
`test_commands`, with a `{base}` placeholder. `evidence_tests` follows the
same convention.

## Requirements

### The contract hash

When `speca <package> --status active` activates a package, it records
`contract_hash` in the frontmatter of `prd.md`, beside `intent_hash`: a
SHA-256 over the canonical encoding of `requirements.json` and
`tasks.json`, the encoding `Save` writes, with every `x-` extension key left
out. A lifecycle move that rewrites the files in that encoding does not
change the hash, and neither does adding, changing or removing an `x-` key.
`--dry-run` reports the hash the activation would record.

A package *requires evidence* when it is active and has a `contract_hash`.
A package without one (a draft, or one activated before this change) is
treated everywhere below as it is today.

The report, `speca <package>`, gains `contract_drift` beside `intent_drift`:
`true` when the package has a `contract_hash` and the files no longer match
it, `false` when they match, absent when the package has no hash. It also
gains `evidence_required`.

While `contract_drift` is `true`:

- the task form, the evidence form and `--status sealed` are refused at stage
  `status`, category `lifecycle`, exit code 1, with nothing written. The
  message names the file or files that differ and says to restore them to
  the activated contract or to write a package that supersedes this one.
  There is no `fix_hint`, and `--force` and `--dry-run` change nothing;
- the report, `--validate`, `--gate`, `--gate-report`, `--render` and
  `--verify-evidence` still answer.

### The evidence command

`test_commands` in `tasks.json` may name `evidence_tests`, a command string
that holds the placeholder `{tests}` exactly once, as an argument of its
own. `speca --validate` reports a command without the placeholder, or with
it twice or inside another argument, as an error.

To run tests, `speca` splits the command as the gate does (no shell),
replaces `{tests}` with one argument per test identifier, and starts it in
`--dir`. A test identifier is a string of 1 to 500 characters with no line
break; `speca` gives it no meaning. The command prints, on stdout, one line
per test: a JSON object with `test`, the identifier as it was given, and
`status`, one of:

| `status` | Meaning |
|---|---|
| `pass` | the test ran and passed |
| `fail` | the test ran and an assertion failed |
| `error` | the test could not be built, collected or started |
| `missing` | no such test exists |
| `skipped` | the test exists and did not run |

Every other line of stdout is ignored, as is a result line for a test that
was not asked for. A test with no result line is `missing`. A test with two
result lines of different status is `error`. The command's exit code decides
nothing. Its stdout and stderr go to a log file under the gate's state
directory, and the result names it.

A repository whose `--dir` holds a `go.mod` and whose `test_commands` names
no `evidence_tests` uses a runner built into `speca`. Its identifiers are
`<package>:<Test>` or `<package>:<Test>/<subtest>`, the package as `go test`
takes it (`./cmd/speca`) and the test name as `go test` prints it. It runs
exactly the named tests, with the test cache off, and reads the results
from `go test -json`: a package that fails to build gives `error` for every
test asked of it, a name no test matches gives `missing`.

Failures, each ending the call with nothing written, exit code 1, stage
`evidence`, category `evidence`:

- no `evidence_tests` and no `go.mod`: the message names `evidence_tests`
  and `tasks.json`. `speca --validate` warns of the same
  (`evidence_command_missing`, low) for a package that would require
  evidence, so the author learns it before the first task;
- the command's program is not found, or its string cannot be split: the
  message names the command and why;
- the command is still running after `--timeout` (a Go duration, default
  `20m`, as for the gate): it is ended with every process it started, and
  the message says it timed out.

### Recording the evidence: `speca <package> --task N --evidence <file>`

The implementer calls this after writing a task's tests and before
implementing. `<file>` is a JSON file of at most 1 MiB:

```json
{
  "evidence": [
    { "covers": "REQ-1.1", "tests": ["./cmd/speca:TestGate/full"] },
    { "covers": "REQ-1.2", "tests": ["./cmd/speca:TestGate/task"],
      "no_red": "the level is reported by code REQ-1.1 already added" }
  ]
}
```

`covers` is the id of a criterion the task owns; for the integration task
it is the id of an execution path. `tests` holds 1 to 50 identifiers.
`no_red`, when present, is the exemption: a reason of 1 to 500 characters.
A task's file names at most 500 distinct tests. One test may appear under
several entries, and one entry per `covers`.

The call takes exactly one task id, which must be `in_progress`, in a
package that requires evidence. It runs every test of the file once. A test
is *exempt* when every entry that lists it carries `no_red`. Then:

- a test that is not exempt and has no recorded failing run must be `fail`;
  the run is recorded with its time and the tree id of the working tree
  (the gate's tree id; absent outside a git work tree);
- a test that already has a recorded failing run keeps it, whatever its
  status now;
- an exempt test must be `pass` or `fail`.

When every test meets its rule, the task's evidence in `progress.json` is
replaced by the file's entries with the recorded runs, `updated_at` is set,
and no other file changes. A recorded run of a test the new file no longer
lists is dropped. The file need not cover every criterion yet; the close
checks that. The result is the report of the package, with `evidence` on
the task and one change of kind `evidence` naming the task.

The call is refused, with nothing written and exit code 1, at stage
`evidence`, when:

- a test that had to fail is `pass`: the message names the test and says it
  passes without the implementation, and that the entry needs `no_red` with
  a reason if the behaviour already existed;
- a test is `error`: the message names it and says a build or collection
  failure is not a failing test;
- a test is `missing` or `skipped`: the message names it with its status;
- a `covers` names a criterion or path the task does not own, or appears
  twice: the message names it.

Every offending test and entry is named, not only the first. Usage errors
(exit code 2, nothing read): `--evidence` without `--task`, with `--state`,
with more than one task id, `next` or `all`; a file that does not exist, is
larger than 1 MiB, is not JSON of the shape above, or breaks one of its
limits. On a package that does not require evidence the call is refused at
stage `evidence`, saying why (a draft, or no `contract_hash`).

`--dry-run` runs the tests and every check, writes nothing, and returns the
result the call would have.

### Closing a task: `--state done`

In a package that requires evidence, `speca <package> --task N --state done`
checks, for each implement task it would close, after the state machine's
own checks:

1. every criterion the task owns has an evidence entry;
2. every test of those entries has a recorded failing run or is exempt;
3. every test, run now, is `pass`.

The integration task is checked the same way over the execution paths, each
mapped to its smoke test, except that rule 2 does not apply to it: its
paths are made live by the tasks before it.

When all hold, the passing run of each test is recorded with its time and
tree id, and the state and the evidence are written to `progress.json` in
one write. Each exempt entry raises a low `evidence_no_red` warning naming
the task, the criterion and the reason.

Otherwise the call is refused at stage `evidence`, exit code 1, nothing
written, for every task named in the call (the task form stays
all-or-nothing). The message names each criterion without an entry, each
test without a failing run, and each test with its status when it is not
`pass`. `fix_hint.flag` is `--force`.

`--state done --force` closes a task without the three checks. It raises a
high `task_evidence_forced` warning naming the task, and the change carries
`forced: true`. The evidence recorded so far is kept as it is.

A move to `pending`, from `in_progress` or `done`, removes the task's
evidence. A move to `dropped` needs none. In a package that does not
require evidence, the task form behaves as it does today.

### Checking the evidence again: `speca <package> --verify-evidence`

The call runs every test recorded for every `done` task on the tree as it
is, once, and answers in `result.evidence_check`: `verdict` (`pass` when
every test is `pass`, `fail` when one is not, `not_run` when the tests could
not be run or no done task has evidence, with `reason`), and one entry per
task with each `covers`, its tests and their status. It writes nothing to
the package. It exits 0 for every verdict, as the gate does, and 1 only
when the package cannot be loaded. It takes `--timeout` and `--dir`.

### What the report and the format show

- Each task of the report gains `evidence`: its entries, each with `covers`,
  `no_red` when set, and per test the identifier, the failing run (`at`,
  `tree`) and the passing run (`at`, `tree`), each absent until recorded.
- `progress.json` holds the same under each task entry; its schema, and the
  frontmatter's (`contract_hash`), gain the fields as optional ones. A
  package without them validates as before.
- `speca --schema` describes the new forms, fields, stage and warnings.
- `specr` renders no evidence, as it renders no task state.

### The implementer's skill

`skills/af-impl/SKILL.md` and its `references/test-first.md` change with
the tool: the test-first step records the failing run with `--evidence`
rather than keeping the command and its output; a test-first deviation is a
`no_red` entry; a refused close is a failed attempt, never answered with
`--force`; the commit body's criterion-to-test list is copied from the
report; and the landing runs `--verify-evidence` on the merged tree and
reports its verdict. `docs/speca.md`, the format specification and
`docs/architecture.md` describe the same.

### The flows

- **A task closes on evidence.** The implementer claims task 2 of an active
  package (`--task 2 --state in_progress`), writes its tests, and calls
  `--task 2 --evidence map.json`; `speca` runs the tests through the
  evidence command, sees each fail and writes the failing runs to
  `progress.json`. The implementer writes the code, the task gate passes,
  and `--task 2 --state done` runs the tests again, sees each pass, and
  writes the state and the passing runs. `speca <package>` shows the task
  done with its evidence.
- **A changed contract stops the work.** With a task in progress, a
  criterion's `action` in `requirements.json` is edited. The next
  `--task 2 --state done` is refused at stage `status`, naming
  `requirements.json`; `speca <package>` reports `contract_drift: true`.
  The edit is reverted, and the same call closes the task.

Constraint: Linux and macOS; Windows is best effort, as for the gate.

## Design Decisions

1. **Any runner, through one project command** (the person's decision). The
   format names the command, its placeholder and its result lines; the
   project's tool does the running.
2. **Go is built in.** A Go repository uses a runner inside `speca` when it
   names no `evidence_tests`, so the repositories that exist today need no
   adapter. Mine: the alternative makes every Go project write the same
   wrapper around `go test -json`.
3. **No runner means no close.** A package that requires evidence in a
   repository with neither `evidence_tests` nor `go.mod` cannot close a task
   without `--force`. Mine, and it follows from decision 1: a check that
   could not run was never measured. `--validate` warns beforehand.
4. **A changed contract is refused, like a drifted intent** (the person's
   decision). No call re-accepts it; the way out is to restore the files or
   to supersede the package.
5. **The hash covers both contract files, whole.** Mine: `tasks.json` holds
   which task owns which criterion and the command that produces the
   evidence, so leaving it out would let either be changed to get a close.
   `x-` keys are left out because the format gives them no meaning.
6. **An exemption is declared, stored and warned** (the person's decision):
   `no_red` with a reason, a low warning at the close.
7. **Only packages activated from now on** (the person's decision). The
   presence of `contract_hash` is the switch; there is no setting.
8. **Two calls, not one.** The failing run is recorded by a call of its own,
   while the task is in progress, because it has to happen before the
   implementation exists; the close reads what was recorded.
9. **`--force` still closes.** It is the task form's existing way past a
   refusal, with a high warning; `af-impl` forbids it. Mine.
10. **The integration task maps paths to smoke tests and needs no failing
    run.** Mine: by the time it starts, the tasks before it have made the
    paths live.
11. **Reopening a task removes its evidence.** Mine: the work is done again,
    so its proof is too.
12. **Deviations stay in ADRs.** The idea named an erratum; `af-impl` already
    records a divergence in an ADR, and decision 4 leaves nothing for an
    erratum to authorise.
13. **Profile: full.** The flows cross `speca`, the evidence command, the
    package's files and the skill.

## Dependencies

- `19_speca_gate` (active, every task done): the evidence command runs with
  the gate's command splitting, timeout, tree id and state directory.
- `12_speca_lifecycle` and `11_speca_task_states` (archived): the status
  form's activation and drift refusal, and the task form, which this
  extends.
- `25_gate_affected_tests_budget` (active, no task started): adds
  `affected_tests` to `test_commands`; both change the same schema, in
  either order.
- `27_unattended_mode` (active, not started): its subagent returns the
  criterion-to-test map; with this change the map is the evidence file.
  Either order.
- ADR 22 (run the gate with `speca`) and ADR 10 (change a package's state
  with its own command): the decisions this builds on. This change needs an
  ADR of its own.
- `skills/af-impl`: changed, as above.

## Verified External API

- `go test -json` (Go toolchain; this repository builds with go 1.27), read
  with `go doc cmd/test2json`: a stream of newline-separated objects with
  `Time`, `Action`, `Package`, `Test`, `Elapsed`, `Output`, `OutputType` and
  `FailedBuild`. `Action` is one of `start`, `run`, `pause`, `cont`, `pass`,
  `bench`, `fail`, `output`, `skip`. `FailedBuild` is set on a `fail` event
  that a build failure caused, which is how a build failure is told from a
  failing assertion. Events for a whole package carry no `Test`.
- **Unverified**: that a `-run` pattern which matches no test produces no
  event carrying that test's name in every supported Go version; assumed
  from the format's description, not run.

## Glossary

- **contract files**: `requirements.json` and `tasks.json` of a package,
  with `prd.md`; what an implementer builds against and may not edit.
- **contract hash**: the SHA-256 recorded at activation over
  `requirements.json` and `tasks.json`; new with this PRD.
- **evidence**: for one task, the map from each criterion it owns to the
  tests that cover it, with each test's failing and passing run; new with
  this PRD.
- **failing run**: a run by `speca` in which a mapped test had the status
  `fail`, recorded with its time and tree id.
- **exemption**: a `no_red` reason on an evidence entry, for a criterion
  whose test cannot fail first because the behaviour already existed.
- **tree id**: the git tree of the working tree as it is, tracked and
  untracked files included, as the gate computes it.
- **gate**: the package's `linter` and `all_tests` commands, run by
  `speca <package> --gate`.
