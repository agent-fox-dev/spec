# The `speca` command

```
speca [flags] <spec>                     a specification package → its state
speca --list [--all]                     every package under the spec root → one entry each
speca <spec> --task T --state S          change the state of tasks of a package
speca <spec> --status S [--by <spec>]    change the lifecycle status of a package
```

`speca` is the spec admin command. It reads a version 2 package through
`afspec` and reports what an agent with a shell needs to know about it: its
lifecycle status, whether it validates, every task with its state and whether
it is ready, the task to work on next, the lifecycle moves allowed from where
it is, and the packages it depends on and that depend on it. It is built for an
agent to call — from a prompt, or from a skill's instructions — and follows the
same interface as `spec` and `specr`: **exactly one positional input**
(none with `--list`), **exactly one JSON object** on stdout on every
program-driven path, the help on stderr, and one exit-code table. The forms are
flags, not subcommands.

`spec` writes a package from an idea, `specr` renders one, `speca` reports and
changes its state. Each command's help and each `--schema` description say so,
in the sentence `cli.SiblingCommands`.

The report, `--validate` and `--list` read the packages and change nothing.
The task form and the status form write. The task form moves tasks through the
library's task state machine, sets `updated_at`, and saves the package with the
library's `Spec.Save`. The status form moves a package through its lifecycle
(activating, sealing, archiving, superseding it) with the library's
`Transition` and `Supersede`, sets `updated_at`, and moves an archived or
superseded package under `archive/`. `Save`, `Transition` and `Supersede` rewrite
**every** artifact of the package (`prd.md`, `requirements.json`,
`test_spec.json`, `tasks.json`, and `architecture.md` when there is one) in its
canonical encoding, through temporary files that they rename. What a call changes
in content is the task states or the status, and `updated_at`, but formatting the
encoding does not keep (a YAML comment in the frontmatter of `prd.md`, say) is
lost, and the files are left with mode `0600`, because the temporary files are
created private. Moving the directory under `archive/` is a rename: it rewrites
no file. With `--dry-run` neither form writes anything. `speca` makes no model
call and no network call, and reads no credential.

```sh
speca 16                                # the state of package 16
speca .specs/16_indexed_code_search     # the same, by path
speca 16 --validate                     # the format's full verdict on it
speca --list                            # every package under the spec root
speca --list --all                      # and those under archive/
speca 16 --task next --state in_progress   # claim the task to work on now
speca 16 --task 4 --state done             # finish task 4
speca 16 --task 1,2 --state in_progress --dry-run   # see what that would do
speca 16 --status active                # activate a draft: the hash of its intent is recorded
speca 16 --status sealed                # seal an active package whose tasks are all done
speca 03 --status superseded --by 12    # 12 replaces 03; 03 goes under archive/
speca --schema                          # the interface, as JSON Schema
```

Two paths are for a person and print text instead: `--version` prints the
build identity (`speca`, then the version, then the commit and build time when
the build set them, from the variables in `version.go`), and `-h`/`--help` or a
bare invocation prints the help to stderr. `--help` exits 0 and writes nothing
to stdout. A bare invocation exits 2; when stdout is not a terminal it also
writes a usage envelope, so a program still gets one object to parse. An
invocation that names flags but no input, and is not `--list`, is a bare
invocation.

## The input

The argument is a reference to one package, not a document. It is the same
reference `specr` takes, resolved by the same code (`internal/specpkg`). It is
tried in this order:

1. a path to the package directory, or to a file inside it
   (`.specs/16_indexed_code_search/tasks.json`), relative to the working
   directory and then to `--dir`;
2. a directory under the spec root (`--specs-dir`, else `$AF_SPEC_DIR`, else
   `<dir>/.specs`), matched by its whole name (`16_indexed_code_search`), its
   number (`16`; compared as a number, so `9` and `09` are the same) or its
   name (`indexed_code_search`).

A reference that matches no package is a usage error that lists the packages
the spec root holds, and names an archived package under `<root>/archive/`
that matches, by its path — archived packages are not looked up by name, but
are reported when given by path. A reference that matches two packages is
refused naming both. `-`, a URL and a multi-line argument are usage errors.
Every one of these messages begins with `speca`.

## The forms

| Flags | What `result` holds |
|---|---|
| none | the report on the package: its identity, whether it validates, its tasks, the next task, the lifecycle moves allowed, and its dependencies and dependents |
| `--validate` | the format's full verdict on the package: the same identity, then `valid`, `validation` and `traceability` |
| `--list` | one entry for every package under the spec root, in the order of their numbers |
| `--list --all` | the same, then the packages under `archive/` |
| `--task T --state S` | the report on the package after the change, with `changes`: the tasks moved, and where |
| `--task T --state S --dry-run` | the same, for a change that was not made: `dry_run` is true |
| `--status S` (`--by R` with `superseded`) | the report on the package after the change, read back from where it is now, with `changes`: the status, the record in the replacing package, and the move under `archive/`, in the order they were written |
| `--status S … --dry-run` | the same, for a change that was not made: `dry_run` is true |

A call names one operation. `--validate` with `--list`, the task form or the
status form with either of them or with each other, a positional input with
`--list`, and `--all` without `--list` are usage errors.

## The report: `speca <spec>`

`result` holds the fields below. A field a form does not produce is absent, not
empty, except where the table says the empty list is written.

| Field | Meaning |
|---|---|
| `spec_dir` | the package directory, relative to `--dir` when inside it, absolute otherwise; runnable as the input of `speca` and `specr` |
| `spec_id`, `spec_name`, `title`, `status`, `created_at`, `updated_at` | from the PRD frontmatter. `status` is `draft`, `active`, `sealed`, `superseded` or `archived` |
| `intent_hash`, `supersedes`, `tags` | from the frontmatter; each is absent when it is unset or empty |
| `valid` | the package's own verdict from `(*Spec).Validate`: the schemas and the cross-file rules C1 to C11. It is always written, so a `false` is not mistaken for an absent field |
| `failing_rules` | the rules the package's errors break (`C3`, `C7`, `json_schema`, …), each once, in the order they first appear; present only when `valid` is `false` |
| `intent_drift` | `true` when the package is `active`, has an `intent_hash`, and its PRD's `## Intent` section no longer hashes to it, or is gone. The library refuses every save of such a package, so a caller learns here why; absent otherwise |
| `tasks` | every task of the plan, in order: `id`, `title`, `kind`, `state`, `optional`, `depends_on`, and `ready`. `[]` when the plan has none |
| `task_counts` | `pending`, `in_progress`, `done`, `dropped` and `total`, the number of tasks. A task whose state is none of the four (the package then does not validate) is in the total and in no count |
| `next_task` | the task to work on now, as the entries of `tasks`: the first task in progress, otherwise the first ready task; absent when there is none |
| `allowed_status` | the lifecycle states the package may move to from its status, in the order `draft`, `active`, `sealed`, `superseded`, `archived`. `[]` for a terminal status |
| `depends_on` | the spec ids in the package's own `tasks.json` `dependencies`, in file order, each once. `[]` when there are none |
| `dependents` | the spec ids of the other packages under the spec root that depend on this one, sorted, each once. `[]` when none does; absent when the spec root could not be read |

