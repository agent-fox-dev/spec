---
spec_id: "11"
spec_name: "speca_task_states"
title: "speca: change the state of a package's tasks"
status: "active"
created_at: "2026-10-22T07:30:22.814235Z"
updated_at: "2026-10-22T07:30:22.814235Z"
intent_hash: "cbc694a5761e9e7b068e3446dfc496a192d57bdf81188ae91e8c70a2969986da"
schema_version: 2
source: "docs/prd/02-change-the-state-of-a-package-with-its-own-command.md"
---
## Intent

An agent working from a shell needs to record its progress through a spec package's plan by claiming a task, finishing it, releasing it or dropping it. It should do this through the library's task state machine and atomic save, not by editing `tasks.json` with sed. This spec gives the `speca` command the form that changes task states. One call moves one or more tasks, and either every move is allowed or none is made. A move the machine does not allow is refused with its reason and the states that are allowed. Every change is listed in the result, so a caller never has to read the files to learn what the call did.

## Goals

- `speca <spec> --task N --state S` moves task N (or several ids, or `next`, or `all`) to state S through `TasksV2Json.TransitionTask`. If any one named move is not allowed, no task changes, the call exits 1 at stage `task` with category `lifecycle`, and `fix_hint.valid` lists the states the task may move to.
- Claiming a task that another call has already claimed fails (`in_progress → in_progress` is not a move). This makes the state machine the compare-and-set that two agents need.
- `--force` with target `done` or `pending` reaches the library's own bypasses, `CompleteTaskStates` and `ResetTaskStates`, and raises a `high` `task_state_forced` warning for each task moved that way. It bypasses nothing else.
- `--dry-run` computes the exact result, refusals and warnings included, and writes nothing.
- A write never silently destroys content. It is refused for a sealed, superseded or archived package, for an active package whose intent has drifted, and for a package that holds keys the schema does not know (unless `--force` is given). It is refused before anything is written and also under `--dry-run`.
- The result lists every change as `{"kind": "task", "id": N, "from": …, "to": …}` and reports the package as it is after the call. `summary` names the move.
- The report form's `next` and the list form's `next` suggest the claim of the next ready task. After a move, `next` points at the call to make after it.

## Non-goals

- Lifecycle moves (`--status active|sealed|archived|superseded`, `--by`), the guards on activation and sealing, moving a package under `archive/`, and the `tasks_open` and `supersedes_not_recorded` warnings. These belong to the next scope, `speca_lifecycle`. That scope extends `--force`, `--dry-run`, `changes` and the `status` stage to its own form.
- The help line naming the sibling commands, `specr`'s `next` suggesting `speca`'s claim and done calls, and the `af-impl` example gaining those calls. These belong to `state_command_cross_links`.
- The report, `--validate` and `--list` forms themselves, the resolver, `TaskInfo.ready`, `afspec` `IsReady`/`NextTask`, `AdminResult`/`DecodeSpeca`, `make build` and ADR 10. These come from `speca_state_report`, which this spec builds on.
- Editing anything about a task other than its `state`: title, steps, tests, criteria, `depends_on`, `optional`.
- A lock between agents. Two writers of one package at once get the last writer's files, as they do with the library.
- Converting a format version 1 package, or writing one.
- Any change to `afspec`'s task state machine or to `Spec.Save`.

## Background

`afspec/task.go` implements format v2 §8.3.1: `pending → in_progress | dropped`, `in_progress → done | pending`, `done → pending`, and `dropped` terminal. `(*TasksV2Json).TransitionTask(taskID int, target string) (*TasksV2Json, error)` returns a new copy, or a `*LifecycleError` for an unknown state, an unknown id or a disallowed move. `CompleteTaskStates([]int)` and `ResetTaskStates([]int)` set `done` or `pending` directly and skip unknown ids. `ValidTaskTransition(current, target TaskState) bool` is exported. The transition table itself is not, so the allowed targets from a state are found by testing each of the four states.

`(*Spec).Save(dir)` refuses `sealed`, `superseded` and `archived` with a `*LifecycleError`. On an active package it refuses intent drift with an `*IntentError` (including a body that has lost its `## Intent`). It writes every artifact through temp files, then renames them. **It does not set `updated_at`**, although format v2 §4 says `updated_at` is "updated on every save". `examples/tasks/main.go`'s `setState` sets `spec.UpdatedAt = time.Now().UTC().Format(time.RFC3339)` itself before `Save`, and that is the loop this command makes callable from a shell.

`Save` re-marshals every artifact from the Go types. `LoadSpec` decodes JSON into those types and drops keys the schema does not allow. It records them only so that `ValidateSchema` reports them as `unknown_field` errors (`afspec/unknown.go`: "would then vanish silently instead of being rejected"). So a write to such a package deletes those keys. A newer format's optional field or a leaked v1 `traceability` would be lost without notice.

