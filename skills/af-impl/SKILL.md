---
name: af-impl
description: Implement a version 3 specification package with the user watching. Reads the spec's three artefacts, surveys how they map onto the code, then implements the tasks in order on one branch, each claimed with speca, test-first, verified by the spec's own checks, and recorded done with speca before it is committed, and runs from the survey to the merge into the base branch on its own, without waiting to be told to continue. Opens an issue for every open item the merge leaves, linked both ways, with the issue command. Never edits the spec, and merges only on checks that pass on the merged tree.
argument-hint: "[path to a spec package, e.g. .specs/16_indexed_code_search]"
disable-model-invocation: false
---

# af-impl

You take a specification package and turn it into code, task by task, with a
person watching. It runs in the user's own session, where they can read the
survey and every task as it goes by. Watching is not a gate: you run the plan
to the end on your own (see *Run to the end*).

The format (`spec-format-v3.md` in the `spec` repository) is the contract.
Five rules shape everything below. **The plan is the contract**: every
criterion carries its check, every criterion is owned by a task, and the task
is the unit of work; you implement what a task owns and nothing else. **The
spec is read-only**: a task's state, in `progress.json`, is the only thing in
the package that changes, always through `speca`, and a task becomes `done`
only after it has been measured. **Measure, do not assert**: the spec's own
`test_commands` run before any change and after every task, and the report
says what they returned. **Tests by behaviour, traceability in the report**:
the spec's checks are behaviours to cover, written into the test files of the
code they exercise and named for what they check; which test covers which
criterion is written in the task's commit, never in the code. **One
landing**: the branch is merged into the base branch once, at the end, and
only on checks that pass on the merged tree.

Two commands of the `spec` repository do the package's bookkeeping — `speca`
reports where a package stands, gives the format's verdict and moves its tasks
through the state machine, and `specr` renders it as Markdown — and a third,
`issue` from `aflib`, opens the issues that track what the branch leaves
undone (§7). Read their JSON; do not work out a package's state, scope or
verdict by hand while the commands are there to ask.

If `.specs/steering.md` exists, read it first. Follow the repository's own
agent instructions (`CLAUDE.md`, `AGENTS.md`) for branching, commits and
landing; where they say nothing, the defaults below apply.

## Run to the end

This is one run, not a conversation: read the package, survey, every task,
conformance, the merge, the report. Nobody has to say "continue". The
person watching reads what you write as it appears and may interrupt; you do
not wait for them. If their request asks for a stop (after the survey, after
task N), stop there and say which command continues; otherwise go to the end.

You stop short of the end, and report how far you got (§8), for these
reasons and no others:

- a refusal in §2 (an unmet dependency is one), or a blocker in §3;
- `speca` refusing a task's claim or its `done`, or changing the package's
  content when it writes (§5);
- a task parked after its second failed attempt (§5), because every later
  task builds on it.

Everything else is information, not a stop: a red baseline, a `draft`
package, drift, a deviation, a recorded decision, a task's first failed attempt, a
conformance finding, a long run. Write it where the report will find it and
carry on. Do not ask whether to continue, whether a task's result is
acceptable, or whether to start the next task. A turn that ends in text is a
stop, so never end one on a summary of the task just finished: a task's
progress line (its number, commit and verdict) goes in the same turn as the
first tool call of the next task.

The branch is the record, not your memory. After any gap (a compaction, an
interruption, a resumed session), `git log`, `git status` and a fresh
`speca <package>` say where you are: `result.next_task` is the task in
progress, else the first ready one. Continue with it, as §4 describes. If
you have a task-list tool, enter every task before the first one, then
conformance, landing and the report, and finish the turn only when none is
left open.

## 1. Read the package

Read [references/speca-specr.md](references/speca-specr.md) in full before the
first call. It gives the fields each command returns, their exit codes, how to
write their output to files outside the repository, what a `speca` write does to
the package, and what to do when the commands are not installed.

The argument the skill was given is a path to a spec package, such as
`.specs/16_indexed_code_search`. It may also be a bare id (`16`), a name, or a
file inside the package; resolve it against the spec root (`.specs/` unless the
project says otherwise). The directory **must** contain all three artefacts:

- `prd.md` — the intent; its frontmatter carries `spec_id`, `spec_name`,
  `title`, `status` and an optional `profile` (`lite`: no execution paths);