A task is **ready** when it is `pending` and everything it depends on is
`done`. Its dependencies are its `depends_on` when that is present, where an
empty list means none; when it is absent, the task before it in the plan, and
the first task then has none (format v2 §8.3). A dependency the plan does not
have, or one that is `dropped`, is not done. The same rule is
`(*TasksV2Json).IsReady`, and `next_task` is `NextTask`, so `speca`, `specr` and
the `examples/tasks` program agree on what "ready" and "next" mean. `ready` is
written on a task only when it is true.

`dependents` is found with the spec root's dependency graph. A dependency on a
package that is not in the root does not stop it: the graph holds the edges
that were read.

A package in another format version (a PRD `schema_version` that is neither
absent nor 2) loads, but none of its tests or tasks is read: a version 1 plan
is `task_groups`, not `tasks`. It is reported with what did load — its `tasks`
are `[]` — and a high-severity `spec_invalid` warning saying so. A version 2
package that does not validate raises no warning: `valid` and `failing_rules`
are the answer.

## The verdict: `speca <spec> --validate`

`result` holds the identity fields of the report and `intent_drift`, then:

| Field | Meaning |
|---|---|
| `valid` | the combined verdict: `true` exactly when no error was kept from the package's own validation or from the cross-spec rules. It equals `validation.valid` |
| `validation` | `valid`, `error_count`, `warning_count`, and `errors` and `warnings`, each finding with its `check` (the rule), `artifact`, `entity` and `message`. The package's own findings come first, then the cross-spec findings that concern it. The library names no rule for a glossary conflict, and no artifact for the dependency cycle; `speca` reports the conflict as `cross_spec_glossary` in `requirements.json`, and the cycle in `tasks.json` |
| `traceability` | the derived coverage, in the shape `spec` reports: `criteria_covered`, `criteria_uncovered`, `paths_covered`, `paths_uncovered` and `tests_unowned` |

The fields that are specific to the report (`tasks`, `task_counts`,
`next_task`, `allowed_status`, `depends_on`, `dependents`, `failing_rules`) are
absent.

The cross-spec rules (format v2 §10.4: an unknown dependency, a dependency
cycle, a missing shared actor, a glossary term defined two ways) need every
package of the spec root, so this form loads each `NN_name` directory that
loads, adds the package itself when it is not among them — an archived package
is not — builds the dependency graph and runs `afspec.ValidateCrossSpec`. Of
what that finds it keeps:

- the errors tagged with the package's `spec_id`, which the library gives to
  the downstream package that decides them (an unknown dependency, a missing
  shared actor);
- the dependency cycle, which names no package, when the package has an edge
  in the graph — one from it or to it — since a package with no edge cannot be
  on a cycle;
- the glossary warnings over a term the package's own glossary defines.

A package under the root that fails to load is left out of the run. A spec root
that cannot be read skips the cross-spec rules, with a `specs_dir_unreadable`
warning, and the verdict is the package's own.

**A package that does not validate is a successful call:** exit 0, `valid`
`false`. Exit 1 is for a call that could not do its job.

## The list: `speca --list [--all]`

`result.packages` is one entry for every `NN_name` directory under the spec
root, in the order of their numbers — the prefix compared as a number, so
`100_x` follows `99_y` — with a tie broken by the whole name. `speca` lists the
directories itself and loads each with `afspec.LoadSpec`, rather than using
`afspec.DiscoverSpecs`, which drops a package whose `prd.md` cannot be read;
such a package must be listed, with the reason.

| Field | Meaning |
|---|---|
| `spec_dir` | as above |
| `spec_id`, `spec_name`, `title`, `status` | from the PRD frontmatter |
| `valid` | the package's own verdict from `Validate`. `false` is written |
| `task_counts` | as in the report |
| `next_task` | as in the report; absent when there is none |
| `archived` | `true` for a package listed from `archive/`, whatever its status: a superseded package under `archive/` is `archived: true` with `status: superseded`. Absent otherwise |
| `load_error` | why the package did not load, in the library's words. Such an entry holds `spec_dir` and `load_error` and nothing else, and the call goes on with the next package |

`--all` appends the packages under `<root>/archive/` in the same order. A spec
root with no `archive/` adds nothing and raises no warning; an `archive/` that
exists and cannot be read adds nothing and raises a `specs_dir_unreadable`
warning naming it.

A spec root that does not exist, is not a directory, or cannot be read fails
the call at stage `load` with `fix_hint.flag` `--specs-dir`: an empty list
would hide a mistyped flag. A root that exists and holds no package is an empty
`packages` list and exit 0.

## The task form: `speca <spec> --task T --state S`

The task form moves one or more tasks of a package to a state, through the
library's task state machine, and writes the package. It takes one input, as the
report does.

`--task` is given once or repeated, and each value may be comma-separated:
`--task 1,2`, `--task 1 --task 2`. Each item is a task id (a whole number, 1 or
more), `next` or `all`; `next` and `all` must each be the only item. Duplicate
ids are one. `--state` is `pending`, `in_progress`, `done` or `dropped`.

- A task id is moved as named, in the order of the plan, even when the task is
  already in that state; the state machine then refuses the move.
- `next` is the task `NextTask` names, the report's `next_task`: the first task
  in progress, else the first ready one.
- `all` is every task except those already in `--state` and those `dropped`.
  When that is none, the call succeeds, changes nothing and writes nothing.

**The state machine decides every move** (format v2 §8.3.1):

| From | May move to |
|---|---|
| `pending` | `in_progress`, `dropped` |
| `in_progress` | `done`, `pending` |
| `done` | `pending` |
| `dropped` | none |