A v1 package loads, but the v2 types read none of its tasks. `specr` refuses `--task` on a package whose `schema_version` is neither 0 nor 2, as a usage error. A `Save` of such a package would replace its v1 artifacts with v2 encodings of almost nothing.

`speca_state_report` (spec 10) delivers `cmd/speca` with the report, `--validate` and `--list` forms, `envelope.AdminResult`, the shared resolver and `TaskInfo` conversion in `internal/specpkg`, `intent_drift`, `next_task`, and `afspec`'s `IsReady`/`NextTask`. This spec adds the first form that writes.

## Requirements

### 1. The task form and what it accepts

- `speca <spec> --task T --state S` is the task form. It takes exactly one positional input, resolved as the report form resolves it.
- `--task` is given once or repeated, and each value may be comma-separated. Each value is one of:
  - a task id: an integer, 1 or more;
  - `next`;
  - `all`.
  `next` and `all` must each be the only value of `--task`. Duplicate ids collapse into one.
- `--state` is one of `pending`, `in_progress`, `done`, `dropped`.
- Each of the following is a usage error: exit 2, stage `usage`, category `usage`, nothing written.
  - `--task` without `--state`, or `--state` without `--task`.
  - A `--state` value outside the four. `fix_hint` is `{flag: "--state", valid: [the four]}`.
  - A `--task` value that is not a positive integer, `next` or `all`, or `next`/`all` mixed with other values.
  - The task form combined with `--validate` or `--list`, because two operations are named.
  - No positional input, or more than one.
  - `-`, a URL or a multi-line input.
  - `--force` or `--dry-run` without a writing form. They apply to `--task` here.
