---
spec_id: "12"
spec_name: "speca_lifecycle"
title: "speca: change a spec package's lifecycle status from the shell"
status: "active"
created_at: "2026-10-22T07:38:59.238888Z"
updated_at: "2026-10-22T07:38:59.238888Z"
intent_hash: "41b432c6da8f9ce99881f49e1f81acc48c449a79a999a82ad9a26d7cf1650419"
schema_version: 2
source: "docs/prd/02-change-the-state-of-a-package-with-its-own-command.md"
---
## Intent

A spec package moves through a lifecycle that the format defines and the library enforces: draft, active, sealed, superseded, archived. An agent working from a shell cannot make any of these moves today. This spec gives `speca` the form that changes a package's lifecycle status. In one call it activates, seals, archives or supersedes a package through the library, and it first applies the checks an operator would make. It moves archived and superseded packages under `archive/`, as the format requires. A move the lifecycle does not allow is refused, with the reason and the moves that are allowed.

## Goals

- `speca <spec> --status active`, `--status sealed`, `--status archived` and `--status superseded --by <spec>` make the move through `afspec` (`Transition`, `Supersede` and a new move that keeps the status). Each call exits 0 and lists every change it wrote.
- A move the lifecycle table does not allow writes nothing and exits 1 at stage `status`, category `lifecycle`. `fix_hint.valid` lists the allowed targets, and the message names the sequence of moves that leads to the target when one exists.
- Activation computes the intent hash. It is refused for a package with no `## Intent` section, and for a package that does not validate unless `--force` is given. Under `--force` the call goes ahead with a `high` `spec_invalid` warning.
- Sealing is refused while any task is `pending` or `in_progress` unless `--force` is given. Under `--force` the call goes ahead with a `high` `tasks_open` warning. Sealing is always refused for a package whose intent has drifted.
- After archiving, the package sits under `archive/` with status `archived`. After superseding, it sits under `archive/` with status `superseded`. If the destination already exists, the call is refused before anything is written.
- Superseding adds the old `spec_id` to the replacing package's `supersedes` when that package can be saved. When it cannot, the call still succeeds and raises a `low` `supersedes_not_recorded` warning.
- If the directory move fails after the status was written, the call exits 1 at stage `archive`, category `disk`, with `resumable: true`, and the result lists what was written. Running the same call again moves the package and changes nothing else.
- `--dry-run` produces the same result, including refusals and warnings, and leaves every file and directory untouched.
- `afspec` gains `ArchiveDestination` and `MoveUnderArchive`. `MoveToArchive` is rebuilt on them and no longer writes a status before it finds that the destination is taken.
- The report form's `next` suggests `--status active` for a draft that validates and has an intent, and `--status sealed` for an active package with no open task.

## Non-goals

- Task-state moves (`--task … --state …`), the `lifecycle` error category, `envelope.Change`, `changes`, `dry_run`, the flags `--force` and `--dry-run`, and the claim suggestion. These belong to `11_speca_task_states`. This spec extends them to `--status`.
- The help line in `spec` and `specr` naming the sibling commands, `specr`'s `next` suggesting `speca` calls, and the `af-impl` example. These belong to `state_command_cross_links`.
- `--status draft`, reopening a sealed package, or moving a package back out of `archive/`.
- Re-hashing a changed `## Intent`. Editing `owner`, `tags` or `source`, or removing entries from `supersedes`. The only write to `supersedes` is the one superseding makes.
- Lifecycle moves on a package whose `schema_version` is neither absent nor 2.
- A copy-then-delete fallback when the move crosses filesystems.
- Checking that the replacing package actually covers the old one's requirements.
- Changing what `MoveToArchive` does to a superseded package. It still transitions it to `archived`, which the table allows.
- A lock between agents.
- Lifecycle suggestions in the `--list` form's `next`.
- A new ADR. ADR 10, written by `10_speca_state_report`, already records the decision to build `speca` with all its forms.
- Changing `RetryableFor` or the retry semantics of `internal/cli`.

## Background

`afspec/lifecycle.go` holds the lifecycle:

- **`validTransitions`:** `draft → active|archived`, `active → sealed`, `sealed → superseded|archived`, `superseded → archived`. `archived` is terminal.
- **`ValidTransition(current, target string) bool`.**
- **`(*Spec).Transition(target, dir string) (*Spec, error)`:**
  - refuses a move not in the table with a `*LifecycleError` (`invalid transition from "x" to "y"`);
  - on `draft → active`, computes `ComputeIntentHash(PRDBody)`, which returns an `*IntentError` when there is no `## Intent`, and captures the identity snapshot;
  - writes through the unexported `saveToDisk`, which skips every guard `Save` has.