Every named move is checked before any is made, and **either every move is made
or none is**. A move the machine does not allow refuses the whole call. A claim
of a task that is already `in_progress` is such a move, which makes the machine
the compare-and-set two agents need: of two calls that follow one another, the
second is refused, and learns that the task is taken. There is no lock: two calls
that read the package at the same moment both see the task pending, both claim
it, and the last to write is the one whose files stand.

The checks run in this order, and each stops the call:

1. the flags: the usage errors of the table below, before anything is read;
2. the package loads (stage `load`);
3. the package is in format version 2: a version 1 package is a usage error, with
   or without `--force`, because a save would replace its plan with an empty one;
4. the package can take a write (stage `status`): it is a draft, or active with
   its intent matching its `intent_hash`. A sealed, superseded or archived package,
   and an active one whose `## Intent` section drifted or is gone, are refused,
   as the library's `Save` would refuse them. `--force` and `--dry-run` change
   nothing here;
5. the ids: an id the plan does not have, `next` with no next task (a plan with
   no tasks says so, rather than that every task is done), and `all` on a plan
   with no tasks are usage errors;
6. the moves (stage `task`);
7. the keys (stage `validate`), when a write would happen: `Save` re-encodes every
   artifact and drops the keys the schema does not allow, so a package that holds
   any (a leaked version 1 `traceability`, or a key of your own in the frontmatter
   of `prd.md`) would lose them. That is refused, listing each artifact and key
   (`tasks.json /traceability`, `prd.md /my_key`), unless `--force` is given. The
   library reports such keys in the three JSON artifacts; the frontmatter of
   `prd.md` is looked at by `speca`. A package that does not validate for another
   reason still takes the change.

**`--force`** applies when `--state` is `done` or `pending`: a move the machine
refuses is then made with the library's own bypasses, `CompleteTaskStates` or
`ResetTaskStates`, and a move it allows still goes through `TransitionTask`. A
task already in the state is no move. Each task set through a bypass has a high
`task_state_forced` warning and `forced: true` on its change. The library has no
setter for `in_progress` or `dropped`, and `speca` writes no state by hand, so
`--force` covers nothing there. `--force` also lets the write go over the keys of
step 7. It skips no usage error, no status refusal and no legacy refusal.

**`--dry-run`** runs every check, expansion, warning and refusal and does not
write. The result is the one the call would have produced, with `dry_run: true`,
the changes and the `updated_at` it would have set. A refusal is the same
refusal, with the same exit code. Its `next` is one entry, the same call without
`--dry-run`, naming the ids it resolved (`--task 1,2`, not `all`) and keeping
`--force`; with nothing to change it has none.

**The write.** When at least one task changes, `updated_at` is set to the time of
the call, UTC, RFC 3339 (`Save` does not), and the package is written with
`Spec.Save`, which writes every artifact through temporary files and then
renames them. A call that changes nothing writes nothing and leaves `updated_at`
as it was. If the write fails, a failure while writing the temporary files leaves
the package unchanged, and one during the renames may leave some files updated;
the message says so and tells the caller to run `speca <spec>`.

**The result** of a call that succeeds is the report of the package as it was
written (its `valid` and `failing_rules` are the verdict on the files `Save`
wrote, which hold none of the keys it deleted), with every field the report has,
and `changes`: one
`{"kind": "task", "id", "from", "to"}` for each task whose state changed, in plan
order, with `"forced": true` where a bypass was used.

**A refusal** (stage `status`, `task` or `validate`), and a failed write, return
the report of the package as it is on disk, with no `changes`, so the caller sees
the states without another call. Its `summary` is the refusal's message and it has
no `next`: a suggestion after a refused claim could send one agent to the task
another holds. A usage error or a package that does not load has no result.

## The status form: `speca <spec> --status S`

The status form moves a package through its lifecycle (format v2 §9) and writes
it. It takes one input, as the report does. `--status` is `active`, `sealed`,
`superseded` or `archived`. `superseded` needs `--by <spec>`, the package that
replaces this one, named as the input is. `--status draft` is a usage error: no
move leads back to draft, and a package whose intent must change is superseded by
a new one. `--by` with any other status, `--status` beside another operation, and
an empty `--status` or `--by` are usage errors too.

**The lifecycle** is the library's (`afspec.ValidTransition`):

| From | May move to |
|---|---|
| `draft` | `active`, `archived` |
| `active` | `sealed` |
| `sealed` | `superseded`, `archived` |
| `superseded` | `archived` |
| `archived` | none |

A package is *under `archive/`* when the directory that holds it is named
`archive`; archived and superseded packages belong there, the others do not. The
move goes under the directory that holds the package, not under `--specs-dir`
(`<holding directory>/archive/<name>`): the two are the same for a package in the
spec root, and a package reached by a path outside it stays in its own tree. A
package that is in a directory named `archive` and is not archived or superseded
can still be activated or sealed where it is; `speca` does not move it.

**The checks** run in this order, under `--dry-run` too. The first to refuse ends
the call: nothing is written, and the envelope has no `result`.

1. `--by` is resolved as the input is, and a `--by` that names the package itself
   is refused (usage; the message begins `--by:`).
2. **Load** (stage `load`, category `input`). The package, and the `--by`
   package, are loaded. One the library refuses, and one in a format version
   other than 2 (`speca` changes the state of version 2 packages only: a write
   would discard the tasks and tests a version 1 package holds), are refused,
   naming the directory and `--by` when the replacing package is at fault.
   `--force` does not reach either. A `--by` package with the input's own
   `spec_id` is a usage error, which only the loaded packages can tell.
3. **The move** (stage `status`, category `lifecycle`). A package that is already
   at the status asked for, which is `archived` or `superseded`, and is not under
   `archive/`, is a *move-only* call: the resume of one that was interrupted.
   Any other move the lifecycle does not allow is refused. The message names the
   package, its status and the target: "already `<status>`" when they are equal,
   otherwise the shortest chain of allowed moves (`.specs/04_x is active and
   cannot move to archived directly; the shortest way is active → sealed →
   archived: seal it first (`speca .specs/04_x --status sealed`), then archive
   it`), or that no sequence of moves leads there. `fix_hint` is `{flag:
   "--status", valid: [the moves allowed from the status]}`, none when there are
   none. `--force` does not override it.