- `requirements.json` — requirements, each a list of EARS criteria with their
  `check`s, plus the execution paths with their `smoke` checks;
- `tasks.json` — `test_commands`, cross-spec `dependencies`, and the ordered
  implement tasks, each owning `criteria`.

`architecture.md` is optional, and so is `progress.json`, where `speca`
writes the task states; without it every task is `pending`. The integration
task of a `full` package is in no file: the tools derive it as task N+1 and
list, render and move it like any other. If a required file is missing, stop
and say which: an incomplete package is not implemented, and it is not yours
to complete. A package under `.specs/archive/` is history; refuse it.

Ask `speca` where the package stands before you read it:

```sh
speca <package> > "${TMPDIR:-/tmp}/af-impl-<NN>-state.json"
```

In `result`: `spec_dir` is the package's directory, and it is `<package>` from
here on, in every `speca` and `specr` call and in the report; `status`, `valid`
with `failing_rules` and `intent_drift` are what §2 checks; `tasks`,
`task_counts` and `next_task` are the plan and where to start; `depends_on` is
the spec ids the package's own `dependencies` name. `next` may also suggest a
lifecycle call, `speca <package> --status active` or `--status sealed`. Those
moves belong to the spec's owner, not to this run (see *Never*); the task calls
in `next` are the ones you follow.

With no argument, run `speca --next`: `result.next_spec` is the package to
implement, the first open one in dependency order whose dependencies are
finished; say which and go on. `result.all_done` true means every package is
done; a missing `next_spec` otherwise means each open package waits, and
`result.waiting` says on what; a failure at stage `graph` is a dependency
cycle. Report any of these and stop.

Render the package with `specr` and read the rendering; do not read the JSON
files and scope them by hand. `specr` gathers a task's criteria, their checks
and the paths those name more reliably than reading them does:

```sh
specr <package> --markdown-file "${TMPDIR:-/tmp}/af-impl-<NN>.md" > "${TMPDIR:-/tmp}/af-impl-<NN>.json"
```

Read the `.md` in full. It holds the PRD, the architecture if there is one, the
requirements with their checks, and the tasks, the integration task last. A
wrong reference or task id is exit `2`;
correct it and render again, and never go on without the rendering.

Note every module, type, command, file and library the spec names, and every
`external_apis` entry whose `verified` is false: those signatures are
assumptions, to confirm against the installed library before use.

## 2. Refuse early

Before reading any code in depth, check what would make the run pointless:

- The working tree is clean. If not, stop and say so; do not stash on the
  user's behalf.
- You are on the branch the work should be based on (the default branch
  unless the user says otherwise) and it is up to date with its remote.
  Never start on the work branch itself: the diff under review would be
  empty.
- The package validates. `speca <package> --validate` gives the format's full
  verdict; [references/speca-specr.md](references/speca-specr.md) says what it
  checks and in what order to read the findings. `result.valid` is the answer,
  and a package that does not validate still exits `0`. A rule that fails is a
  defect in the spec, and the spec is not yours to edit: report it and stop.
- `status` is `draft` or `active`. A `sealed`, `superseded` or `archived`
  package is not implemented; `draft` is, with a warning, and stays a draft:
  activating it is the owner's call.
- `intent_drift` is absent. A drift means the PRD's `## Intent` was edited
  after the package was activated; the library refuses every write to such a
  package, so no task state could be recorded, and whether the new intent is a
  new package is the owner's question. Report it and stop.
- Every id in `result.depends_on` names a package that is sealed or whose
  last task is `done`: run `speca <id>` for each and read its `status` and
  the `state` of its last task. One that is not stops you here; ask.
- The package can take the state changes the run will make. Run the first one
  as a dry run: `speca <package> --task <N> --state in_progress --dry-run` for
  a `next_task` N that is `pending`, or `--state done` for one already
  `in_progress`. It writes nothing, and it checks the package, not the task: on
  a branch being continued, §4 finds the task again. A refusal at stage
  `status` or `validate` is a blocker — report `error.message` and stop, and
  never get past it with `--force`. No `next_task` at all means every task is
  `done` or `dropped`, so there is nothing to implement, or that the pending
  tasks wait on one another, which is a defect in the plan; say which, and
  stop.
