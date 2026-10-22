# Change the state of a package with its own command

## Intent

A spec package carries two kinds of state: the lifecycle status in its PRD
frontmatter (`draft`, `active`, `sealed`, `superseded`, `archived`) and the
state of every task in `tasks.json` (`pending`, `in_progress`, `done`,
`dropped`). Both have a state machine, and `afspec` enforces both. Neither
has a command. An agent that finishes a task today records it by editing
`tasks.json` with grep and sed, and an agent that finishes a package leaves
it `active` because sealing it is library API it cannot call from a shell.
`speca`, the spec admin command, puts the library's lifecycle and task
operations behind the interface `spec` and `specr` share, so an agent changes
a package's state with the one call that the format allows, and is refused,
with the reason and the allowed moves, when it asks for one it does not.

## Goals

- An agent moves one or more tasks through the state machine of format v2
  §8.3.1 with one call, `speca <spec> --task N --state S`, and the change is
  written through `afspec` with the same guards the library gives an
  embedder.
- An agent moves a package through its lifecycle with one call,
  `speca <spec> --status S`: `draft → active` (hashing the intent),
  `active → sealed`, `sealed → superseded` (with `--by`, the replacement),
  and `→ archived`, which also moves the package under `archive/`.
- A bare `speca <spec>` reports the package's state without changing it:
  its status, whether it validates, every task with its state, the next task
  that is ready, the moves allowed from here, and the packages it depends on
  and that depend on it.
- `speca --list` reports every package under the spec root the same way, one
  line each, so an agent choosing what to work on reads one object.
- `speca <spec> --validate` reports the format's verdict on a package: the
  schemas, the cross-file rules C1 to C11 and the cross-spec rules, every
  error naming its rule.
- Every refusal says why and what is allowed instead, in `error.fix_hint`,
  and every change is listed in the result, so a caller never has to read
  the files to learn what the call did.
- The command follows the interface of `spec` and `specr`: exactly one
  positional input, exactly one JSON object on stdout, forms as flags, the
  help on stderr, `--schema`, `--version`, and the three exit codes.

## Non-goals

- Editing the content of a package: requirements, criteria, tests, tasks,
  the PRD body or `architecture.md`. `speca` changes state, not substance.
  The mutation API of `afspec` stays library API.
- Creating a package. `spec` writes packages from an idea; a hand-written
  scaffold through `CreateSpec` is a possible later form.
- Re-hashing a changed `## Intent` section on an active package. The intent
  is protected after activation by the format's §9; a changed intent is a
  new package that supersedes the old one.
- Changing `owner`, `tags`, `source` or `supersedes` directly. The one write
  to `supersedes` is the one superseding makes.
- A lock between agents. Two agents that write one package at once get the
  last writer's `tasks.json`, as they do with the library.
- Rendering. `specr` renders; `speca` reports state and changes it.
- Any model call, credential or network access. `speca` runs in
  milliseconds on the files alone.
- Converting a format version 1 package. `speca` reads version 2 only, like
  `specr`.

## Background

`afspec` implements both state machines. `Spec.Transition` moves a package
between the lifecycle states of `lifecycle.go`'s table, computes the intent
hash on `draft → active` and refuses a body without `## Intent`;
`Spec.Supersede` prepends the deprecation banner to a sealed package;
`MoveToArchive` transitions a package to `archived` and renames its
directory under `<root>/archive/`. `TasksV2Json.TransitionTask` moves one
task along §8.3.1 and refuses anything else; `CompleteTaskStates` and
`ResetTaskStates` set `done` and `pending` directly, bypassing the machine.
`Spec.Save` refuses a sealed, superseded or archived package, a changed
identity on an active one, and intent drift. `examples/tasks/main.go` shows
the whole loop an agent runs: pick the next ready task, claim it, render the
package scoped to it, finish it or release it.