- **`(*Spec).Supersede(supersedingSpecID, dir string) (*Spec, error)`:**
  - refuses a package that is not `sealed`;
  - prepends `> **DEPRECATED:** This specification has been superseded by spec <id>. …` to the body;
  - sets `superseded` and writes through `saveToDisk`.
- **`MoveToArchive(specDir, root string) error`:**
  - loads the package and transitions it to `archived` if it is not already. This also turns a `superseded` package into `archived`;
  - only then creates `<root>/archive/` and checks whether `<root>/archive/<name>` exists. A taken destination therefore leaves a package archived but still in place;
  - renames the directory. Its comment promises a copy-then-delete fallback that the code does not have.

None of these functions sets `updated_at`. Format v2 §4 says it changes on every save, and `examples/tasks/main.go` sets it by hand before `Save`.

`(*Spec).Save` refuses a sealed, superseded or archived package, an identity change on an active one, and intent drift (`*IntentError`). `Transition` bypasses all of these, so the library would seal an active package whose intent has drifted. `afspec.HasIntent(body)` applies the same rule as `ComputeIntentHash`. `Validate` does not check for the intent section.

`LoadSpec` keeps keys that the schema does not allow out of the decoded types. `Validate` reports each one as an `unknown_field` error, and any write through `saveToDisk` silently drops them. `spec` activates a package only after it validates (`specgen/pipeline.go` around line 926).

Format v1.3 §9, which v2 §9 leaves unchanged, sets the rules this spec implements:

- **Superseding (§9.1):** the new spec lists the old id in `supersedes`, the old one gets a banner and the status `superseded`, and its folder moves to `archive/`.
- **Archiving without superseding (§9.2):** `sealed → archived` or `draft → archived`, and the folder moves to `archive/`.
- **Mutations:** a sealed, superseded or archived package allows none.

Today no shell command can make any of these moves. Spec 09 of this repository has every task done and is still `active`.

## Requirements

### 1. A move under `archive/` that keeps the status

- `afspec.ArchiveDestination(specDir, root string) (string, error)` returns `<root>/archive/<base name of specDir>`. It returns a `*SaveError` when that path already exists (message `archive conflict: <path> already exists`) or cannot be checked. It writes nothing.
- `afspec.MoveUnderArchive(specDir, root string) (string, error)` moves the package directory to `ArchiveDestination(specDir, root)` and returns the new directory. Before it moves anything:
  - it loads the package with `LoadSpec`, and returns a load failure as the `*LoadError` it is;
  - it refuses a package whose status is neither `archived` nor `superseded` with a `*LifecycleError`, because only those statuses live under `archive/`;
  - it refuses a taken destination with the `*SaveError` from `ArchiveDestination`.

  It then creates `<root>/archive/` (mode 0755) if it is missing and renames the directory. Either failure is a `*SaveError`, and the package stays where it was. It rewrites no file, so the status, `updated_at` and content stay the same.
- `MoveToArchive` keeps its signature and its documented effect. It still transitions any package that is not already `archived`, a superseded one included. It now checks `ArchiveDestination` before the transition writes anything, so a taken destination leaves the package untouched. It moves the package with `MoveUnderArchive`. Its comment no longer promises copy-then-delete. The existing `TestMoveToArchive` passes unchanged.

### 2. The form and its usage errors

- `speca <spec> --status S` with S one of `active`, `sealed`, `superseded` or `archived`. `--by <spec>` names the replacing package and is required with `superseded`.
- These are usage errors: exit 2, stage `usage`, category `usage`, nothing written.
  - `--status draft`. The message says that no move leads back to draft and that a package whose intent must change is superseded by a new one. `fix_hint` is `{flag: "--status", valid: [the four]}`.
  - Any other value not in the set, with the same `fix_hint`.
  - `--status superseded` without `--by`.
  - `--by` without `--status superseded`.
  - A `--by` reference the shared resolver refuses. The message starts `--by:`.
  - A `--by` that resolves to the same directory as the input.
  - A `--by` package whose `spec_id` equals the input's. This is detected after loading.
  - `--status` together with another operation (`--task`/`--state`, `--validate`, `--list`).
  - `--status` with no positional input.