- The package is then loaded. A load failure is exit 1 at stage `load`, category `input`, as in the report form.
- After the load, these are also usage errors (exit 2), each naming the package:
  - **An id the plan does not have.** The message names every unknown id. `fix_hint` is `{flag: "--task", valid: [every task id]}`. A plan with no tasks says so and has no `fix_hint`.
  - **`next` when there is no `next_task`.** The message says whether every task is done or dropped, or whether no pending task has its dependencies done. In the second case `fix_hint` is `{flag: "--task", valid: [the pending ids]}`.
  - **`all` on a plan with no tasks.**
  - **A package that declares a format version other than 2** (by `specr`'s rule: `schema_version` neither 0 nor 2). The message says that `speca` writes version 2 only and that a save would discard the v1 plan.

### 2. Expanding `next` and `all`

- `next` resolves to the report's `next_task`, from `afspec`'s `NextTask`: the first task `in_progress`, else the first ready pending task.
- `all` resolves to every task in the plan except those already in the target state and those that are `dropped`. When that leaves nothing, the call succeeds (exit 0) with no changes and writes nothing.
- An explicit id is always moved as named, even when the task is already in the target state. That move is then refused by the machine, which is the compare-and-set.

### 3. All-or-nothing moves through the state machine

- Every named task is checked with `ValidTaskTransition` before anything is applied. The allowed moves are applied in plan order with `TransitionTask`, each on the copy the previous one returned.
- If any named move is not allowed (and is not covered by `--force`, see 5), the whole call is refused: exit 1, stage `task`, category `lifecycle`, and no task changes. The message names every refused task with its current state and the target, cites format v2 §8.3.1, and says what each refused task may move to. `fix_hint` is `{flag: "--state", valid: [states the first refused task, in plan order, may move to]}`. It is absent when that task has no moves (it is `dropped`). In that case the message says that `--force` with `done` or `pending` is the only way out.
- A claim of a task already `in_progress` is refused this way. That refusal is how two agents learn that a task is taken.

### 4. Writes the package's status allows

- Before any move is checked, the command refuses a package that `Save` would refuse. This happens in both normal and `--dry-run` runs, so the two agree. The refusal is exit 1, stage `status`, category `lifecycle`, with no `fix_hint`. It applies to:
  - a `sealed`, `superseded` or `archived` package. The message says its tasks can no longer change and a change is a new package that supersedes it.
  - an `active` package with intent drift (`intent_drift` from the report, including a body with no `## Intent` while `intent_hash` is set). The message says the library refuses every write until the intent matches its hash again.
- `--force` does not bypass this. The library has no public way to write such a package.
- A `*LifecycleError` or `*IntentError` that `Save` itself returns maps to the same stage and category.
- Drafts and active packages without drift take task changes.

### 5. `--force`

- When the target is `done` or `pending`, a named move that the machine refuses is made with `CompleteTaskStates` or `ResetTaskStates` instead. Moves the machine allows still go through `TransitionTask`.
- Each task set through a bypass:
  - is named in one `high` `task_state_forced` warning (stage `task`) with its from and to states;
  - carries `forced: true` on its change entry.
- A task already in the target state produces neither a change nor a warning.
- With target `in_progress` or `dropped`, `--force` does not bypass the machine. The library has no setter for those states, and the command writes no state by hand. A refusal message in that case says so.
- `--force` also skips the unknown-key guard of requirement 6. It skips nothing else: not usage errors, not the status refusal of requirement 4, and not the legacy-version refusal.

### 6. Readiness and validity

- Moving a task to `in_progress` when `IsReady` reports it not ready does not stop the call. A `low` `task_not_ready` warning (stage `task`) names the task and the ids in its `depends_on` that are not `done`. Readiness is judged on the plan as it was before the call.
- A package that does not validate still takes a task change, with one `low` `spec_invalid` warning (stage `validate`) naming the failing rules, as `specr` names them (distinct `Check`, else `Category`).
- **Exception:** when validation reports `unknown_field` errors, the write would delete those keys. The call is refused before anything is written: exit 1, stage `validate`, category `invalid_spec`. The message lists each artifact and key. There is no `fix_hint`.
- With `--force`, the write goes ahead and the one `spec_invalid` warning is `high`, naming the keys the write removed.
- A call raises at most one `spec_invalid` warning.

### 7. Writing the change and reporting it

- When at least one task changes, the command:
  1. sets `updated_at` to the current time in UTC, RFC 3339, as `examples/tasks` does;
  2. writes the package with `Spec.Save(spec.Dir)`.
- A `*SaveError` is exit 1, stage `write`, category `disk`, `retryable: false`. The message says that a failure while writing the temporary files leaves the package unchanged, and that a failure during the final renames may leave some files updated, so the caller should run `speca <spec>` to see the state.
- When nothing changes, nothing is written and `updated_at` is unchanged.
- On success (exit 0) the result is the report of the package after the call, with every field the report form produces, computed on the state that was written. It adds `changes`: one `{"kind": "task", "id": N, "from": S0, "to": S1}` per task whose state changed, in plan order, with `forced: true` where requirement 5 applies.
- `summary` reads:
  - `speca: <dir name> task N <from> → <to>` for one change;
  - `speca: <dir name> tasks N, M → <to>` for several;
  - `speca: <dir name> no task changed` for none.
  Under `--dry-run`, ` (dry run, nothing written)` is appended.
- On a refusal at stage `status`, `task` or `validate`, or a failure at stage `write`, the result is the report of the package as it is on disk, with no `changes`, so the caller sees the current task states without another call. Usage and load failures carry no result.

### 8. `--dry-run`

- `--dry-run` runs the task form through every check, expansion, warning and refusal above, and skips the write.
- The result is the result the call would have produced, including the would-be `updated_at`, with `dry_run: true` and the changes it would have made.
- A refusal is the same refusal, with the same exit code.

### 9. `next`

- **Report form.** `next` gains the claim suggestion, `speca <spec_dir> --task N --state in_progress`, when all of these hold:
  - no task is `in_progress`;
  - `next_task` is a ready pending task;
  - the package can take a write (a draft, or active without intent drift).
- **List form.** `next` gains the same claim for the first `active` package, in number order, that has no task in progress, has a ready pending task and has no intent drift.
- **After a successful move.** `next` is the report's `next` computed on the package after the call. So a claim is followed by `specr <spec_dir> --task N`, and a `done` by the next ready task's claim, or nothing when no task is left.
- **Under `--dry-run`.** `next` holds one entry: the same call without `--dry-run`. It names the resolved ids (comma-joined) rather than `next` or `all`, and keeps `--force` when it was given. It is empty when there are no changes.
- **On any failure.** `next` is absent.

### 10. Envelope, self-description and documentation

- `envelope` gains:
  - `CategoryLifecycle = "lifecycle"` (not retryable);
  - `WarnTaskNotReady = "task_not_ready"` and `WarnTaskStateForced = "task_state_forced"`, with their `internal/cli` aliases and `warnStages` entries at stage `task`;
  - the `Change` type, with `kind`, `id`, `from`, `to` and `forced`. `kind` is an open set; this spec writes only `task`, and `id`, `from`, `to` and `forced` are `omitempty`;
  - on `AdminResult`, `changes` (`omitempty`) and `dry_run` (`omitempty`).
  Nothing already in `envelope` changes meaning.
- `speca`'s help covers the task form, `--force` and `--dry-run`.
- `--schema` describes the new flags, with `--state` as an enum, the new result fields, and exit codes covering refusals. The golden file is regenerated.
- `docs/speca.md` documents the form, the flags, the errors and the warnings below. `docs/api.md` adds `Change`, `CategoryLifecycle` and the two warning codes. `docs/architecture.md` notes that `speca` writes through `Spec.Save`. `docs/generated/` is regenerated with `make doc`.

### Errors and exit codes added by this spec

| `stage` | `category` | When | Exit |
|---|---|---|---|
| `usage` | `usage` | a `--task`/`--state` value or combination that cannot hold, an unknown task id, `next` with no next task, a v1 package, `--force`/`--dry-run` without a writing form | 2 |
| `status` | `lifecycle` | the package is sealed, superseded or archived, or active with intent drift | 1 |
| `task` | `lifecycle` | the task state machine does not allow a named move; nothing was written | 1 |
| `validate` | `invalid_spec` | the write would delete keys the schema does not allow, without `--force` | 1 |
| `write` | `disk` | `Save` could not write the package | 1 |

### Warning codes added or used by this spec

| Code | Severity | Stage | When |
|---|---|---|---|
| `task_not_ready` | low | `task` | a task moved to `in_progress` has a dependency not `done` |
| `task_state_forced` | high | `task` | `--force` set a state the machine would have refused |
| `spec_invalid` | low; high when `--force` writes over unknown keys | `validate` | the package does not validate and the change went ahead |

## Design Decisions

1. **The library makes every change, and the command only adds checks.** Moves go through `TransitionTask`, bypasses through `CompleteTaskStates`/`ResetTaskStates`, and the write through `Save`. One state machine cannot drift from a copy of itself.
2. **Moves are all-or-nothing, checked in full before any is applied.** A partial claim of three tasks would leave the plan in a state no caller asked for.
3. **A refused move gets the category `lifecycle`, not `usage`.** The package was read and the call was well-formed. `usage` would claim that nothing was read and would hide the allowed targets that `fix_hint.valid` carries.
4. **The state machine is the compare-and-set, and there is no lock.** The read-to-write window is microseconds, and a lock file would bind every embedder.
5. **`--force` reaches only the library's own bypasses.** `done` and `pending` have setters. `in_progress` and `dropped` do not, and the command never writes a state by hand.
6. **The command sets `updated_at` itself, because `Save` does not.** The input assumed `Save` sets it. The code shows it does not, and `examples/tasks` sets it before `Save`. Changing `Save` would alter every embedder's round-trip.
7. **Package-status refusals are checked by the command before the write, as well as by `Save`.** Without that, `--dry-run` could not report the refusal the real call would hit. They are reported at stage `status`, as the input's error table places them.
8. **A task change goes ahead on an invalid package, except when the write would delete unknown keys.** Recording work should not wait on a dangling id. But `Save` silently drops keys the Go types lack, and that is data loss, so it is a refusal `--force` can override.
9. **A v1 package is refused as a usage error, as `specr` refuses `--task` on one.** A v2 save of a v1 package would destroy its plan.
10. **`next` resolves to `next_task` exactly.** The report, its `next` suggestion and `--task next` then name the same task. `--task next --state in_progress` is refused while a task is in progress, which tells the caller to finish that task first.
11. **`all` skips tasks already in the target state and dropped tasks. An explicit id never skips.** `all` states an end state in bulk, while an explicit id is a claim on one task whose current state matters. A dropped task is terminal and changes only when named by id with `--force`.
12. **When several tasks are refused, `fix_hint.valid` holds the targets of the first refused task in plan order.** The message names every refused task. One flag can carry only one set of valid values, and the single-task case, which is the common one, is then exact.
13. **No changes means no write.** An idempotent call leaves `updated_at` and the files untouched.
14. **`next` after a move is the report's `next` on the new state.** The input asked for "the same task's claim" after a release. The report rule gives that same task whenever it is the first ready one, and otherwise gives the earlier ready task that `next_task` already names. One rule keeps the forms agreeing.
15. **The list form's claim suggestion lands here.** `speca_state_report` could not suggest a call that did not exist yet.
16. **A refusal still carries the package's report, without `next`.** The caller sees the current states without another call. Suggestions after a refused claim could point one agent at another agent's task.
17. **No `artifacts` entry for a state change.** Nothing new was produced. `changes` is the record of what was written.
18. **Change entries carry `forced: true` for a bypassed move.** A program can then tell forced moves from normal ones without parsing a warning message.
19. **`--force` and `--dry-run` without a writing form are usage errors.** A flag that has no effect on the call is a flag that cannot hold.

## Dependencies

| Spec | Reason |
|---|---|
| `10_speca_state_report` | Provides `cmd/speca` and its report builder, `intent_drift`, `next_task` and `next`, `envelope.AdminResult`/`DecodeSpeca`, `internal/specpkg` (resolver, `TaskInfo` with `ready`), and `afspec`'s `IsReady`/`NextTask`. This spec adds the task form, `changes`/`dry_run` and the claim suggestions to them. |