None of that is reachable from a shell. The README says so: lifecycle
management "has no CLI surface; it's library API for an embedder to call".
agent-fox's `impl` tool is such an embedder. An agent driving the `af-impl`
skill, or a Claude Code session told to implement spec 16, is not. It reads
`tasks.json`, finds the task's `"state": "pending"` and writes
`"in_progress"` over it with sed, which the library's atomic save, its
deterministic field order and its state machine were built to prevent. The
lifecycle is worse off: spec 09 of this repository has all eleven tasks
`done` and is still `active`, because nothing an agent runs can seal it.

ADR 05 built `specr` for the same reason, for the read side: an agent with a
shell needed the library's renders, and got them behind the interface
`spec` and agent-fox's tools share. `specr` left its reference resolver
(`cmd/specr/locate.go`) in the command, and its result type in `envelope`,
where a third command can reach it.

Two gaps in the library matter here. `MoveToArchive` transitions whatever
it moves to `archived`, so a superseded package moved with it loses its
`superseded` status, which format v1.3 §9.1 (unchanged in v2 §9) keeps when
the folder moves. And nothing in the library computes which task is ready
next; the tasks example does it in `pick`.

## Requirements

### The interface

- `speca [flags] <spec>` takes exactly one positional input, a reference to
  a package, resolved as `specr` resolves it: a path to the package
  directory or to a file inside it, then a directory name, number or name
  under the spec root (`--specs-dir`, else `$AF_SPEC_DIR`, else
  `<dir>/.specs`). The resolver is one piece of code shared by both
  commands, not a copy. An archived package is reached by its path only, as
  with `specr`.
- Exactly one JSON object goes to stdout on every program-driven path, the
  envelope of `internal/cli`, with `tool` set to `speca`. The help goes to
  stderr on `-h`, `--help` and a bare invocation; a bare invocation also
  writes a usage envelope when stdout is not a terminal. `--version` prints
  the build identity. `--schema` prints the self-description (flags, input,
  result, exit codes) like `specr --schema`, checked in as a golden file.
- The forms are flags. At most one operation is named per call; two are a
  usage error:

  | Form | What it does |
  |---|---|
  | `speca <spec>` | reports the package's state; changes nothing |
  | `speca <spec> --task N --state S` | moves task N (or several, comma-separated) to state S |
  | `speca <spec> --status S` | moves the package to lifecycle state S |
  | `speca <spec> --status superseded --by <spec>` | supersedes the package with another |
  | `speca <spec> --validate` | the full validation report |
  | `speca --list` | every package under the spec root; `--all` adds `archive/` |

- `--dry-run` runs any form that writes with nothing written: the result is
  what it would have been, with `dry_run: true`, and a refusal is still a
  refusal.
- `--force` skips the command's own guards, named below, and nothing the
  library refuses on its own, except where the library itself offers a
  direct setter.
- `--specs-dir` and `--dir` mean what they mean for `specr`. `-`, a URL and
  a multi-line input are usage errors: `speca` works on a package on disk.
- `--list` takes no positional input; one is a usage error. Every other
  form takes exactly one.

### The report

`speca <spec>` reads the package and writes:

- its identity: `spec_dir` (relative to `--dir` when inside it), `spec_id`,
  `spec_name`, `title`, `status`, `created_at`, `updated_at`,
  `intent_hash` when set, `supersedes` and `tags` when not empty;
- `valid`, from `Spec.Validate`, with the failing rules named when false;
- `intent_drift`: true when the package is active and its `## Intent`
  section no longer hashes to `intent_hash`. Such a package refuses every
  save, so a caller learns here why its next call will fail;
- `tasks`: every task in order as `specr`'s `TaskInfo` (`id`, `title`,
  `kind`, `state`, `optional`, `depends_on`), plus `ready`: true for a
  pending task whose `depends_on` are all done;
- `task_counts`: `pending`, `in_progress`, `done`, `dropped`, `total`;
- `next_task`: the task an agent should work on now: the first task
  `in_progress`, else the first ready pending task; absent when there is
  none. This is the rule of `examples/tasks`'s `pick`, moved into the
  library so the example, the command and an embedder agree;
- `allowed_status`: the lifecycle states the package may move to from its
  status, per `afspec.ValidTransition`;