- `--dry-run` and `--force`, the flags `11_speca_task_states` adds, apply to this form as described below. The help, the flag descriptions and `--schema` describe the form. `--schema` gives `status` as an enum of the four values, and `cmd/speca/testdata/schema.golden.json` is regenerated.

### 3. Checks made before any write

These run in this order for every `--status` call, and they also run under `--dry-run`. The first one that refuses ends the call with nothing written and no `result`.

1. **Load.** The package, and the `--by` package, are loaded with `LoadSpec`. A failure exits 1 at stage `load`, category `input`, naming the directory (and `--by` when it is the replacing package). A package whose `schema_version` is neither absent nor 2 is refused at the same stage and category: `speca` changes the state of format version 2 packages only. `--force` does not override either case.
2. **The move.** A package is *under `archive/`* when the directory that holds it is named `archive`.
   - When the target equals the current status, the status is `archived` or `superseded`, and the package is not under `archive/`, the call is a *move-only* call. It is the resume of an interrupted call (Requirement 8).
   - Otherwise, when `afspec.ValidTransition(status, target)` is false, the call is refused at stage `status`, category `lifecycle`. The message names the package, its status and the target:
     - it says "already <status>" when target and status are equal;
     - otherwise it names the shortest sequence of allowed moves from the status to the target, when one exists. For example: active → archived is "seal it first (`speca <spec> --status sealed`), then archive it".

     `fix_hint` is `{flag: "--status", valid: allowed_status}` when `allowed_status` is not empty, and absent when it is. `--force` does not override this refusal.
3. **The target's own checks** (Requirements 4, 5 and 7).
4. **Unknown keys.** This check applies to moves that rewrite the package's files: sealing, archiving and superseding, but not move-only calls. Activation is already covered by its validation check. When `Validate` reports any `unknown_field` error, the call is refused at stage `status`, category `lifecycle`, naming the files and keys, with `fix_hint.flag` set to `--force`. Under `--force` the call goes ahead, and a `high` `spec_invalid` warning names the keys the write drops.
5. **The archive destination.** For a move that ends under `archive/`, from a package not already there, the call checks `afspec.ArchiveDestination(dir, <the directory holding the package>)`. A taken destination is refused at stage `archive`, category `disk`, `resumable: false`, naming the existing directory.

Every package `speca` writes has `updated_at` set to the current UTC time in RFC 3339 before it is written. A move-only call rewrites no file and leaves `updated_at` as it is.

### 4. Activation (`draft → active`)

- When `afspec.HasIntent(PRDBody)` is false, the call is refused at stage `status`, category `lifecycle`. The message says to add a `## Intent` section to `prd.md`, spelled exactly so on a line of its own. `--force` does not override this.
- When `Validate` is not valid, the call is refused at stage `validate`, category `invalid_spec`. The message names the failing rules, and `fix_hint.flag` is `--force`. Under `--force` the call goes ahead with a `high` `spec_invalid` warning naming the rules.
- The write is `Transition("active", dir)`. The package stays where it is. The result carries the new `intent_hash`; under `--dry-run` it carries the hash `ComputeIntentHash` gives.

### 5. Sealing (`active → sealed`)

- When the package has intent drift (an `intent_hash` is set, and the body hashes to a different value or has no `## Intent`), the call is refused at stage `status`, category `lifecycle`, with no `fix_hint`. The message says to restore the intent that was activated, or to write a new package that supersedes this one. `--force` does not override this.
- When any task is `pending` or `in_progress` (optional tasks included), the call is refused at stage `status`, category `lifecycle`. The message names each open task with its state and says to finish or drop it. `fix_hint.flag` is `--force`. Under `--force` the call goes ahead with a `high` `tasks_open` warning that lists the open tasks and their states.
- The write is `Transition("sealed", dir)`. The package stays where it is.

### 6. Archiving (`draft`, `sealed` or `superseded` → `archived`)

- **Package not under `archive/`:** `Transition("archived", dir)`, then `MoveUnderArchive(dir, <its holding directory>)`. Changes: `status`, then `moved`.
- **Package already under `archive/`:** `Transition` only. Change: `status`.
- **Move-only call** (status `archived`, not under `archive/`): `MoveUnderArchive` only. Change: `moved`.

### 7. Superseding (`sealed → superseded`, with `--by`)

- The replacing package must be `draft`, `active` or `sealed`. Otherwise the call is refused at stage `status`, category `lifecycle`, with `fix_hint.flag` `--by`. `--force` does not override this.
- The writes happen in this order:
  1. `Supersede(<replacing spec_id>, dir)` writes the banner and the status.
  2. The old `spec_id` is added at the end of the replacing package's `supersedes`, unless it is already there.
  3. `MoveUnderArchive(dir, <holding directory>)` moves the package, unless it is already under `archive/`. Its status stays `superseded`.