4. **The target's own checks**, below.
5. **Keys the schema does not allow** (stage `status`, category `lifecycle`), for
   the moves that write the package's files and are not an activation (which asks
   the same as part of its validation): sealing, archiving and superseding. A
   write drops such a key, so a package that holds any is refused, naming each
   (`tasks.json /traceability`, `prd.md /my_key`), with `fix_hint.flag`
   `--force`. The library reports those of the three JSON artifacts; the
   frontmatter of `prd.md` is looked at by `speca`. A move-only call writes no
   file, and is not asked.
6. **The archive destination** (stage `archive`, category `disk`), for a move that
   ends under `archive/` from a package that is not there. A destination that is
   taken is refused, naming it. `--force` does not override it. The call has no
   `result` and is not resumable: nothing was written.

**Each target**

- `active`. A draft with no `## Intent` section cannot be activated, and `--force`
  does not change that: the message says to add the heading `## Intent`, spelled
  exactly so and on a line of its own. A package that does not validate is
  refused at stage `validate`, category `invalid_spec`, naming the failing rules,
  with `fix_hint.flag` `--force`; a key the schema does not allow counts as a
  failing rule `unknown_field`. The write is `Transition("active")`, which records
  the hash of the intent. The package stays where it is.
- `sealed`. A package whose intent has drifted cannot be sealed, and `--force`
  does not change that: a drifted package cannot be saved, and sealing would make
  the drift permanent. The message says to restore the intent that was activated,
  or to write a new package that supersedes this one; there is no `fix_hint`. A
  task still `pending` or `in_progress`, an optional one included, refuses it too,
  naming each with its state, with `fix_hint.flag` `--force`. The write is
  `Transition("sealed")`.
- `archived`, from `draft`, `sealed` or `superseded`. A package not under
  `archive/` gets `Transition("archived")` and then the move; one already under
  it gets the status only and stays. A move-only call (the package is `archived`
  and not under `archive/`) only moves it: no file is rewritten and `updated_at`
  stays as it was.
- `superseded`, from `sealed`, with `--by`. The replacing package must be a
  `draft`, an `active` or a `sealed` one; a superseded or archived package cannot
  replace another (stage `status`, `fix_hint.flag` `--by`; `--force` does not
  override it). The writes come in this order, so that the step most likely to
  fail is the last: `Supersede` writes the banner and the status; the old
  `spec_id` is added at the end of the replacing package's `supersedes`, unless it
  is there; the package is moved under `archive/`, unless it is there. Its status
  stays `superseded`. The id is recorded with `Save`, and only when the replacing
  package is a draft, or active without drift, and holds no key the schema does
  not allow. Otherwise, or when `Save` fails, the call still succeeds: a low
  `supersedes_not_recorded` warning names the replacing package's directory, the id
  not recorded and the reason, and that package is not touched. `--force` does not
  extend to it. A move-only call (the package is `superseded` and not under
  `archive/`, with `--by`) skips `Supersede`, records the id if it is missing, and
  moves the package.

**`--force`** skips three checks and nothing else: that a package being activated
validates (a high `spec_invalid` warning names the rules), that no task is open
when sealing (a high `tasks_open` warning lists them), and that no key the schema
does not allow would be dropped (a high `spec_invalid` warning names the keys). It
does not reach a usage error, the version check, the lifecycle, a missing intent,
a drifted intent, the status of the replacing package, a taken destination, or
the replacing package's own save.

**The writes.** `updated_at` is set to the time of the call, UTC, RFC 3339 (the
library does not), on every package the call writes, the replacing package too.
The warnings are raised once the first write has been made: a call that is
refused, or whose first write fails, raises none.

**`--dry-run`** runs every check and plans every warning, writes and moves
nothing, and reports the package as it would be left, held in memory: its status,
`updated_at`, the intent hash an activation would record, and the directory it
would move to. `dry_run` is `true`, and the changes, warnings, refusals and exit
code are the real call's. It cannot know that the disk would refuse the save of
the replacing package, which is a warning in the real call and a change in the
dry run. Its `next` is the same call without `--dry-run`, keeping `--by` and
`--force`.

**The result** of a call that succeeds is the report of the package, read back
with `LoadSpec` from where it is now (so `spec_dir` is the directory after any
move), with `changes` in the order they were written:

| `kind` | Fields | Meaning |
|---|---|---|
| `status` | `from`, `to` | the lifecycle status |
| `supersedes` | `spec`, `added` | the id `added` (the superseded package's) was added to the `supersedes` of the package `spec` |
| `moved` | `from`, `to` | the package's directory, from where it was to where it is, as `spec_dir` shows paths |

**A failure while writing.**

- A write the disk refuses (`Transition`, `Supersede`) is stage `write`, category
  `disk`, with the library's message and no `result`. A lifecycle or intent error
  that reaches the write despite the checks is stage `status`, category
  `lifecycle`.
- A package that was written but cannot be read back is stage `load`, category
  `input`. The `result` carries the changes and the package's identity as the
  library returned it from the write; it has no tasks or verdict.
- **The move under `archive/` fails after the status was written** (stage
  `archive`, category `disk`, **`resumable: true`**, `retryable: false`). The
  package is where it was, in its new status. The message names its directory and
  new status and says that running the same call again moves it; the `summary`
  says the status was written and the move was not. The `result` is the package
  as it is now, with what was written; `next` is the same call against that
  directory (with `--by` for a package being superseded). Running it again is a
  move-only call: it moves the package and changes nothing else. A move-only call
  whose move fails is reported the same way.

## The output

Report on package `09`, whose first task is done:

```jsonc
{
  "tool": "speca",
  "version": "v1.4.1",
  "ok": true,
  "status": "done",
  "exit_code": 0,
  "summary": "speca: 09_agent_mode draft, 1 of 3 tasks done; next task 2",
  "result": {
    "spec_dir": ".specs/09_agent_mode",
    "spec_id": "09",
    "spec_name": "agent_mode",
    "title": "Agent mode for the spec CLI",
    "status": "draft",
    "created_at": "2026-09-08T09:00:00Z",
    "updated_at": "2026-09-08T09:00:00Z",
    "valid": true,
    "tasks": [
      { "id": 1, "title": "Detect agent mode and keep stdout JSON-only", "kind": "implement", "state": "done" },
      { "id": 2, "title": "Error envelope and closed-pipe handling", "kind": "implement", "state": "pending",
        "depends_on": [1], "ready": true },
      { "id": 3, "title": "Verify agent mode end to end", "kind": "integration", "state": "pending",
        "depends_on": [1, 2] }
    ],
    "task_counts": { "pending": 2, "in_progress": 0, "done": 1, "dropped": 0, "total": 3 },
    "next_task": { "id": 2, "title": "Error envelope and closed-pipe handling", "kind": "implement",
                   "state": "pending", "depends_on": [1], "ready": true },
    "allowed_status": ["active", "archived"],
    "depends_on": ["08"],
    "dependents": []
  },
  "next": [ { "tool": "speca", "input": ".specs/09_agent_mode", "flags": ["--status", "active"],
              "why": "the package validates and has an intent; activating it records the hash of its ## Intent" },
            { "tool": "speca", "input": ".specs/09_agent_mode", "flags": ["--task", "2", "--state", "in_progress"],
              "why": "task 2 (Error envelope and closed-pipe handling) is the next ready task and nothing is in progress; claim it before working on it" },
            { "tool": "specr", "input": ".specs/09_agent_mode", "flags": ["--task", "2"],
              "why": "task 2 (Error envelope and closed-pipe handling) is the next ready task; render the package scoped to it" } ],
  "duration_ms": 3,
  "started_at": "2026-10-22T08:28:47Z"
}
```

The verdict on the same package, in a spec root that has no package `08`:

```jsonc
{
  "tool": "speca",
  "ok": true,
  "exit_code": 0,
  "summary": "speca: 09_agent_mode does not validate (1 error: cross_spec_unknown_dependency)",
  "result": {
    "spec_dir": ".specs/09_agent_mode",
    "spec_id": "09",
    "valid": false,
    "validation": {
      "valid": false, "error_count": 1, "warning_count": 0,
      "errors": [ { "check": "cross_spec_unknown_dependency", "artifact": "tasks.json", "entity": "09",
                    "message": "spec 09 declares a dependency on spec \"08\", which does not exist in the spec root" } ]
    },
    "traceability": { "criteria_covered": 5, "paths_covered": 1 }
  }
}
```

And the list of two packages (the entries abbreviated):

```jsonc
{
  "summary": "speca: 2 packages (2 draft)",
  "result": {
    "packages": [
      { "spec_dir": ".specs/01_test_feature", "spec_id": "01", "status": "draft", "valid": true,
        "task_counts": { "pending": 3, "in_progress": 0, "done": 0, "dropped": 0, "total": 3 },
        "next_task": { "id": 1, "title": "Load and save spec artifacts", "kind": "implement", "state": "pending", "ready": true } },
      { "spec_dir": ".specs/09_agent_mode", "spec_id": "09", "status": "draft", "valid": true, … }
    ]
  }
}
```

The task form, claiming the next task (the fields of the report are left out):

```jsonc
{
  "tool": "speca",
  "ok": true,
  "exit_code": 0,
  "summary": "speca: 09_agent_mode task 1 pending → in_progress",
  "result": {
    "spec_dir": ".specs/09_agent_mode",
    "updated_at": "2026-10-22T09:23:23Z",
    "next_task": { "id": 1, "title": "Detect agent mode and keep stdout JSON-only", "kind": "implement", "state": "in_progress" },
    "changes": [ { "kind": "task", "id": 1, "from": "pending", "to": "in_progress" } ]
  },
  "next": [ { "tool": "specr", "input": ".specs/09_agent_mode", "flags": ["--task", "1"],
              "why": "task 1 (Detect agent mode and keep stdout JSON-only) is in progress; render the package scoped to it" } ]
}
```

The same claim made again is refused, with the states the task may move to; the
result is the package as it is on disk, and there is no `next`:

```jsonc
{
  "tool": "speca",
  "ok": false,
  "exit_code": 1,
  "summary": ".specs/09_agent_mode: the task state machine (format v2 §8.3.1) does not allow this move, so no task was changed: task 1 is in_progress and cannot move to in_progress (it is already there; it may move",
  "error": {
    "stage": "task", "category": "lifecycle", "retryable": false, "resumable": false,
    "message": ".specs/09_agent_mode: the task state machine (format v2 §8.3.1) does not allow this move, so no task was changed: task 1 is in_progress and cannot move to in_progress (it is already there; it may move to pending or done)",
    "fix_hint": { "flag": "--state", "valid": ["pending", "done"] }
  },
  "result": { "spec_dir": ".specs/09_agent_mode", "next_task": { "id": 1, "state": "in_progress", … }, … }
}
```

And a dry run of finishing it, whose `next` is the real call:

```jsonc
{
  "summary": "speca: 09_agent_mode task 1 in_progress → done (dry run, nothing written)",
  "result": {
    "changes": [ { "kind": "task", "id": 1, "from": "in_progress", "to": "done" } ],
    "dry_run": true,
    …
  },
  "next": [ { "tool": "speca", "input": ".specs/09_agent_mode", "flags": ["--task", "1", "--state", "done"],
              "why": "the same call without --dry-run, which makes these changes" } ]
}
```

The status form, superseding package `03` with package `12` (the fields of the
report are left out):

```jsonc
{
  "tool": "speca",
  "ok": true,
  "exit_code": 0,
  "summary": "speca: superseded 03_old_search by 12, moved to .specs/archive/03_old_search",
  "result": {
    "spec_dir": ".specs/archive/03_old_search",
    "status": "superseded",
    "changes": [
      { "kind": "status", "from": "sealed", "to": "superseded" },
      { "kind": "supersedes", "spec": "12", "added": "03" },
      { "kind": "moved", "from": ".specs/03_old_search", "to": ".specs/archive/03_old_search" }
    ]
  }
}
```

A dry run of activating package `09`, whose `next` is the real call:

```jsonc
{
  "summary": "speca: activated 09_agent_mode (dry run; nothing written)",
  "result": {
    "spec_dir": ".specs/09_agent_mode",
    "status": "active",
    "intent_hash": "5d56eba561fe88c8c8dbf48cf78b803dd91e3f61b45c1d7ff556a55ae82323d3",
    "changes": [ { "kind": "status", "from": "draft", "to": "active" } ],
    "dry_run": true,
    …
  },
  "next": [ { "tool": "speca", "input": ".specs/09_agent_mode", "flags": ["--status", "active"],
              "why": "the same call without --dry-run, which makes these changes" } ]
}
```

A move the lifecycle does not allow is refused, with the way there and the moves
that are allowed, and no `result`:

```jsonc
{
  "tool": "speca",
  "ok": false,
  "exit_code": 1,
  "summary": ".specs/04_legacy_cache is active and cannot move to archived directly; the shortest way is active → sealed → archived: seal it first (`speca .specs/04_legacy_cache --status sealed`), then archive it",
  "error": {
    "stage": "status", "category": "lifecycle", "retryable": false, "resumable": false,
    "message": ".specs/04_legacy_cache is active and cannot move to archived directly; the shortest way is active → sealed → archived: seal it first (`speca .specs/04_legacy_cache --status sealed`), then archive it",
    "fix_hint": { "flag": "--status", "valid": ["sealed"] }
  }
}
```

And an archive whose move was refused after the status was written, which running
the suggested call finishes (the message abbreviated):

```jsonc
{
  "tool": "speca",
  "ok": false,
  "exit_code": 1,
  "summary": "speca: 05_cache is archived: the status was written and the move was not; run the same call again to move it under archive/",
  "error": {
    "stage": "archive", "category": "disk", "retryable": false, "resumable": true,
    "message": ".specs/05_cache is now archived, but moving it under archive/ failed (save error: cannot move spec to archive: … permission denied); it is still where it was, and running the same call again moves it"
  },
  "result": {
    "spec_dir": ".specs/05_cache",
    "status": "archived",
    "changes": [ { "kind": "status", "from": "sealed", "to": "archived" } ],
    …
  },
  "next": [ { "tool": "speca", "input": ".specs/05_cache", "flags": ["--status", "archived"],
              "why": "the status was written and the move was not; the same call moves the package and changes nothing else" } ]
}
```

`ok`, `status`, `exit_code`, `summary`, `error` and `warnings` mean what they
mean for `spec` (see [cli.md](cli.md#the-output)). The envelope fields `speca`
never writes (`input`, `model`, `usage`, `side_effects`) are absent.

### `summary`

One sentence per form, which the shell caps at 200 characters:

| Form | Shape |
|---|---|
| report | `speca: <directory> <status>, <done> of <total> tasks done`, then `; next task N` when there is one, then `; does not validate (<rules>)` when `valid` is `false` |
| `--validate` | `speca: <directory> validates`, or `speca: <directory> does not validate (<n> errors: <rules>)`; `1 error` for one |
| `--list` | `speca: <n> packages` (`1 package` for one), then, when there is anything to count, ` (` the non-zero counts per status in lifecycle order, then `<n> unreadable`, `)` |
| status form | `speca: activated <name>`, `speca: sealed <name>`, `speca: archived <name>, moved to <path>`, `speca: superseded <name> by <spec_id>, moved to <path>`, and `speca: moved <name> to <path>` for a call that only moves the package; `<name>` is the package's directory name, `<path>` where it went, as `spec_dir` shows it. An archive or a supersede that needs no move has no `, moved to` clause. Under `--dry-run` the sentence ends ` (dry run; nothing written)` (the task form's has a comma). After a move under `archive/` that failed, it says the status was written and the move was not. A refusal has no result, so its `summary` is the refusal's message |
| task form | `speca: <directory> task N <from> → <to>`, `speca: <directory> tasks N, M → <to>`, or `speca: <directory> no task changed`; ` (dry run, nothing written)` is added under `--dry-run`. A refusal has no sentence of its own: its `summary` is the refusal's message. The shell adds `; <n> high-severity warning(s)` to the sentence of any success that raised a high warning, so a call that used `--force` to set a state, or to write over keys, ends in `; 1 high-severity warning` |

### Reading it from Go

`envelope.DecodeSpeca` reads the object, whichever form it was, into an
`envelope.AdminEnvelope`, whose `Result` is an `*envelope.AdminResult`; the
fields that were absent are zero. `Valid` is a `*bool`, so a package that does
not validate (`false`) is told from one the form did not ask about (`nil`). The
package is described in [api.md](api.md#envelope).

```go
env, err := envelope.DecodeSpeca(stdout)
if err != nil {
    return err
}
if !env.OK {
    return env.Error
}
if env.Result.NextTask != nil {
    fmt.Println("work on task", env.Result.NextTask.ID)
}
```

### `next`

`next` suggests only calls that exist:

- after a report with a `next_task` N: `specr <spec_dir> --task N`, whose `why`
  says whether the task is in progress or is the next ready task. When the task
  is a ready pending one and the package can take a write (a draft, or an active
  package whose intent has not drifted), the claim comes first:
  `speca <spec_dir> --task N --state in_progress`;
- after `--list`: the claim and the render for the first active package, in list
  order and not under `archive/`, that has no task in progress, has a ready task
  N and whose intent has not drifted;
- after a task form that made changes: the report's calls about the tasks on the
  package as it now is, so a claim is followed by the render of the task, and a
  task finished by the claim of the next ready one, or by nothing when no task is
  left. The status call that `speca <spec>` suggests (below) is the report form's
  alone: after the last task is done, `speca <spec>` is what suggests sealing;
- after a dry run: the one call that makes the changes (see above), or nothing
  when there are none;
- after a report on a `draft` that validates, has a `## Intent` and holds no key
  the schema does not allow: `speca <spec_dir> --status active`, **before** any
  suggestion about its tasks;
- after a report on an `active` package that has no task `pending` or
  `in_progress`, whose intent has not drifted and that holds no key the schema
  does not allow: `speca <spec_dir> --status sealed`. These conditions are the
  checks of the status form, so a suggested call is never refused. `--list`
  suggests no status call;
- after a status call: the claim of `next_task`, after an activation, when that
  task is ready; nothing after sealing, archiving, superseding or a call that only
  moves the package; after a dry run, the same call without `--dry-run` (with
  `--by` and `--force` kept); after a move under `archive/` that failed after the
  status was written, the same call against the package where it is, with `--by`
  for a package being superseded;
- after `--validate`, after a refusal or a failure other than the one just
  described, and after a report or a list with nothing to name: nothing.

### `error`

| `stage` | `category` | When |
|---|---|---|
| `usage` | `usage` | more than one input; an input with `--list`; `--validate` with `--list`; `--all` without `--list`; `-`, a URL or a multi-line input; a `--dir` that is not a directory; a reference that names no package or two; an undefined flag. For the task form: `--task` without `--state` or the reverse; a `--state` outside the four (`fix_hint` is `{flag: "--state", valid: [the four]}`); a `--task` value that is not an id, `next` or `all`, or `next`/`all` beside anything else; the task form with `--validate` or `--list`; `--force` or `--dry-run` without `--task` or `--status`; and, once the package is loaded, an id it does not have (`fix_hint` is `{flag: "--task", valid: [every id]}`, none when the plan has no tasks), `next` with no next task (`fix_hint` lists the pending ids when they are only blocked), `all` on a plan with no tasks, and a package in a format version other than 2. For the status form: `--status draft` (`fix_hint` is `{flag: "--status", valid: ["active", "sealed", "superseded", "archived"]}`, as for any value that is not one of the four, an empty one included); `--status superseded` without `--by`; `--by` without `--status superseded`, or with another status, or empty; the status form with `--task`/`--state`, `--validate` or `--list`; a `--by` that the resolver refuses (the message begins `--by:`), that names the package itself, or whose package has the input's `spec_id`. Nothing was written, and the message is also written to stderr |
| `load` | `input` | `afspec.LoadSpec` refused the package (a missing artifact, malformed JSON or YAML), naming the directory and the library's reason; or `--list` on a spec root that cannot be read, with `fix_hint.flag` `--specs-dir`. The status form: also the `--by` package (the message names `--by`), a package in a format version other than 2, and a package that was written but cannot be read back (its `result` carries the changes) |
| `status` | `lifecycle` | the task form on a package that cannot take a write: sealed, superseded or archived, or active with a drifted `## Intent`; or `Save` refused it as such. No `fix_hint`; `--force` and `--dry-run` change nothing. The status form: a move the lifecycle does not allow (`fix_hint` is `{flag: "--status", valid: [the moves allowed]}`, none when there are none); an activation with no `## Intent`; sealing a package whose intent drifted (no `fix_hint`) or that has a task open (`fix_hint.flag` `--force`); a replacing package that is superseded or archived (`fix_hint.flag` `--by`); keys the schema does not allow (`fix_hint.flag` `--force`); or a lifecycle or intent error that reached the write. `--force` reaches only the ones with a `fix_hint.flag` of `--force` |
| `task` | `lifecycle` | the task state machine does not allow a named move. The message names every refused task with its state and the target, cites §8.3.1 and says where each may go; `fix_hint` is `{flag: "--state", valid: [the first refused task's targets]}`, none when that task is `dropped`, and the message then says `--force` with `done` or `pending` is the only way out. Nothing was changed |
| `validate` | `invalid_spec` | the task form: the write would delete keys the schema does not allow, and `--force` was not given. The message lists each artifact and key; no `fix_hint`. The status form: an activation of a package that does not validate (the message names the failing rules; `fix_hint.flag` `--force`) |
| `write` | `disk` | `Save` could not write the package. Not retryable; see "The write" above. The status form: `Transition` or `Supersede` could not, with the library's message and no `result` |
| `archive` | `disk` | the status form: the archive destination is taken, before anything was written (no `result`, not resumable); or the move under `archive/` failed after the status was written (**resumable**, with a `result` and a `next`) |
| `schema` | `internal` | the `--schema` document could not be encoded |

`result` is absent from the usage, `load` and `schema` errors. The task form's
`status`, `task`, `validate` and `write` failures carry the report of the package
as it is on disk, without `changes` and without `next`. The status form's
refusals and its `write` failure carry no `result`; the two failures that follow a
write do: the read-back (stage `load`) with the changes, and the move under
`archive/` (stage `archive`) with the changes, the package where it is, and the
call that finishes it. `retryable` is always false. `resumable` is true for the
move under `archive/` that failed after the status was written, and for nothing
else.

### Exit codes

| Code | `status` | Meaning |
|---|---|---|
| `0` | `done` | reported — including a package that does not validate; or the tasks were moved, or none needed to be; or the status was changed, or the package only moved |
| `1` | `failed` | the package could not be loaded, or the spec root `--list` reads could not be read; or the task form was refused after the package was read (stages `status`, `task`, `validate`), or its write failed; or the status form was refused (stages `status`, `validate`, `archive`) or failed (stages `write`, `archive`, `load`) |
| `2` | `usage` | usage error — nothing was written |

### Warning codes

| Code | Severity | Stage | When |
|---|---|---|---|
| `specs_dir_unreadable` | low | preflight | the spec root (or, for `--list --all`, `archive/`) could not be read. The report goes without `dependents`, the verdict without the cross-spec rules, the list without the archived packages |
| `spec_invalid` | high | validate | the package is in a format version other than 2: its tests and tasks are not read. For the task form: `--force` writes over keys the schema does not allow, which the warning names |
| `spec_invalid` | low | validate | the task form went ahead on a package that does not validate, which the warning names by rule. A call raises at most one `spec_invalid` |
| `spec_invalid` | high | validate | the status form: `--force` activated a package that does not validate (naming the rules, and the keys the write drops), or wrote a package that holds keys the schema does not allow (naming them) |
| `tasks_open` | high | status | `--force` sealed a package with a task `pending` or `in_progress`. The warning lists each task with its state |
| `supersedes_not_recorded` | low | status | superseding went through, but the old `spec_id` was not added to the replacing package's `supersedes`: it is sealed, its intent drifted, it holds keys the schema does not allow, or its save failed. The warning names the replacing package's directory, the id and the reason |
| `task_not_ready` | low | task | a task moved to `in_progress` has a dependency that is not `done`, judged on the plan before the call. The warning names the task and the dependencies |
| `task_state_forced` | high | task | `--force` set a state the machine would have refused. One for each such task, naming it and its two states |

The warnings of the task and status forms are raised when the call goes ahead: a
refusal, or a write that fails, raises none, since they would speak of changes
that were not made. The shell adds `; <n> high-severity warning(s)` to the
`summary` of a success that raised a high warning.

## Flags

| Flag | Default | Effect |
|---|---|---|
| `--validate` | off | give the format's full verdict on the package |
| `--list` | off | list every package under the spec root; takes no input |
| `--all` | off | with `--list`, also list the packages under `archive/`, marked archived |
| `--task` | | the task form: the tasks to move, once or repeated, each value a comma-separated list of ids, `next` or `all` |
| `--state` | | the task form: the state to move them to: `pending`, `in_progress`, `done` or `dropped` |
| `--status` | | the status form: the lifecycle status to move the package to: `active`, `sealed`, `superseded` or `archived`. An archived or superseded package is moved under `archive/` |
| `--by` | | with `--status superseded`: the package that replaces this one, named as the input is |
| `--force` | off | with the task form: set `done` or `pending` where the machine refuses it, and write over keys the schema does not allow. With the status form: skip the checks that an activation validates, that no task is open when sealing, and that no key the schema does not allow would be dropped |
| `--dry-run` | off | with the task form or the status form: run every check and report the result, write nothing |
| `--specs-dir` | `<dir>/.specs`, or `$AF_SPEC_DIR` | where `NN_name` packages live |
| `--dir` | `.` | the repository the spec root and a relative reference are resolved against |
| `--schema` | | print the self-description below and exit 0 |
| `--version` | | print the build identity and exit 0 |

Flags may come before or after the input. `$AF_SPEC_DIR` is the one environment
variable `speca` reads; it reads no credential and no model variable.

## Self-description (`--schema`)

`speca --schema` prints one JSON object describing the interface and exits 0.
It reads nothing — no package, no `--dir` — so it runs anywhere:

- `tool`, `description` — the command and what it does, with the sentence
  naming `spec`, `specr` and `speca`;
- `input` — what the one positional input may be, with examples;
- `flags` — a standalone JSON Schema (2020-12) document of the flags, each
  with its `type`, `default` and `description`, generated from the flag set;
  `--state` is an `enum` of the four states and `--status` of the four targets.
  `--schema` and `--version` are not listed;
- `result` — a standalone JSON Schema document of the envelope, with `result`
  replaced by `speca`'s own result type and every field described, generated
  from the Go types; the envelope fields `speca` never writes (`input`,
  `model`, `usage`, `side_effects`) are left out;
- `exit_codes` — the three codes and what each means.

The document is checked in at `cmd/speca/testdata/schema.golden.json`, and a
test fails when a flag or a result field changes without it;
`UPDATE_GOLDEN=1 go test -run '^TestSchemaGolden_TS10_20$' ./cmd/speca`
regenerates it deliberately.

## Calling it from an agent

The help, `--schema` and `next` are what make `speca` discoverable: an agent
that knows only the command's name can run `speca --help` or `speca --schema`,
and one that asked for a package's state is told which `specr` call to make
next.

In a skill's instructions, for example agent-fox's `af-impl` at its first
step:

```markdown
**Where am I.** Run `speca <package>` and read `result`: `valid` and
`failing_rules` say whether the package validates, `intent_drift` whether its
intent was edited after activation, and `next_task` is the task to work on —
the one in progress, else the first ready one. `result.tasks` lists the plan
with each task's state and readiness. `next` names the `specr` call that
renders that task. `speca --list` shows every package at once.
```

The same skill's "one task at a time" step claims the task before it renders it
and records it done after:

```markdown
**One task at a time.** Claim the task before you start it:
`speca <package> --task <N> --state in_progress`. A refusal at stage `task`
means another agent holds it; pick another from `result.tasks`. Then work
from the spec scoped to it: run `specr <package> --task <N>` and read
`result.markdown` — the requirements that own the task's criteria, its tests
and the paths they verify in full, everything else by id. When its tests
pass, record it: `speca <package> --task <N> --state done`, and follow `next`
to the task after it. If you stop before it is done, release it with
`--state pending`. Never edit `tasks.json` by hand: if `speca` or `specr` is
not installed, read the four artefacts and scope them by hand, and say in your
report which task you finished instead of changing its state.
```

`specr` suggests those two calls itself: its `next` holds the claim after a whole
render of a package that can take a write, and after `specr <package> --task N` the
claim of a ready `N` or, once `N` is in progress, the call that records it done (see
[specr.md](specr.md#artifacts-and-next)). `speca` and `specr` agree on whether a
package can take a write (`specpkg.Writable`: a draft, or an active package whose
intent has not drifted), so neither suggests a claim for one the other would refuse
for that reason. Both still offer it for a package that holds a key the schema does not
allow, or is in another format version, and `speca` refuses the call with the reason.

Reading the state of a package is the first half of working through it; the
second, recording a task as claimed, finished or released, is the task form. The
loop an agent runs is: `speca <package>` and take the claim from `next`;
`speca <package> --task next --state in_progress`; render the task and do the
work; `speca <package> --task N --state done`, whose `next` is the claim of the
task after it. `--dry-run` shows the effect of a call before it is made. Two
agents that claim the same task get the machine's answer: the second call is
refused, with the states the task may move to and the package as it is on disk.
The same loop in Go is [`examples/tasks`](../examples/tasks/main.go).

The lifecycle has the same shape. A draft that validates is told by `next` to
`speca <package> --status active`, whose `next` is the claim of its first task;
an active package whose tasks are all finished is told to
`speca <package> --status sealed`. A call refused for a reason that `--force`
overrides says so in its `fix_hint`; one that is refused for a rule of the format
does not. When a move under `archive/` fails after the status was written, the
envelope is `resumable` and its `next` is the call to make again, which moves the
package and changes nothing else.

## What it leaves out

`speca` is a fast call, so the parts of `spec`'s shell that serve a long model
run are absent: there is no model, no `--preflight`, no `--detail`, no `--output`
and no progress on stderr. (`--dry-run` here is that of the task and status forms, not `spec`'s.)
It changes nothing but the state of the tasks of a package, its lifecycle status
(with the `intent_hash` an activation records and the banner a superseded package
is given) and where it lives, the `supersedes` of a replacing package, and the
`updated_at` of what it writes: not a package's content (requirements, tests, a task's title,
steps or dependencies), and it creates and renders no package, which are `spec`'s
and `specr`'s. It does not move a package back to `draft`, reopen a sealed one,
or take one out of `archive/`; it does not re-hash a changed `## Intent`, edit
`owner`, `tags` or `source`, or remove an entry from `supersedes`; it does not
copy and delete where a rename crosses filesystems; and it does not check that
the replacing package covers what the old one did. (A save writes every artifact
again, as the sections of the forms say, but with the content it had.) It takes no lock:
two writers of one package at once get the last writer's files, as they do with
the library, and the state machine is what a second claim, made after the first
was written, meets. The decision to build it is
[ADR 10](adr/10-report-and-change-the-state-of-a-package-with-its-own-command.md).