- `depends_on` and `dependents`: the other packages, by `spec_id`, from
  `afspec.BuildDependencyGraph` over the spec root. A root that cannot be
  read makes these absent with a `specs_dir_unreadable` warning.

`next` suggests what to run: `specr <spec> --task N` for the task in
progress; `speca <spec> --task N --state in_progress` for the next ready
task when none is in progress; `speca <spec> --status sealed` when the
package is active and every task is `done` or `dropped`; `speca <spec>
--status active` when it is a draft that validates.

### Task states

- `--task` takes one or more task ids, comma-separated or repeated, or
  `next`, which resolves to `next_task` and is a usage error when there is
  none. `all` names every task of the plan. `--task` without `--state`, and
  `--state` without `--task`, are usage errors. An id the plan does not have
  is a usage error whose `fix_hint.valid` lists the ids, as for `specr`;
  nothing is changed.
- `--state` takes `pending`, `in_progress`, `done` or `dropped`. Anything
  else is a usage error listing the four.
- Each named task is moved with `TasksV2Json.TransitionTask`. A move the
  machine does not allow is refused for the whole call: no task changes,
  the error names the task, its state and the target, and `fix_hint.valid`
  lists the states the task may move to. So a task another agent already
  claimed refuses a second claim (`in_progress → in_progress` is not a
  move), which is the one check two agents need.
- With `--force`, a target of `done` or `pending` is set with the library's
  `CompleteTaskStates` or `ResetTaskStates` instead, bypassing the machine,
  and the result carries a `high` `task_state_forced` warning naming each
  task so moved. `--force` does not bypass the machine for `in_progress` or
  `dropped`: the library has no setter for them, and the command writes
  none.
- Moving a task to `in_progress` whose `depends_on` are not all `done` goes
  ahead with a `low` `task_not_ready` warning naming the unfinished
  dependencies. It is a warning, not a refusal, because an agent sometimes
  works out of order on purpose and the plan's order is advice, not a rule
  of the format.
- A package that does not validate still takes a task-state change, with a
  `low` `spec_invalid` warning naming the rules, as `specr` renders one.
  Recording work is not made to wait on a dangling id elsewhere.
- The change is written with `Spec.Save`, which sets `updated_at` and
  refuses a sealed, superseded or archived package and an active one with
  intent drift. Such a refusal is reported as it comes from the library,
  with `fix_hint` naming the way out where there is one (`--status` for a
  sealed package is none; intent drift is none).
- The result lists every change as `{"kind": "task", "id": N, "from":
  "pending", "to": "in_progress"}`, and reports the package as the report
  form does, after the change. `summary` reads, for example,
  `speca: 16_indexed_code_search task 3 pending → in_progress`.
- `next` after a move to `in_progress` suggests `specr <spec> --task N`;
  after `done`, the next ready task's claim, or sealing when none is left;
  after `pending` (a release), the same task's claim.

### Lifecycle

- `--status` takes `active`, `sealed`, `superseded` or `archived`. `draft`
  is not a target of any move and is refused as a usage error saying so.
- A move the lifecycle table does not allow is refused by
  `Spec.Transition`; the error names the status and the target, and
  `fix_hint.valid` lists `allowed_status`. `active → archived` is such a
  refusal, and its message names the path: seal first, then archive.
  `--force` does not bypass the table: the table is the format's §9.
- **`active`** (from `draft`): the command validates the package first and
  refuses one that does not validate, naming the rules, as `spec` refuses
  to activate one. `--force` activates it anyway with a `high`
  `spec_invalid` warning. A body without `## Intent` is refused by the
  library, and the message says what to add. On success the result carries
  the new `intent_hash`.
- **`sealed`** (from `active`): the command refuses while any task is
  `pending` or `in_progress`, naming them; `--force` seals anyway with a
  `high` `tasks_open` warning listing them.
- **`archived`** (from `draft`, `sealed` or `superseded`): the package is
  transitioned and its directory moved under `<root>/archive/` with the
  library's move. A package already under `archive/` is only transitioned.
  A name already present under `archive/` is refused before anything is
  written. A move that fails after the status was written is reported at
  stage `archive`, category `disk`, `retryable: true`, naming the directory
  where the package now is with its new status: a second run finds it
  archived and only moves it.