- Step 2 writes with `Save`. It is attempted only when the replacing package is `draft`, or `active` without intent drift, and has no `unknown_field` errors. In any other case, or when `Save` fails, the call still succeeds. A `low` `supersedes_not_recorded` warning names the replacing package's directory, the id that was not recorded, and the reason. `--force` does not extend to the replacing package.
- **Move-only call** (status `superseded`, not under `archive/`, `--by` given): step 1 is skipped, step 2 runs if the id is still missing, and step 3 runs.
- Changes, in order: `status`, `supersedes`, `moved`.

### 8. Failures while writing

- A `*SaveError` from `Transition`, `Supersede` or the step-2 `Save` of the target package exits 1 at stage `write`, category `disk`, with the library's message and no `result`.
- A `*LifecycleError` or `*IntentError` that reaches the write despite the checks exits 1 at stage `status`, category `lifecycle`.
- `MoveUnderArchive` can fail after the status was written. That call exits 1 at stage `archive`, category `disk`, with `resumable: true`; `retryable` stays as `RetryableFor` gives it (false). The message names the directory the package is in now and its new status, and says that running the same call again moves it. In this case `result` is present: it holds the package as it is now (`spec_dir` where it is, the new status) and `changes` with what was written. `next` holds the same call against the current `spec_dir`, with `--by` for superseding.
- When reading the package back after a successful write fails, the call exits 1 at stage `load`, category `input`, and `result` still carries `changes`.

### 9. The result, `summary` and `next`

- **The result.** On success, the result holds the package as the report form of `10_speca_state_report` reports it. It is read back from disk with `LoadSpec` from where the package now is, so `spec_dir` is the directory after any move, shown by the display rule. Under `--dry-run` it is computed from the state the package would have, held in memory. `changes` lists each write in order:
  - `{"kind": "status", "from": "active", "to": "sealed"}`
  - `{"kind": "supersedes", "spec": "<replacing spec_id>", "added": "<old spec_id>"}`
  - `{"kind": "moved", "from": "<display path>", "to": "<display path>"}`
- **`--dry-run`.** The result has `dry_run: true`, and the file tree and every modification time stay unchanged.
- **`summary`.** One sentence per call, for example:
  - `speca: activated 10_speca_state_report`
  - `speca: sealed 09_adversarial_prd_review`
  - `speca: archived 03_x, moved to .specs/archive/03_x`
  - `speca: superseded 03_old_search by 12, moved to .specs/archive/03_old_search`
  - `speca: moved 03_x to .specs/archive/03_x`

  Under `--dry-run` the sentence ends ` (dry run; nothing written)`. After an archive-stage failure, the sentence says that the status was written and the move was not.
- **`next` after a call:**
  - after `active`: the claim of `next_task` when that task is ready (`speca <spec_dir> --task N --state in_progress`);
  - after `sealed`, `archived`, `superseded` or a move-only call: nothing;
  - under `--dry-run`: the same call without `--dry-run`;
  - on a refusal: nothing;
  - on an archive-stage failure: the re-run described in Requirement 8.
- **`next` in the report form** (`speca <spec>`) gains two suggestions:
  - for a `draft` whose `Validate` is valid and whose body has a `## Intent`: `speca <spec_dir> --status active`, listed before any task suggestion;
  - for an `active` package with no task `pending` or `in_progress`: `speca <spec_dir> --status sealed`.

  Because these conditions match the checks above, a suggestion never leads to a refusal. The `--list` form's `next` is unchanged.

### 10. Output types, warnings and documentation

- `envelope.Change` gains `spec` and `added` (`omitempty`, with `description` tags), and its documentation names the kinds `status`, `moved` and `supersedes`.
- `envelope` gains `WarnTasksOpen = "tasks_open"` and `WarnSupersedesNotRecorded = "supersedes_not_recorded"`. `internal/cli/warn.go` gains their aliases and `warnStages` entries of `"status"`. The documentation of `ErrorInfo.Stage` lists `status` and `archive`. `envelope` still imports only the standard library.
- `cmd/speca`'s `Result` implements `Resumable() bool`. It is true only for the archive-stage failure.
- Documentation updated in the same change, following the steering:
  - `docs/speca.md`: the `--status` form, its checks, errors and warnings, `--by`, and resuming;
  - `README.md` and `docs/architecture.md`: lifecycle changes now have a command;
  - `docs/api.md`: `ArchiveDestination`, `MoveUnderArchive`, the `Change` fields and the warning codes;
  - `afspec/README.md`: the lifecycle table;
  - `docs/generated/`, regenerated with `make doc`.

  No test checks the docs.