- `test_commands.linter` and `test_commands.all_tests` are commands the
  project really has (`make`, `go test`, `pytest`, `npm test`), need no
  shell features, and belong to this project's ecosystem.

Then read [references/gate.md](references/gate.md) in full, and run the
baseline, `speca <package> --gate full`, on the unchanged tree; note its
verdict and commit. A red baseline is a fact, not a blocker: it
changes what a pass means later, so say so in the report and go on with the
baseline as the bar; do not stop to ask. If the user asked for it to be fixed first, that is one
commit of its own before any task — `fix: <subject>` with a
`Spec: <NN_name>, repair` trailer — and the bar after it is green.

## 3. Survey

One read-only pass over the spec and the repository, before any branch exists.
Read only files `git ls-files` lists. Read
[references/survey.md](references/survey.md) in full before starting: it gives
the five things the brief records — where things live, the project's
conventions, where each of the spec's checks goes, the drift between the spec
and the code with a numbered decision for each, and what each task's `steps`
and `touches` leave out.

The survey also reads back the open items this package must take up or not
build on, as [references/survey.md](references/survey.md) says.

Show the brief, in full, before writing code, then go straight on to §4 in the
same turn. The user can steer by interrupting; you do not wait for them to.

A spec that cannot be implemented as written — it names a module the PRD does
not say to create and nothing in the code plays that role, or it contradicts an
invariant the code enforces — is a blocker: ask, with both readings and what
each would change. That is the right answer perhaps one time in twenty.
Everything smaller is drift with a resolution.

## 4. Branch

`feature/<NN>-<slug>` from the base branch, where `NN` is the `spec_id` and
the slug is the `spec_name`. When that branch already exists, check it out and
**continue**: a `wip:` commit at the tip is dropped so the task starts again
from the last landed commit, and then `speca <package>`, run on the branch,
says where the plan stands. Tasks whose `state` is `done` are skipped, and
`next_task` is where you start: a task left `in_progress` is reopened as it
is, a `pending` one is claimed (§5). Never force-push, and never rewrite a
commit that landed.

## 5. Each task, in order

For each task that is not `done`, in array order (a task's `depends_on`
must all be `done` first):

1. **Claim.** When the task is `pending`, claim it before anything else:

   ```sh
   speca <package> --task <N> --state in_progress
   ```

   A task already `in_progress` is the one being resumed, and it is yours: do
   not claim it again, since the state machine refuses a second claim. A
   refusal names its stage, which
   [references/speca-specr.md](references/speca-specr.md) explains; each is a
   blocker, so report `error.message` and stop. A `task_not_ready` warning
   means something the task depends on is not `done`: release it
   (`--state pending`) and take `result.next_task` instead. The claim is a
   change to the working tree, committed with the task's work.

2. **Scope.** Render the spec scoped to this task, to files outside the
   repository as in §1:

   ```sh
   specr <package> --task <N> --markdown-file "${TMPDIR:-/tmp}/af-impl-<NN>-task-<N>.md" > "${TMPDIR:-/tmp}/af-impl-<NN>-task-<N>.json"
   ```

   Read the Markdown in full: it holds what the task owns, and every other
   requirement, path and task by id and title as context. Render again for each
   task, because states change as tasks land. `result.task` must say
   `in_progress`. Implement this task and no other; do not touch another task's
   work.

3. **Orient.** Read the files the task `touches` and their tests, and the
   survey's notes on them. Paths in the spec are what the author expected;
   confirm them against the code.
4. **Test first.** Read
   [references/test-first.md](references/test-first.md) in full once, before
   the first task's tests, and work from it for every later task without
   reading it again. It says how a task's checks become the project's own
   tests — at the level where the behaviour lives, named for the behaviour,
   in the test file of the area they exercise — and what each check kind
   asserts.

   The gate is this. Run the new and extended tests and watch them fail
   **before** any implementation file changes, and keep the command and the
   failure, because you report them. If the tests cannot compile until the task
   adds a signature, add the minimal stub first, then see them fail on
   behaviour and not on a compile error. A test that passes before the
   implementation proves nothing about it: when a red first run is impossible,
   say so as a test-first deviation, with the existing test that covers it,
   rather than burying it in notes.

   Write down, for the commit, which test covers each criterion the task owns.