- **`superseded`** (from `sealed`): requires `--by <spec>`, a reference to
  the replacing package resolved as the input is, which must name a
  different package; without it the call is a usage error. The old package
  gets the banner and the status through `Spec.Supersede`, then its
  directory is moved under `archive/` keeping the status `superseded`, as
  §9.1 says. The replacing package's `supersedes` gains the old `spec_id`
  when it is not already there and the package can be saved (a draft or an
  active one); when it cannot (sealed, or intent drift), the old package is
  still superseded and the result carries a `low` `supersedes_not_recorded`
  warning saying which package to edit by hand. The library needs the move
  that keeps `superseded`; today's `MoveToArchive` would overwrite it with
  `archived`.
- Every lifecycle change lists what it did: `{"kind": "status", "from":
  "active", "to": "sealed"}`, `{"kind": "moved", "from": ".specs/03_x",
  "to": ".specs/archive/03_x"}`, `{"kind": "supersedes", "spec": "12",
  "added": "03"}`. `spec_dir` in the result is where the package is after
  the call. `summary` reads, for example,
  `speca: sealed 09_adversarial_prd_review` or
  `speca: superseded 03_old_search by 12, moved to .specs/archive/03_old_search`.
- `next` after `active` suggests the first task's claim; after `sealed`,
  nothing; after `archived` and `superseded`, nothing.

### Validation

- `speca <spec> --validate` runs `Spec.Validate` and, over every package
  the spec root holds, `afspec.ValidateCrossSpec`, and reports
  `validation` as `spec`'s result does: `valid`, every error with its rule,
  artifact, entity and message, and the warnings. It also reports the
  derived `traceability` (`criteria_covered`, `criteria_uncovered`,
  `paths_covered`, `paths_uncovered`, `tests_unowned`) in the shape
  `spec`'s result uses.
- An invalid package is exit 0 with `valid: false`: the call did what it
  was asked. A package that cannot be loaded is exit 1 at stage `load`.
- A spec root that cannot be read skips the cross-spec rules with a
  `specs_dir_unreadable` warning.

### The list

- `speca --list` discovers every package under the spec root with
  `afspec.DiscoverSpecs`, loads each, and reports `packages`, in number
  order, each with `spec_dir`, `spec_id`, `spec_name`, `title`, `status`,
  `valid`, `task_counts` and `next_task`. A package that fails to load is
  listed with `load_error` and nothing else, and does not fail the call.
- `--all` adds the packages under `archive/`, each with `archived: true`
  (the directory, not the status: a superseded package under `archive/` is
  `archived: true` with `status: superseded`).
- `summary` counts them: `speca: 9 packages (2 draft, 5 active, 2 sealed)`.
- `next` suggests, for the first active package with a ready task and
  nothing in progress, that task's claim.

### The output

The result is a public type in `envelope`, `AdminResult`, decoded with
`envelope.DecodeSpeca`, beside `SpecResult` and `RenderResult`. One type
serves every form; the fields a form does not produce are absent:

| Field | Form | Meaning |
|---|---|---|
| `spec_dir`, `spec_id`, `spec_name`, `title`, `status`, `created_at`, `updated_at`, `intent_hash`, `supersedes`, `tags` | all but `--list` | the package, after the call |
| `valid`, `intent_drift` | all but `--list` | as in the report |
| `tasks`, `task_counts`, `next_task`, `allowed_status`, `depends_on`, `dependents` | all but `--list` and `--validate` | as in the report |
| `changes` | `--task`, `--status` | what was written, in order; under `--dry-run`, what would have been |
| `dry_run` | `--task`, `--status` | true under `--dry-run` |
| `validation`, `traceability` | `--validate` | the full verdict |
| `packages` | `--list` | one entry per package |

`TaskInfo` gains `ready`; `envelope` gains `AdminResult`, `AdminEnvelope`,
`DecodeSpeca`, `Change`, `TaskCounts`, `PackageInfo`, the new warning codes
and the error category `lifecycle`. Nothing already in `envelope` changes
meaning.