## Design Decisions

1. **The status write and the directory move are separate library calls.** `MoveUnderArchive` only moves, and the caller writes the status first with `Transition` or `Supersede`. This keeps `superseded` by construction, lets `speca` tell a failed status write from a failed move, and makes the resume a plain "move only".
2. **`MoveUnderArchive` refuses any status other than `archived` or `superseded`.** §9 puts only those statuses under `archive/`, and the library should not let a caller break that.
3. **`MoveToArchive` keeps turning a superseded package into `archived`.** `superseded → archived` is a move the table allows, and embedders rely on the documented effect. Its one change is to check the destination before writing, which only makes a failure safer.
4. **The move goes under the directory that holds the package, not under `--specs-dir`.** The two are the same for every package in the spec root. For a package reached by a path outside the root, this keeps it in its own tree instead of moving it into another one.
5. **"Under `archive/`" means the holding directory is named `archive`.** This needs no spec-root resolution. A package hand-placed in some other `archive/` is treated the same way.
6. **The archive failure is `resumable: true`, not `retryable: true`.** `Run.Envelope` always sets `retryable` from `RetryableFor` (only `api` and `aborted`). A failed rename (EXDEV, permissions) does not succeed just because the call is repeated. "Re-running the same input continues the work" is exactly what happens here.
7. **Intent drift blocks sealing, even under `--force`.** `Save` refuses every write of a drifted package. Sealing through `Transition` would be the one write that gets past that rule, and it would make the drift permanent.
8. **`--force` skips only the checks an operator might reasonably override:** the validation check on activation, the open-task check on sealing, and the unknown-key check. The lifecycle table, the `## Intent` rule, drift, the replacing package's status, a taken destination and format version 1 stay hard refusals, because each one is a rule of the format or a fact on disk.
9. **Unknown keys are guarded before a rewrite, at stage `status`, category `lifecycle`.** `saveToDisk` drops them silently. `11_speca_task_states` guards its writes the same way, and the input's error table puts every check refusal under `status`/`lifecycle`.
10. **The order of supersede writes is banner and status, then `supersedes`, then the move.** The rename, which is the step most likely to fail, comes last. The resume then has only the move left, and the step-2 record is idempotent.
11. **The replacing package must be `draft`, `active` or `sealed`.** A package that is already superseded or archived cannot sensibly replace anything.
12. **A missing `supersedes` record is a warning, not a failure.** The old package's change is the one the caller asked for. A sealed or drifted replacement cannot be saved, and the warning names the file.
13. **`speca` sets `updated_at` itself.** The library's `Transition` and `Supersede` do not set it, and format §4 requires it on every save. The format is RFC 3339 UTC, as in `examples/tasks`.
14. **Activation validates with the package's own `Validate`, not the cross-spec rules.** This is the verdict `spec` uses before it activates, and the one the report form calls `valid`.
15. **`--status draft` is a usage error.** No row of the table leads back to `draft`. The reason is clearer as a message than as a lifecycle refusal from every state.
16. **The refusal message names the shortest allowed path.** "Seal first, then archive" is what the agent needs, and a breadth-first search over `ValidTransition` gives it without a second table.
17. **The result is read back from disk after a write.** A copy kept in memory would still carry the unknown keys a forced write dropped, and the old directory.
18. **Activation and seal suggestions copy the checks' own conditions.** A suggested call therefore never comes back as a refusal.
19. **No lifecycle moves on format version 1 packages.** A write through v2 types would destroy their tasks and tests. The input rules out conversion.

## Dependencies

| Spec | Relationship | Reason |
|---|---|---|
| `10_speca_state_report` | depends on | The `speca` command shell, the shared resolver and display rule in `internal/specpkg`, the report builder whose fields this form returns, `envelope.AdminResult`, `DecodeSpeca` and `TaskCounts`, and ADR 10 |
| `11_speca_task_states` | depends on | The `--force` and `--dry-run` flags, `envelope.Change` with `changes` and `dry_run` on `AdminResult`, `envelope.CategoryLifecycle`, the pre-write guard against unknown keys, and the task claim form that `next` suggests after activation |