5. **Implement** the `steps`, in order, until the tests pass. Follow the
   conventions the survey recorded and the files you are editing already
   use. No unrelated improvements: a "while I was here" fix belongs in a
   change of its own. When every task lists `touches`, the change may touch
   only those paths, the paths the survey's decisions name, documentation,
   and the test files of the areas the task touches.
6. **Check, narrowly.** Run the tests you wrote, then the package you
   changed, in the suite's targeted form (`go test ./pkg -run Name`,
   `pytest tests/test_x.py::test_y`, `npm test -- <file>`). The full suite
   is the gate's job, once, after the task.
7. **Documentation.** Update what the change makes wrong, from the code:
   every quoted string, field name, exit code and sample payload is copied
   from the code or a test, not from the PRD, and you can say which line it
   came from. When the code does not do what the spec asks, the docs say
   what the code does. The steering file's documentation table, if there is
   one, names the files.
8. **Never edit the spec package.** The task's state, written by `speca`,
   is the one change it takes. If the implementation has to diverge
   from a criterion, its contract or a survey decision, do what is
   right for the code, record the decision as a rule the code follows in
   the ADR of the feature (or a new ADR), or where the repository's
   instructions say decisions go, update the reference documentation, and
   declare the deviation in your report with the code line and the test of
   the delivered behaviour. A gap you declare is reported as one; a gap you
   bury in notes is reported as a defect.

Then the gate, `speca <package> --gate task` (`full` after the integration
task), its verdict compared with the baseline:

| Before | After | Verdict | Land? |
|---|---|---|---|
| green | green | pass | yes |
| red | green | pass, baseline was failing | yes, and the report says so |
| green | red | regressed | no |
| red | red | still failing | no |
| a check could not run | | gate failed | no; the work was never measured |

A task lands only on a landable verdict **and** a report that answers for
every check of its criteria and every `done_when` entry. A task that changed
nothing
is a failed attempt. A failed attempt is discarded — reset to the last
commit, new files removed — and the task is tried once more with the failure
in view, at once, without a message in between. The reset takes the claim
with it, so claim the task again (step 1) before the second attempt. A second
failure is parked as a `wip: <subject>` commit with the task's state
`in_progress`, which the claim already wrote; that ends the run, because every
later task builds on it. Return to the base branch and report what failed
with the check's own output. A test that still fails is
reported as failed, with why; it is worth more than one claimed passed on a
run that never happened. Never skip, delete or weaken a test to get green.

**Record.** On a landable verdict, record the task done, through the state
machine:

```sh
speca <package> --task <N> --state done
```

Check with `git status` and `git diff` on `<package>` that the claim and this
call changed `progress.json` alone; another file changed, or a refusal, is a
blocker: report it. Never retry with `--force` or set a state by hand while
`speca` is there; without it, change that one `state` entry and say so.

**Commit** the recorded state with the task's work as one `feat: <subject>`
commit, its body ending in a `Spec: <NN_name>, task <N>` trailer and a
`Deviation: {"key": …, "reason": …}` trailer for each deviation declared. The
body lists every criterion the task owns against the test that covers it, one
per line, in the form
[references/test-first.md](references/test-first.md) gives: that is where the
package's traceability lives. Do not write that checks pass; say what they
returned. The gate that passed after task N is the baseline for task N+1, so
"regressed" always means "this task broke it".

**Next.** §7 is assembled from `git log`, not from what you remember, so the
commit body carries what the merge commit will need: the test verdicts, the
test each criterion maps to, any test-first deviation. Then write one progress line
and, in the same turn, begin the next task, which the `done` call's `next` and
its `result.next_task` name. When there is no `next_task` and no task is
`pending` or `in_progress`, begin §6 the same way.

**The integration task** is last, task N+1: `speca` claims and records it
like any other, and `specr <package> --task N+1` renders every path with its
smoke check and a `## Contracts` section. It exists to catch the wiring gaps
component tests cannot see. Its section of
[references/test-first.md](references/test-first.md), already read in step 4,
says what it writes, traces and searches. A path that is not live in production code fails this
task, and a recorded decision or a deferral does not satisfy it. A `lite`
package has none.

## 6. Conformance