### Errors and exit codes

| `stage` | `category` | When | Exit |
|---|---|---|---|
| `usage` | `usage` | a flag that cannot hold, two operations, no or two packages, an unknown task id, a `--state` or `--status` value not in its set, `--by` missing or naming the input itself | 2 |
| `load` | `input` | `afspec.LoadSpec` refused the package, or the `--by` package | 1 |
| `task` | `lifecycle` | the task state machine refused a move; nothing was written | 1 |
| `status` | `lifecycle` | the lifecycle table refused a move, a guard refused one (open tasks on seal, no `## Intent` on activate), or `Save` refused the write (sealed, intent drift) | 1 |
| `validate` | `invalid_spec` | activation of a package that does not validate, without `--force` | 1 |
| `write` | `disk` | the package could not be written; nothing changed on disk, the save is atomic per file | 1 |
| `archive` | `disk` | the directory could not be moved after its status was written; `retryable: true` | 1 |

`lifecycle` is a new error category: the package was read, the call was
well-formed, and what it asked for is not a move the state allows. It is not
`usage`, which means nothing was read, and not `invalid_spec`, which is
about the package's content. `fix_hint.valid` carries the allowed targets
wherever there are any.

| Code | `status` | Meaning |
|---|---|---|
| `0` | `done` | the report, list or validation was made, or the change was written (or, under `--dry-run`, computed) |
| `1` | `failed` | the package could not be read, the move was refused, or the write failed; the stage says which |
| `2` | `usage` | usage error; nothing was read, nothing was written |

### Warning codes

| Code | Severity | Stage | When |
|---|---|---|---|
| `spec_invalid` | low, or high under `--force` activation | `validate` | the package does not validate and the call went ahead |
| `task_not_ready` | low | `task` | a task moved to `in_progress` has a dependency not `done` |
| `task_state_forced` | high | `task` | `--force` set a state the machine would have refused |
| `tasks_open` | high | `status` | `--force` sealed a package with tasks not `done` or `dropped` |
| `supersedes_not_recorded` | low | `status` | the replacing package could not be written |
| `specs_dir_unreadable` | low | `discover` | the spec root could not be read for the graph or the cross-spec rules |

### The library

Three additions to `afspec`, each a small function beside what exists:

- a move under `archive/` that keeps a `superseded` status (and still
  transitions a draft or sealed package to `archived`), which `MoveToArchive`
  then shares its code with;