Every task was verified by the checks; before anything is merged, ask what the
checks cannot: does the finished branch do what the spec says? Read
[references/conformance.md](references/conformance.md) in full before
measuring, and measure the whole diff from the base branch — its structure, its
tests, its scope, the package itself, a clean environment, and a review of the
diff against the spec on a fresh footing.

`missing`, `different`, `tautological`, `no_assertions`, `not_followed`, a
contradicted document and a clean-environment failure are **blocking**. Fix
what can be fixed in one `fix: <subject>` commit with a
`Spec: <NN_name>, conformance` trailer, landed only on checks that still pass;
declare what cannot, with a recorded decision. Documentation findings and files
out of scope can only be fixed.

What remains is the `unmet` list: every **open item**, meaning what the branch
does not do that the spec asks, and what the run could not show. Each is one
entry, with what is missing, why, the ids it concerns and its evidence. §7
opens an issue for each, and the merge commit must say the work is not complete
while the list is non-empty.

## 7. Land

Read [references/landing.md](references/landing.md) in full before pushing. It
gives the issue every open item gets and how to look for one that exists
already, the merge commit's message section by section, the merge itself, and
the fallbacks when `issue` is not installed.

- Push the branch to origin, unless the repository's instructions or the user
  say the branch stays local.
- **Track the open items.** Every entry of the `unmet` list gets an issue
  before the merge, so the gap is tracked after it and the merge commit can
  link each one. An issue that cannot be opened at all stops nothing: the
  entry stays in the merge commit with `(no issue: <the reason>)`, and the
  report gives the call that opens it.
- **Merge** the branch into the up-to-date base branch with `--no-ff`, and
  commit the merge only on a landable `--gate full`, then thorough target, on
  the merged tree. Its message holds what the run measured, open items
  first, and every claim in it comes from something that ran. A conflict or a
  verdict that is not landable aborts the merge.
- **Push the base branch**, unless the branch stays local, and stay on it.
  Do not seal the spec, though `speca <package>` now suggests
  `speca <package> --status sealed`: the lifecycle is the owner's. Name that
  call in the report.

After the push, close each listed item the branch resolved and pass on what
awaited this package ([references/landing.md](references/landing.md)).

If a push or the merge fails, say exactly what failed and leave the branch and
commits in place.

## Never

- Edit `prd.md`, `requirements.json` or `tasks.json`, or `progress.json` by
  hand while `speca` is there. A task's `state` changes through
  `speca <package> --task <N> --state <S>`. A spec that is wrong gets a
  recorded decision and a report, not a quiet fix.
- Move the package's lifecycle (`speca --status`: activate, seal, archive,
  supersede) or pass `--force` to `speca`. The lifecycle is the owner's, and
  a move the state machine refuses is a finding, not an obstacle.
- Implement a task whose tests you have not seen fail, short of a declared
  test-first deviation, or claim a test you did not write or a check you
  did not run.
- Write a verification result the checks did not produce, or soften a red
  baseline into a pass.
- Skip, delete or weaken a test to get green, or satisfy the integration
  task with a deferral.
- Start the next task before the current one has landed or been parked.
- Stop between tasks, ask whether to go on, or end a turn for any reason
  *Run to the end* does not list.
- Commit to the base branch anything but the one merge of §7, merge on a
  verdict that is not landable, or squash. Force-push, or rewrite a landed
  commit.
- Stash, reset or otherwise change the user's uncommitted work.
- Widen the change to things no task asked for.
- Put a spec, requirement, criterion or path id in code, a test name, a test
  file name or a comment; the commit body is where they go.
- Leave an open item without an issue or the reason it has none, or open one
  for anything else. Edit, comment on, relabel or close an issue that is not
  `af:*`, or one that neither awaited this package nor is resolved by it.

## 8. Report

- The spec: id, title, status, and tasks total, done, skipped, remaining,
  from a last `speca <package>` (`task_counts`).
- Branch, commits, the merge commit and whether the base branch was pushed
  (or what stopped it).
- Per task: outcome, commit, verdict from the table, and the check output
  when it is not a pass.
- The survey's decisions, and the deviations declared with the ADRs that
  record them.
- The conformance findings, fixed and unmet.
- The gate report, and the open-item counts the trailer holds.
- The open items, each with its issue URL, or why it has none and the
  `issue --create` call that opens it.
- Anything left unfinished, and the command that continues it.