- `NextTask` (or a name in the library's idiom) on `TasksV2Json`: the task
  in progress, else the first pending task whose dependencies are all done,
  the rule of `examples/tasks`'s `pick`, which then calls it;
- `ReadyTasks` or an `IsReady(task)` helper, so `ready` in the report and
  `next_task` agree by construction.

The resolver in `cmd/specr/locate.go` moves to a package both commands
import (`internal/cli`, or a package of its own under `internal/`), with
its error messages naming the calling tool.

### Documentation and build

- `make build` installs `speca` beside `spec` and `specr`, stamped the same
  way; `--version` prints the same identity.
- `docs/speca.md` documents the command as `docs/specr.md` documents
  `specr`: the forms, the input, the report, the output, the errors, the
  warnings, the flags, `--schema`, and how a skill calls it. The README's
  claim that lifecycle management has no CLI surface is replaced by a
  section on `speca`; `docs/architecture.md` adds the command and its
  package graph; `docs/api.md` adds `AdminResult` and `DecodeSpeca`;
  `docs/generated/` is regenerated.
- The decision is recorded as an ADR, as ADR 05 recorded `specr`.
- The `af-impl` skill's "one task at a time" step, which `docs/specr.md`
  quotes, gains the two calls an agent makes around the render: claim the
  task before it, record it done after.

## Design Decisions

1. **Forms are flags, not verbs.** ADR 05 settled this for `specr`, and
   agent-fox's ADR 03 left no room for a second interface. `speca --task 3
   --state done` reads as a statement of the new state, which is what the
   call is. Four operations, each one flag, with at most one per call, keep
   the flag set as small as `specr`'s.
2. **A bare `speca <spec>` is a report, not an error.** `specr <spec>` with
   no flags renders the whole package; `speca <spec>` with no flags reports
   its state. An agent that knows only the command's name runs it on a
   package and learns the tasks, the next one, and the moves allowed, with
   `next` spelling out the call. Nothing an agent runs by mistake changes a
   file.
3. **The library does every change; the command adds guards.** Two state
   machines in two places would drift. `speca` calls `TransitionTask`,
   `Transition`, `Supersede` and the archive move, and adds only what a
   human operator would check first: a package validates before it is
   activated, its tasks are done before it is sealed, a task's dependencies
   are done before it is claimed. The first two refuse, the third warns.
4. **`--force` skips the command's guards and uses the library's own
   bypasses, nothing more.** `CompleteTaskStates` and `ResetTaskStates` exist
   for an embedder that knows better; `--force` with `done` or `pending`
   reaches them, and nothing else, because the command never writes a state
   by hand. The lifecycle table has no bypass in the library and gets none
   here: `active → archived` is two calls, and the error says so.
5. **A refused move is a failure with its own category, not a usage
   error.** The call was well-formed and the package was read; what it
   asked for is not allowed from this state. `usage` would say nothing was
   read, which is false, and would hide the one thing the caller needs,
   the allowed targets, which `fix_hint.valid` carries.
6. **The state machine is the compare-and-set.** Two agents that both
   claim task 3 cannot both succeed: the second finds it `in_progress`, and
   `in_progress → in_progress` is not a move. A lock would cover the race
   between the read and the write, which lasts microseconds on one file,
   and would need a lock file every embedder must honour. Not worth it.
7. **A task-state change goes ahead on an invalid package; activation does
   not.** An agent records work as it happens, and a dangling test id in
   another part of the package should not block the record. Activation is
   the moment the format hashes the intent and the package becomes the
   contract; `spec` refuses to activate an invalid package and `speca`
   does the same.
8. **Archiving moves the directory; superseding too.** The format's §9 says
   `archived` means moved under `archive/`, and §9.1 moves a superseded
   package as well. A status with the package left in place would list it
   among the live packages, where `spec` numbers new packages after it and
   `specr` resolves it by name. The library gains the move that keeps
   `superseded`; the existing one overwrites it.
9. **Superseding writes the replacing package's `supersedes` when it can,
   and says so when it cannot.** §9.1 step 1 is the new package's entry;
   leaving it to the agent means it is forgotten. A sealed replacing
   package cannot be saved, and a replacement that is already sealed is
   rare enough that a warning naming the file is the right size.
10. **`--validate` is exit 0 on an invalid package.** The question was
    "does it validate?" and the answer is in `valid`. Exit 1 is for a call
    that could not do what it was asked.
11. **`next_task` moves into the library.** The report, `next`, the list and
    the tasks example all need "the task to work on now", and an embedder
    such as agent-fox's `impl` has the same question. One function, one
    answer.
12. **No `--status draft`.** No row of the table leads back to `draft`, and
    a package whose intent must change is superseded, not reopened. Refusing
    it as a usage error, with the reason, is clearer than letting the
    library refuse it as a move from every state.
13. **`--dry-run` on every writing form.** An agent about to seal or archive
    a package can see the changes and the refusals first at no cost, as it
    can with `spec --dry-run`.

## Dependencies

- **ADR 05, render a package with its own command.** `speca` is the writing
  counterpart of `specr`, on the same interface, the same resolver and the
  same envelope.
- **ADR 01, run the spec command on the AgentKit driver.** The shell in
  `internal/cli` (`Run`, `Emit`, `SplitArgs`, the envelope, the fix hints,
  the schema generator) is what `speca` is built on; it links no model.
- **Format v2 §8.3.1 and §9.** The two state machines `speca` exposes; the
  command adds no state to either.
- **`afspec`** gains the archive move that keeps `superseded`, `NextTask`
  and the readiness helper; `envelope` gains `AdminResult` and its types;
  `cmd/specr` gives up its resolver to a shared package.
