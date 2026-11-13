# The `speca` command

```
speca [flags] <spec>                     a specification package → its state
speca --list [--all]                     every package under the spec root → one entry each
speca --next                             the package to implement now, in dependency order
speca <spec> --task T --state S          change the state of tasks of a package
speca <spec> --status S [--by <spec>]    change the lifecycle status of a package
speca <spec> --render                    write the package's spec.md, its render
speca --render [--all]                   write the spec.md of every package
speca <spec> --migrate                   convert a version 2 package to version 3
speca --migrate [--all]                  convert every version 2 package
speca <spec> --gate task|full            run the package's linter and all_tests (affected_tests at the task level, when named) → one verdict
speca <spec> --gate-report               sum the package's gate runs since a commit
```

`speca` is the spec admin command. It reads a version 3 package through
`afspec` and reports what an agent with a shell needs to know about it: its
lifecycle status, whether it validates, every task with its state and whether
it is ready (the integration task a `full` package derives among them), the
task to work on next, the lifecycle moves allowed from where it is, and the
packages it depends on and that depend on it. It is built for an
agent to call — from a prompt, or from a skill's instructions — and follows the
same interface as `spec` and `specr`: **exactly one positional input**
(none with `--list` or `--next`, or with `--render` or `--migrate` for every package), **exactly one JSON object** on stdout on every
program-driven path, the help on stderr, and one exit-code table. The forms are
flags, not subcommands.

`spec` writes a package from an idea, `specr` renders one, `speca` reports and
changes its state. Each command's help and each `--schema` description say so,
in the sentence `cli.SiblingCommands`.

The report, `--validate`, `--list` and `--next` read the packages and change nothing.
The task form, the status form, the render form and the migrate form write.
The task form moves tasks through the library's task state machine and
writes `progress.json`, with its `updated_at`, through the library's
`Spec.SaveProgress`, and no other file. The status form moves a package
through its lifecycle (activating, sealing, archiving, superseding it) with
the library's `Transition` and `Supersede`, and moves an archived or
superseded package under `archive/`. The render form writes the package's
`spec.md` and nothing else. The migrate form converts a version 2 package to
version 3 in place with the library's `MigrateV2`. `Transition`, `Supersede`
and `Save` rewrite **every** artifact of the package (`prd.md`,
`requirements.json`, `tasks.json`, `progress.json`, and `architecture.md`
when there is one) in its canonical encoding, and write `spec.md`, the
package's render, beside them, through temporary files that they rename. What
a call changes in content is the status, but formatting the encoding does not
keep (a YAML comment in the frontmatter of `prd.md`, say) is lost, and the
files are left with mode `0600`, because the temporary files are created
private. Moving the directory under `archive/` is a rename: it rewrites no
file. With `--dry-run` no form writes anything. The gate form starts the
package's `linter` and `all_tests` commands as processes and keeps their logs,
the passes it reuses and a run log in the user's state directory, outside the
repository; it and the gate report write nothing under `--dir`. `speca` makes
no model call and no network call, and reads no credential (what the gate's
commands do is theirs).

```sh
speca 16                                # the state of package 16
speca .specs/16_indexed_code_search     # the same, by path
speca 16 --validate                     # the format's full verdict on it
speca --list                            # every package under the spec root
speca --list --all                      # and those under archive/
speca --next                            # the package to implement now
speca 16 --task next --state in_progress   # claim the task to work on now
speca 16 --task 4 --state done             # finish task 4
speca 16 --task 1,2 --state in_progress --dry-run   # see what that would do
speca 16 --status active                # activate a draft: the hash of its intent is recorded
speca 16 --status sealed                # seal an active package whose tasks are all done
speca 03 --status superseded --by 12    # 12 replaces 03; 03 goes under archive/
speca 16 --render                       # write spec.md, 16's render, and nothing else
speca --render --all --dry-run          # which packages' spec.md are not their render
speca 16 --migrate                      # convert 16 from version 2 to version 3, in place
speca --migrate --all --dry-run         # which packages, archive/ included, are still version 2
speca 16 --gate full                    # run 16's linter and all_tests; reuse a pass of the same tree
speca 16 --gate-report --since 4388b94  # the gates of 16 since that commit, summed
speca --schema                          # the interface, as JSON Schema
```

Two paths are for a person and print text instead: `--version` prints the
build identity (`speca`, then the version, then the commit and build time when
the build set them, from the variables in `version.go`), and `-h`/`--help` or a
bare invocation prints the help to stderr. `--help` exits 0 and writes nothing
to stdout. A bare invocation exits 2; when stdout is not a terminal it also
writes a usage envelope, so a program still gets one object to parse. An
invocation that names flags but no input, and is not `--list`, `--next`,
`--render`, `--migrate`, `--gate` or `--gate-report`, is a bare invocation;
`--gate` and `--gate-report` with no input are usage errors that say they need
a package.

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
| `--validate` | the format's full verdict on the package: the same identity, then `valid`, `validation` and `traceability`, and beside them the authoring `hints` |
| `--list` | one entry for every package under the spec root, in the order of their numbers |
| `--list --all` | the same, then the packages under `archive/` |
| `--next` | `next_spec`: the package to implement now; `all_done`: whether nothing is left; `waiting`: the open packages that cannot start, and why |
| `--task T --state S` | the report on the package after the change, with `changes`: the tasks moved, and where |
| `--task T --state S --dry-run` | the same, for a change that was not made: `dry_run` is true |
| `--status S` (`--by R` with `superseded`) | the report on the package after the change, read back from where it is now, with `changes`: the status, the record in the replacing package, and the move under `archive/`, in the order they were written |
| `--status S … --dry-run` | the same, for a change that was not made: `dry_run` is true |
| `--render` | `rendered`: the package, and what the call did to its `spec.md` |
| `--render --dry-run` | the same, for a write that was not made: `dry_run` is true |
| `--render` with no input | the sweep: `rendered` holds every package under the spec root, in the order of their numbers |
| `--render --all` with no input | the same, then the packages under `archive/` |
| `--migrate` | `migrated`: the package, and whether it was `written` as version 3, left `unchanged` (it was version 3 already) or `failed` |
| `--migrate --dry-run` | the same, for a conversion that was not written: `dry_run` is true |
| `--migrate` with no input | the sweep: `migrated` holds every package under the spec root, in the order of their numbers |
| `--migrate --all` with no input | the same, then the packages under `archive/` |
| `--gate task` or `--gate full` | `gate`: the verdict of the package's `linter`, then `all_tests`, or the recorded pass of the same tree reused |
| `--gate-report` | `gate_report`: the package's gate runs since a commit, summed |
| `--task N --evidence F` | the report on the package after the evidence of task N was recorded, with `changes` (one entry of kind `evidence`) and `evidence_log` |
| `--task N --evidence F --dry-run` | the same, for evidence that was not written: `dry_run` is true |
| `--verify-evidence` | `evidence_check`: every test recorded for every done task, run again on the tree as it is, with one verdict |

A call names one operation. `--validate` with `--list`, the task form or the
status form with either of them or with each other, `--next` with any other
operation or with an input, `--render` or `--migrate`
with any of them or with each other, a positional input with `--list`, and
`--all` without `--list` (or without `--render` or `--migrate` and with no
input) are usage errors. `--force` with `--render` or `--migrate` is one too:
neither form refuses anything `--force` could override; and so is `--all` with
either and an input, since `--all` adds `archive/` to the sweep and means
nothing for one package. `--gate` and `--gate-report` take a package and go with
no other operation, nor with each other; `--clean-env` and `--timeout` go with
`--gate`, `--base` with `--gate task`, and `--since` with `--gate-report`, and a `--gate` other than `task` or
`full` (case-sensitive, with `fix_hint.valid` `["task", "full"]`) or a
`--timeout` that is not a duration greater than zero is refused. `--evidence`
takes a package and `--task` with exactly one task id (not `next` or `all`),
and goes with no `--state`, no `--force` and no other operation;
`--verify-evidence` takes a package and goes with no other operation.
`--timeout` goes with `--gate`, `--evidence`, `--verify-evidence` and
`--state done`, the forms that may run a package's tests, and with no other.

## The report: `speca <spec>`

`result` holds the fields below. A field a form does not produce is absent, not
empty, except where the table says the empty list is written.

| Field | Meaning |
|---|---|
| `spec_dir` | the package directory, relative to `--dir` when inside it, absolute otherwise; runnable as the input of `speca` and `specr` |
| `spec_id`, `spec_name`, `title`, `status`, `created_at` | from the PRD frontmatter. `status` is `draft`, `active`, `sealed`, `superseded` or `archived` |
| `updated_at` | when a task of the package last changed state, from `progress.json`; absent when none has, or the package has no progress file yet |
| `intent_hash`, `supersedes`, `profile` | from the frontmatter; each is absent when it is unset or empty. `profile` is `full` or `lite`; absent means `full` |
| `valid` | the package's own verdict from `(*Spec).Validate`: the schemas and the cross-file rules C1 to C8. It is always written, so a `false` is not mistaken for an absent field |
| `failing_rules` | the rules the package's errors break (`C3`, `C7`, `json_schema`, …), each once, in the order they first appear; present only when `valid` is `false` |
| `intent_drift` | `true` when the package is `active`, has an `intent_hash`, and its PRD's `## Intent` section no longer hashes to it, or is gone. The library refuses every save of such a package, so a caller learns here why; absent otherwise |
| `contract_hash` | from the frontmatter: `{"requirements.json": …, "tasks.json": …}`, the SHA-256 of each contract file recorded at activation; absent for a draft and for a package activated before it was recorded |
| `contract_drift` | for a package with a `contract_hash`: `true` when `requirements.json` or `tasks.json` no longer hashes to it, `false` when both match; absent for a package without one. See [the contract hash](#the-contract-hash) |
| `evidence_required` | `true` when the package is active and has a `contract_hash`, so that its tasks close on evidence; absent otherwise |
| `tasks` | every task of the plan, in order, the derived integration task last: `id`, `title`, `kind` (`implement` for a task of `tasks.json`, `integration` for the derived one), `state` (from `progress.json`; `pending` for a task with no entry), `depends_on`, `ready`, and `evidence` when the task has some (see [the evidence form](#the-evidence-form-speca-spec---task-n---evidence-file)). `[]` when the plan has none |
| `task_counts` | `pending`, `in_progress`, `done`, `dropped` and `total`, the number of tasks, the integration task among them. A task whose state is none of the four (the package then does not validate) is in the total and in no count |
| `next_task` | the task to work on now, as the entries of `tasks`: the first task in progress, otherwise the first ready task; absent when there is none |
| `allowed_status` | the lifecycle states the package may move to from its status, in the order `draft`, `active`, `sealed`, `superseded`, `archived`. `[]` for a terminal status |
| `depends_on` | the spec ids in the package's own `tasks.json` `dependencies`, in file order, each once. `[]` when there are none |
| `dependents` | the spec ids of the other packages under the spec root that depend on this one, sorted, each once. `[]` when none does; absent when the spec root could not be read |

A task is **ready** when it is `pending` and everything it depends on is
`done`. Its dependencies are its `depends_on` when that is present, where an
empty list means none; when it is absent, the task before it in the plan, and
the first task then has none (format v3 §8.2). The integration task, which
depends on every task of the plan, is ready when each of them is `done` or
`dropped`. A dependency the plan does not have, or one that is `dropped`, is
not done. The same rule is `(*afspec.Spec).IsReady`, and `next_task` is
`NextTask`, so `speca`, `specr` and the `examples/tasks` program agree on what
"ready" and "next" mean. `ready` is written on a task only when it is true.

`dependents` is found with the spec root's dependency graph. A dependency on a
package that is not in the root does not stop it: the graph holds the edges
that were read.

A package in another format version (a PRD `schema_version` that is neither
absent nor 3) loads, but none of its progress or tasks is read: a version 2
task carries its state in `tasks.json`, and a version 1 plan is
`task_groups`, not `tasks`. It is reported with what did load — its `tasks`
are `[]` — and a high-severity `spec_invalid` warning saying so and naming
`speca <package> --migrate`, which converts a version 2 package. A version 3
package that does not validate raises no warning: `valid` and `failing_rules`
are the answer.

## The verdict: `speca <spec> --validate`

`result` holds the identity fields of the report and `intent_drift`, then:

| Field | Meaning |
|---|---|
| `valid` | the combined verdict: `true` exactly when no error was kept from the package's own validation or from the cross-spec rules. It equals `validation.valid` |
| `validation` | `valid`, `error_count`, `warning_count`, and `errors` and `warnings`, each finding with its `check` (the rule), `artifact`, `entity` and `message`. The package's own findings come first, then the cross-spec findings that concern it. The library names no artifact for the dependency cycle; `speca` reports it in `tasks.json` |
| `traceability` | the derived coverage, in the shape `spec` reports: `criteria_covered`, `criteria_uncovered`, `paths_covered`, `paths_uncovered` and `criteria_unowned` |
| `hints` | the authoring hints (below), each with its `check`, `artifact`, `entity` and `message`; `[]` when there are none. They are not the format's rules and never change `valid` |

The fields that are specific to the report (`tasks`, `task_counts`,
`next_task`, `allowed_status`, `depends_on`, `dependents`, `failing_rules`) are
absent.

The cross-spec rules (format v3 §10.3: an unknown dependency, a dependency
cycle, a missing shared actor between two packages that both have execution
paths, and the handoff rules below) need every package of the spec root, so this form loads each `NN_name` directory that
loads, adds the package itself when it is not among them — an archived package
is not — builds the dependency graph and runs `afspec.ValidateCrossSpec`. `spec`
gives every package it writes the same verdict (`specpkg.Verdict`), against the
spec root as it is then, before it activates the package. Of what that finds it keeps:

- the errors tagged with the package's `spec_id`, which the library gives to
  the downstream package that decides them (an unknown dependency, a missing
  shared actor), and to the package that wrote a handoff or a take;
- the `handoff_not_taken` warnings tagged with it, as the receiver;
- the dependency cycle, which names no package, when the package has an edge
  in the graph — one from it or to it — since a package with no edge cannot be
  on a cycle.

The handoff rules read the `handoffs` of `requirements.json` and the
`takes_handoffs` of each task (format v3 §6.4.1):

| Check | Of | When |
|---|---|---|
| `cross_spec_handoff_direction` | the sending package, on `requirements.json` | a handoff's `to` names no package under the spec root (an archived one included: it has landed), names the package itself, or names a package that does not depend on the sender, directly or transitively; the message names the handoff, its `to` and which it is |
| `cross_spec_handoff_taken` | the taking package, on `tasks.json` | a `takes_handoffs` entry names a package that does not exist, a handoff that package does not declare, or one addressed to another package; the message names the task and the entry |
| `handoff_not_taken` (a warning) | the receiving package, on `tasks.json` | a handoff another package addresses to it that no task takes, naming the sending package, the handoff's id and its `what` |

With `--strict-handoffs` (which goes with `--validate` only, and stores
nothing) each `handoff_not_taken` is an error instead, and `valid` is false
while one is left; the same call without the flag warns again.

A package under the root that fails to load is left out of the run. A spec root
that cannot be read skips the cross-spec rules, with a `specs_dir_unreadable`
warning, and the verdict is the package's own.

After the cross-spec rules comes the rule `steering_version` (format v3
§10.4), which reads the spec root's `steering.md` and is always a warning on
`prd.md`, never an error. It applies to a draft or active package whose PRD
frontmatter records `steering_version`, under a steering that is versioned
([configuration.md](configuration.md#the-steering)):

| The package's `steering_version` | Finding |
|---|---|
| the steering's version | nothing |
| older | one warning naming both versions and every amendment since, newest first, each with its version, date and change; when one of them raised the MAJOR number, it adds that a directive the package was written under has been removed or changed |
| newer than the steering's | one warning naming both versions and saying the package's is newer |
| in no amendment of the record | one warning naming both versions and saying so |

A sealed, superseded or archived package, one with no `steering_version`, and
any package under an unversioned or malformed steering get nothing. A person
who has checked the package against the amendments clears the warning by
setting `steering_version` to the steering's version, and then runs
`speca <spec> --render`, since a hand edit of `prd.md` leaves `spec.md` behind
it; the field is outside the hashed `## Intent`, so an active package takes the
change.

The package's own validation includes the rule `rendered` (format v3 §10.2),
which compares the package's `spec.md` with its render, in `artifact`
`spec.md`:

| `spec.md` | `draft` or `active` | `sealed`, `superseded` or `archived` |
|---|---|---|
| absent | warning: `spec.md is missing; run speca <dir> --render` | the same warning |
| not the render, from some line on | error: `spec.md is not the package's render from line N on; run speca <dir> --render` | the same, as a warning |
| there and unreadable | error: `spec.md cannot be read (<reason>); run speca <dir> --render` | the same, as a warning |
| the render | nothing | nothing |

`<dir>` is the name of the package's directory. Because the rule is part of the
package's own validation, the report's `valid`, each `--list` entry's `valid`
and the status form's activation check see it too: a draft whose `spec.md` is
stale is not activated until `speca <spec> --render` writes it again (or
`--force` activates it with a high `spec_invalid` warning naming `rendered`).

### The authoring hints

`result.hints` holds the checks `spec` makes on a package while it generates
one, which a package written by hand or in a session would otherwise never
meet ([ADR 20](adr/20-report-the-authoring-hints-beside-the-verdict.md)). They
come from `internal/authoring`, the same code `spec` runs, in this order:

| `check` | `artifact` | `entity` | What it names |
|---|---|---|---|
| `vague_language` | `requirements.json` | the criterion id | each vague word (`appropriate`, `properly`, `correctly`, `reasonable`, `relevant`, `adequate`, `suitable`, `as needed`, `if necessary`, `etc`) in a criterion's `action`, then its `condition` |
| `line_reference` | `tasks.json` | the task id | each step or `touches` entry that names a line of a file (`store.go:88`, `render.go:12-30`, `line 40`) |
| `foreign_test_command` | `tasks.json` | `test_commands.<field>` | each of `all_tests`, `linter`, `spec_tests` and `affected_tests` whose program belongs to a known ecosystem the repository under `--dir` does not have, with the repository's own command where it has one. A program in no known ecosystem (`./scripts/ci.sh`, `make check`) and a repository of no detected language give nothing |

The format's validation matches no words (format v3 §0, rule 3; [ADR
18](adr/18-allow-extension-keys-and-move-word-checks-out-of-the-validator.md)),
so a hint is never an error or a warning of `validation`: a package with hints
validates, activates and is implemented as one without. `spec` refuses a
foreign test command and sends the other two back to the model once; `speca`
only reports them.

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

## The next package: `speca --next`

`--next` answers "which package do I implement now?" for an agent, or a
script, that works through a spec root one package at a time. It reads every
`NN_name` directory under the spec root as `--list` does, and the packages
under `archive/` for their spec ids alone.

A package is **open** when it loads as version 3, is a `draft` or `active`,
and has a task, the integration task included, that is `pending` or
`in_progress`. A package is **finished** when it is `sealed`, `superseded` or
`archived`, or when it is version 3 and none of its tasks is `pending` or
`in_progress`. A dependency — a spec id in a package's `tasks.json`
`dependencies` — is finished when every package under the spec root with that
id is finished, or, when none has it, when a package under `archive/` does: an
archived package is history, and nothing more will be done in it.

The packages under the spec root are put in **dependency order**: each after
every package it depends on, and of the packages that could come next, the
first in number order. `next_spec` is the first open package in that order
whose every dependency is finished. So the next package is not always the
lowest-numbered open one: `01` that depends on `03` comes after `03`.

| Field | Meaning |
|---|---|
| `next_spec` | the package to implement now, as an entry of `--list` (`spec_dir`, `spec_id`, `spec_name`, `title`, `status`, `valid`, `task_counts`, `next_task`); absent when there is none. It is chosen whether or not it validates: `valid` is there to say |
| `all_done` | `true` when nothing is left to implement: no package under the spec root is open, and every one of them loads as version 3. Always written. `false` with no `next_spec` means every open package waits on something |
| `waiting` | in number order, every package under the spec root that is open but has a dependency that is not finished, every one that does not load, and every one in another format version that is not finished, each with `spec_dir`, `spec_id` (absent when it did not load), `waits_on` (the ids of its dependencies that are not finished, in the order its `tasks.json` names them) and `reason`, which says of each such id whether its package is open or missing from the spec root and `archive/` altogether. `[]` when there are none |

There are three outcomes, all exit 0: a `next_spec`; `all_done` true; or
neither, and `waiting` says what holds every open package. A package that does
not load, or is version 2, is never chosen and keeps `all_done` false, but it
blocks only the packages that depend on its id.

The dependencies of the packages under the spec root must not form a cycle — a
package that depends on itself included, and whatever the state of the
packages in it. A cycle fails the call at stage `graph`, exit 1, with no
result; the message names the packages of one cycle in order, each depending
on the one after it (`.specs/02_b → .specs/03_c → .specs/02_b`). A dependency
on an id no package has is no edge of the graph, and so no cycle; it leaves its
package waiting.

A spec root that cannot be read fails the call at stage `load`, as for
`--list`; an `archive/` that cannot be read raises a `specs_dir_unreadable`
warning, and a dependency on a package there is then not finished.

`specrun` runs `af-impl` once for each package this choice names, through the
same library function, `afspec.ChooseNext`, until every package is done, and
stops when no package can start ([specrun.md](specrun.md)).

## The task form: `speca <spec> --task T --state S`

The task form moves one or more tasks of a package to a state, through the
library's task state machine, and writes its `progress.json`. It takes one
input, as the report does. The integration task of a `full` package, task
`N+1`, is moved like any other.

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

**The state machine decides every move** (format v3 §8.1):

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
3. the package is in format version 3: a version 1 or version 2 package is a
   usage error, with or without `--force`, because none of its task states can
   be read; the message names `speca <package> --migrate`;
4. the package can take a write (stage `status`): it is a draft, or active with
   its intent matching its `intent_hash` and its contract files matching its
   `contract_hash`. A sealed, superseded or archived package, an active one
   whose `## Intent` section drifted or is gone, and an active one whose
   `requirements.json` or `tasks.json` changed since activation (the message
   names each file that differs, and says to restore it or to write a package
   that supersedes this one; there is no `fix_hint`) are refused. `--force` and
   `--dry-run` change nothing here;
5. the ids: an id the plan does not have, `next` with no next task (a plan with
   no tasks says so, rather than that every task is done), and `all` on a plan
   with no tasks are usage errors;
6. the moves (stage `task`);
7. in a package that requires evidence, the evidence of each task the call
   closes (stage `evidence`; see [closing a task on its evidence](#closing-a-task-on-its-evidence));
8. the keys (stage `validate`), when a write would happen: `SaveProgress`
   re-encodes `progress.json` and drops the keys the schema does not allow, so a
   package whose progress file holds any would lose them. That is refused,
   listing each key (`progress.json /my_key`), unless `--force` is given. The
   other artifacts are not written by a task move, so their keys are safe. A
   package that does not validate for another reason still takes the change.

**`--force`** applies when `--state` is `done` or `pending`: a move the machine
refuses is then made with the library's own bypasses, `CompleteTaskStates` or
`ResetTaskStates`, and a move it allows still goes through `TransitionTask`. A
task already in the state is no move. Each task set through a bypass has a high
`task_state_forced` warning and `forced: true` on its change. The library has no
setter for `in_progress` or `dropped`, and `speca` writes no state by hand, so
`--force` covers nothing there. `--force` also lets the write go over the keys of
step 8, and closes a task without the checks of its evidence (step 7). It skips no usage error, no status refusal and no legacy refusal.

**`--dry-run`** runs every check, expansion, warning and refusal and does not
write. The result is the one the call would have produced, with `dry_run: true`,
the changes and the `updated_at` it would have set. A refusal is the same
refusal, with the same exit code. Its `next` is one entry, the same call without
`--dry-run`, naming the ids it resolved (`--task 1,2`, not `all`) and keeping
`--force`; with nothing to change it has none.

**The write.** When at least one task changes, the progress file's
`updated_at` is set to the time of the call, UTC, RFC 3339, and
`progress.json` is written with `Spec.SaveProgress`, through a temporary file
it renames; no other file of the package changes, so `git diff` after a task
move holds `progress.json` alone. A call that changes nothing writes nothing
and leaves `updated_at` as it was. If the write fails, a failure while writing
the temporary file leaves the package unchanged, and one during the rename may
have replaced `progress.json`; the message says so and tells the caller to run
`speca <spec>`.

**The result** of a call that succeeds is the report of the package as it was
written (its `valid` and `failing_rules` are the verdict on the package with
the progress `SaveProgress` wrote, which holds none of the keys it deleted),
with every field the report has,
and `changes`: one
`{"kind": "task", "id", "from", "to"}` for each task whose state changed, in plan
order, with `"forced": true` where a bypass was used.

**A refusal** (stage `status`, `task` or `validate`), and a failed write, return
the report of the package as it is on disk, with no `changes`, so the caller sees
the states without another call. Its `summary` is the refusal's message and it has
no `next`: a suggestion after a refused claim could send one agent to the task
another holds. A usage error or a package that does not load has no result.

## The contract hash

`speca <spec> --status active` records `contract_hash` in the frontmatter of
`prd.md`, beside `intent_hash`: the SHA-256 of `requirements.json` and of
`tasks.json`, each over its canonical encoding (the encoding `Save` writes)
with every `x-` extension key left out, keyed by the file's name:

```yaml
contract_hash:
  requirements.json: "<64 hex digits>"
  tasks.json: "<64 hex digits>"
```

Another encoding of the same content, and adding, changing or removing an `x-`
key, leave the hash as it was; any other change to a file changes its hash.
`--status active --dry-run` reports the hash the activation would record.

A package **requires evidence** when it is active and has a `contract_hash`.
A draft, and a package activated before the hash was recorded, do not: the
presence of the hash is the switch, and there is no setting. While an active
package's contract files no longer match its hash (`contract_drift: true`),
every call of the task form, the evidence form and `--status sealed` is
refused at stage `status`, with nothing written, `--force` or not; the report,
`--validate`, `--gate`, `--gate-report`, `--render` and `--verify-evidence`
still answer. There is no call that accepts a changed contract: the files are
restored, or a package that supersedes this one is written (ADR 26).

## The evidence form: `speca <spec> --task N --evidence <file>`

The evidence form records, for task N of a package that requires evidence, the
run in which each of its tests failed: the run that has to happen once its
tests are written and before it is implemented. The file is at most 1 MiB:

```json
{
  "evidence": [
    { "covers": "REQ-1.1", "tests": ["./cmd/speca:TestGate/full"] },
    { "covers": "REQ-1.2", "tests": ["./cmd/speca:TestGate/task"],
      "no_red": "the level is reported by code REQ-1.1 already added" }
  ]
}
```

`covers` is a criterion the task owns (a requirement id in the task's
`criteria` stands for its every criterion); for the integration task it is an
execution path. `tests` holds 1 to 50 test identifiers of 1 to 500
characters, with no line break; a file names at most 500 distinct tests, one
test may appear under several entries, and one `covers` under one. `no_red` is
an exemption, a reason of 1 to 500 characters for a criterion whose behaviour
existed before the task. A test is **exempt** when every entry that lists it
carries `no_red`.

**The tests run** through the package's `evidence_tests` command (format v3
§7.1): split as the gate splits a command, with no shell, `{tests}` replaced by
one argument per identifier, started in `--dir`, its exit code deciding
nothing. Each test takes the status of its result line on stdout,
`{"test": <identifier>, "status": "pass" | "fail" | "error" | "missing" |
"skipped"}`; every other line is ignored, a test with no line is `missing`, and
one with two lines of different status is `error`. A package that names no
`evidence_tests` in a `--dir` that holds a `go.mod` uses the runner built into
`speca`: identifiers are `<package>:<Test>` or `<package>:<Test>/<subtest>`,
the package as `go test` takes it (`./cmd/speca`); it runs `go test -json
-count=1` once per package with a `-run` pattern that matches exactly the
tests named, and reads their `pass`, `fail` and `skip` events. Every test of a
package that does not build is `error`; a name that matches no test, and an
identifier with no `:`, are `missing`. The run's stdout and stderr go to a log
in the gate's log directory, named in `evidence_log`; where the state
directory cannot be written there is no log, and a low
`evidence_log_not_written` warning says so. A run still going at `--timeout`
(default `20m`) is ended with every process it started.

**Each test is judged**: one that already has a recorded failing run keeps it,
whatever its status now; an exempt test, and every test of the integration
task, must be `pass` or `fail` and gets no failing run; every other test must
be `fail`, and the run is recorded as its failing run, `{"at", "tree"}`, the
time and the gate's tree id of the working tree (no `tree` outside a git work
tree). When every test meets its rule, the task's evidence in `progress.json`
is replaced by the file's entries with their runs (the runs of a test no
longer listed are dropped), `updated_at` is set, and no other file is
written. The result is the report, with the task's `evidence`, one change
`{"kind": "evidence", "id": N}` and `evidence_log`. `--dry-run` runs the tests
and every check and writes nothing.

**Refusals**, each with nothing written and exit code 1:

| Stage | When |
|---|---|
| `status` | the package cannot take a write (as the task form's step 4), its changed contract included |
| `evidence` | the package is a draft, or has no `contract_hash`; the task is not `in_progress`; a `covers` the task does not own, or given twice (every one named, before any test runs); a test that had to fail is `pass` (it passes without the implementation, and needs `no_red` if the behaviour already existed), `error` (a build or collection failure is not a failing test), `missing` or `skipped`, and an exempt test that is neither `pass` nor `fail` (every one named); the tests could not be run: no `evidence_tests` and no `go.mod`, a program not found, a command that cannot be split, a timeout |
| `validate` | `progress.json` holds keys its schema does not allow, which the write would delete |

`--evidence` without `--task`, with `--state` or `--force`, with more than one
task id, `next` or `all`, and a file that does not exist, is over 1 MiB, is not
an evidence file or breaks one of its limits, are usage errors, refused before
the package is read.

### Closing a task on its evidence

In a package that requires evidence, `--state done` checks each task it would
close, after the state machine's own checks:

1. every criterion the task owns has an evidence entry (for the integration
   task, every execution path);
2. every test of those entries has a recorded failing run or is exempt (not
   for the integration task, whose paths the tasks before it made live);
3. every test, run now through the evidence command, is `pass`.

When every task of the call passes, each test's passing run is recorded with
its time and tree id, and the states and the evidence are written to
`progress.json` in one write; each exempt entry is a low `evidence_no_red`
warning naming the task, the criterion and the reason, and `evidence_log` names
the run's log. Otherwise the whole call is refused at stage `evidence`, exit
code 1, nothing written for any task, naming each criterion without an entry,
each test without a failing run and each test whose status is not `pass`, with
`fix_hint.flag` `--force`. `--state done --force` closes without the checks and
without running a test, keeps the evidence as it is, sets `forced: true` on the
change and raises a high `task_evidence_forced` warning. A move to `pending`
removes the task's evidence; a move to `dropped` needs none. `--dry-run` runs
the tests and the checks and writes nothing.

## The verify form: `speca <spec> --verify-evidence`

The verify form runs every test recorded for every `done` task once, on the
tree as it is, and answers in `evidence_check`, writing nothing to the package,
whatever its status (a package whose contract drifted included). It takes
`--timeout` and `--dir`, and exits 0 for every verdict, as the gate does; 1 only
when the package cannot be loaded.

| Field | Meaning |
|---|---|
| `verdict` | `pass` when every test is `pass`; `fail` when one is not; `not_run` when no done task has evidence or the tests could not be run |
| `reason` | with `not_run`, why |
| `tasks` | each done task with evidence, in plan order: `id`, and `evidence`, each `covers` with its `tests`, each `{test, status}`. `[]` when none was checked |
| `log` | the run's log, as for the evidence form |

## The status form: `speca <spec> --status S`

The status form moves a package through its lifecycle (format v3 §9) and writes
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
   other than 3 (`speca` changes the state of version 3 packages only: a write
   would discard what the version 3 types do not read of a version 1 or 2
   package; the message names `speca <package> --migrate`), are refused,
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
   `--force`. The library reports those of the JSON artifacts, `progress.json`
   included; the frontmatter of `prd.md` is looked at by `speca`. A move-only call writes no
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
  the hash of the intent and the [contract hash](#the-contract-hash). The
  package stays where it is.
- `sealed`. A package whose intent has drifted cannot be sealed, and `--force`
  does not change that: a drifted package cannot be saved, and sealing would make
  the drift permanent. The message says to restore the intent that was activated,
  or to write a new package that supersedes this one; there is no `fix_hint`. A
  package whose contract files no longer match its `contract_hash` is refused
  the same way, naming the files. A task still `pending` or `in_progress`, the integration task included, refuses
  it too, naming each with its state, with `fix_hint.flag` `--force`. The write is
  `Transition("sealed")`.
- `archived`, from `draft`, `sealed` or `superseded`. A package not under
  `archive/` gets `Transition("archived")` and then the move; one already under
  it gets the status only and stays. A move-only call (the package is `archived`
  and not under `archive/`) only moves it: no file is rewritten.
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

**The writes.** `updated_at` is not touched: it is `progress.json`'s, and says
when a task last changed state, which a lifecycle move is not. The warnings
are raised once the first write has been made: a call that is refused, or
whose first write fails, raises none.

**`--dry-run`** runs every check and plans every warning, writes and moves
nothing, and reports the package as it would be left, held in memory: its status,
the intent hash an activation would record, and the directory it would move
to. `dry_run` is `true`, and the changes, warnings, refusals and exit
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

## The render form: `speca <spec> --render` and `speca --render [--all]`

Every save of a package writes its render, `spec.md`, beside its artifacts
(format v3 §3): a header line, a blank line, and what `specr <spec>` returns in
`result.markdown`. The render form brings that file up to date without saving
the package: it loads the package, composes the render
(`afspec.(*Spec).RenderedBytes`), and writes it to `spec.md` through a temporary
file it renames (`cli.WriteAtomic`), whatever the package's status — `spec.md`
is derived, outside the package's contract, and touches no intent hash. It
writes no other file. The render holds the contract and never the progress, so
a task move does not change it.

`result.rendered` holds one entry for the package named, or, with no input,
one for every package under the spec root in the order of their numbers, then
with `--all` one for every package under `archive/` — the sweep, which goes
through the directories as `--list` does. Each entry is:

| Field | Meaning |
|---|---|
| `spec_dir`, `spec_id`, `spec_name`, `status` | the package, as in the report |
| `rendered` | `written` when `spec.md` was absent or differed from the render, and was written; `unchanged` when it held the render already, and was left alone (its bytes and its modification time); `failed` in the sweep, when the package could not be loaded or its `spec.md` could not be written |
| `reason` | why the package failed; present exactly when `rendered` is `failed` |
| `archived` | `true` for a package rendered from `archive/`; absent otherwise |

With `--dry-run` the outcome is decided the same way, `dry_run` is `true`, and
nothing is written. When one package is named, a package that cannot be loaded
fails the call at stage `load`, naming its directory and the library's reason,
and a `spec.md` that cannot be written fails it at stage `write`, naming the
path and why; neither writes a file. The sweep has every package to report, so
a package it cannot load or write is an entry with `rendered` `failed` and its
`reason`, the others are rendered all the same, and the call exits 0. A spec
root that does not exist, is not a directory or cannot be read fails the sweep
at stage `load` with `fix_hint.flag` `--specs-dir`, as it fails `--list`; one
that holds no package is `"rendered": []`. The `summary` counts the outcomes,
`speca: <n> written, <n> unchanged, <n> failed`, with
` (dry run, nothing written)` under `--dry-run`; `next` is empty.

## The migrate form: `speca <spec> --migrate` and `speca --migrate [--all]`

The library reads version 3 only, and format v3 §13 maps every version 2
construct to its version 3 form deterministically. The migrate form performs
that conversion in place, whatever the package's status, with the library's
`afspec.MigrateV2`: it reads the version 2 package, builds the version 3 one in
memory and writes every file of it — `prd.md`, `requirements.json`,
`tasks.json`, `progress.json`, `architecture.md` when there is one, and
`spec.md`, rendered again — through the temporary files and renames a save
uses, then removes `test_spec.json`. In short: every `REQ` and `PATH` id loses
its `{spec_id}-` prefix; each criterion takes its `check` from the tests that
verified it and each path its `smoke` check from its smoke tests; a
`complex_event` criterion becomes `event_driven` with its guard joined into the
condition; a `system` shared by every criterion of a requirement moves up to
it; `introduction` is dropped and a glossary becomes a `## Glossary` section
of the PRD; `updated_at` moves to `progress.json` with every task's state, the
authored integration task leaving the plan to become the progress entry of task
`N+1`; `owner` and `tags` become `x-owner` and `x-tags` when set. The
`## Intent`, the `intent_hash` and the status are not changed, so an active
package stays active with the hash it had. A version 2 package with a
criterion no test verifies, or a path no smoke test verifies, has no
deterministic conversion and is refused, naming the gap; version 2's own rules
forbid both, so a valid version 2 package always converts.

`result.migrated` holds one entry for the package named, or, with no input, one
for every package under the spec root in the order of their numbers, then with
`--all` one for every package under `archive/` — the sweep, which goes through
the directories as `--list` and `--render` do. Each entry is:

| Field | Meaning |
|---|---|
| `spec_dir`, `spec_id`, `spec_name`, `status` | the package, as in the report; the identity is absent when the package did not load |
| `from_version` | the `schema_version` the package declared before the call; absent when it did not load |
| `migrated` | `written` when the package was version 2 and was converted; `unchanged` when it is version 3 already, and was left alone; `failed` when it is neither, could not be read, has a gap the conversion refuses or could not be written |
| `reason` | why the package failed; present exactly when `migrated` is `failed` |
| `archived` | `true` for a package converted from `archive/`; absent otherwise |

With `--dry-run` the conversion is made in memory and nothing is written:
`dry_run` is `true`, and a package that would convert is `written`. When one
package is named, a package that fails is a failure of the call at stage
`migrate`, category `input`, with the reason, and nothing is written. The sweep
has every package to report, so a package it cannot convert is an entry with
`migrated` `failed` and its `reason`, the others are converted all the same,
and the call exits 0. A spec root that does not exist, is not a directory or
cannot be read fails the sweep at stage `load` with `fix_hint.flag`
`--specs-dir`, as it fails `--list`; one that holds no package is
`"migrated": []`. The `summary` counts the outcomes,
`speca: <n> written, <n> unchanged, <n> failed`, with
` (dry run, nothing written)` under `--dry-run`; `next` is empty.

The conversion is run once over a spec root and never again: a version 3
package is reported `unchanged`, and a sweep over a converted root writes
nothing. The decision is
[ADR 19](adr/19-migrate-the-format-to-version-3.md).

## The gate form: `speca <spec> --gate task|full`

The gate decides whether a task or a branch may land: the package's own
`linter` command, then its `all_tests` command, from `test_commands` in
`tasks.json`. The gate form runs them in the repository `--dir` names,
whatever the package's lifecycle status, and answers with one verdict in
`result.gate`. The level, `task` (after an implement task) or `full` (the
baseline, after the integration task, after the conformance fixes and on the
merged tree), is part of the answer and of the key a pass is recorded under;
both levels run the same two commands, except that a task gate of a package
whose `test_commands` name `affected_tests` runs that one in place of
`all_tests` (below). The decision is
[ADR 22](adr/22-run-the-gate-with-speca.md).

```sh
speca 16 --gate full                    # the baseline, or the merged tree
speca 16 --gate task                    # after an implement task
speca 16 --gate full --clean-env        # the conformance pass's clean environment
speca 16 --gate task --timeout 10m      # end a command still running after 10 minutes
speca 16 --gate task --base 3f2a9c1     # the affected tests of the change since 3f2a9c1
```

- **The affected tests.** At the task level, a package with
  `test_commands.affected_tests` runs `linter`, then that command, named
  `affected_tests`, and not `all_tests`: the tests the task's change can
  affect, as the project's own tool decides. `{base}` in it is replaced by the
  full id of the commit `--base` names, `HEAD` when it is not given, then the
  string is split and run as every gate command; `result.gate.base` is that
  id. The command as it ran is part of the key a pass is recorded under, so
  two bases are two keys. A package without `affected_tests` runs `all_tests`
  as before, and has no `base` whether or not `--base` was given. A `--base`
  that names no commit, a `--dir` outside a git work tree for a package with
  `affected_tests`, and `--base` with `--gate full` or without `--gate` are
  usage errors, made before anything runs; a `--base` is checked whenever it
  is given, also for a package without `affected_tests`. An `affected_tests`
  that does not hold `{base}` exactly once (a package that does not
  validate) ends the gate with `not_run`. The full level always runs
  `all_tests`.

- **No shell.** Each command string is split on white space, a stretch inside
  single or double quotes kept as one argument without its quotes, and the
  program is started directly (`gate.Split`). A pipe, `&&`, a redirection,
  `$VAR` or `*` reaches the program as a literal argument. `make test` and
  `go test ./... -run 'A|B'` work; anything that needs a shell does not.
- **First failure stops.** A command that exits with a code other than 0 ends
  the gate with `fail`, and the commands after it do not start.
- **`not_run`.** A command that cannot start ends the gate with `not_run`, and
  `reason` names the command and why: its program is not found, its string
  is empty or has an unterminated quote, or `test_commands` names no such
  command. A command that could not start has no entry in `commands`.
- **The timeout.** On Linux and macOS each command runs in a process group of
  its own (elsewhere the timeout ends the command's own process only). One
  still running when `--timeout` (a Go duration, default `20m`) has passed is
  ended together with every process it started, its entry has `timed_out` and
  no `exit_code`, and the gate ends with `fail`. A process a command left
  running when it exited is ended too; if it held the command's output open,
  `speca` waits two seconds for it, and the command's own exit code stands.
- **The output.** Each command's stdout and stderr go together to a log file
  of its own under the state directory, named in its `log`. None of it reaches
  `speca`'s stdout, which stays one JSON object. A command that fails or times
  out carries the last 40 lines of its output in `tail`, all of them when it
  printed fewer, `""` when it printed nothing; the tail is kept as the output
  is written, and the log is not read back. A command that exits 0 has no
  `tail`.
- **The units.** A full gate reads its `all_tests` output, as it is written,
  for Go's per-package result lines: `ok <package> <n>s` and
  `FAIL <package> <n>s`, with or without text after the duration. Lines with
  `(cached)`, `[no test files]` or `[build failed]` are not units. The five with
  the most seconds are `slowest`, slowest first, equal ones in output order;
  for an output with no such line (pytest, Jest, a linter's) the list is `[]`.
  A task gate has no `slowest`.
- **The clean environment.** With `--clean-env`, every command runs with
  `HOME` set to an empty directory made for this gate outside the repository,
  `GIT_CONFIG_GLOBAL` and `GIT_CONFIG_SYSTEM` set to the null device,
  `GIT_CONFIG_NOSYSTEM=1` and `GIT_TERMINAL_PROMPT=0`; every other variable is
  inherited. The directory is removed when the gate ends, whatever its verdict,
  a timeout included, with what the commands wrote into it (read-only
  directories, such as Go's module cache, are made writable first). Without the
  flag the commands inherit `speca`'s environment unchanged.

`result.gate` holds:

| Field | Meaning |
|---|---|
| `level` | `task` or `full` |
| `verdict` | `pass`, `fail` or `not_run` |
| `clean_env` | `true` when the commands ran with `--clean-env`; always written |
| `base` | a task gate that ran `affected_tests` only: the full id of the commit that replaced `{base}` |
| `reason` | why the gate is `not_run`; present exactly then |
| `reused` | the recorded pass the gate was answered from: `commit` (checked out when it ran), `at` (RFC 3339) and `seconds` (its total); absent when the commands ran |
| `commands` | one entry per command that ran, in run order: `name` (`linter`, `all_tests`, or `affected_tests` for a task gate that ran it), `exit_code` (absent when it timed out), `seconds` (to the millisecond), `log` (absent when the file could not be written), `tail` (a failure only) and `timed_out`; `[]` when a pass was reused |
| `seconds` | the sum of the commands' seconds; `0` when a pass was reused |
| `slowest` | a full gate only: `{unit, seconds}`, at most five; `[]` when there are none |
| `budget` | a full gate only, run or reused, whatever its verdict: `seconds` (`gate_budget_seconds` of the steering's frontmatter, else `120`), `exceeded` (more seconds than the budget; exactly the budget is not) and `over_by_seconds` (when exceeded); a reused gate is measured by its recorded run's seconds. A steering frontmatter that breaks a rule, a budget that is no whole number above zero among them, raises a high `steering_malformed` warning, and the default applies |
| `growth` | a full gate only: `base_seconds`, the passing full gate recorded for the tree of the merge base of `HEAD` and the base branch with the same commands, directory and environment, and `grew_by`, the difference over `base_seconds` to four decimals (negative when faster); absent with no such record, no base branch or no git work tree, and on that tree itself |

A full gate that passed, from a repository whose `all_tests` prints two Go
result lines (the samples are the command's output, with the temporary state
directory they ran under written as `~/.local/state`; a real `log` is an
absolute path):

```json
"gate": {
  "level": "full",
  "verdict": "pass",
  "clean_env": false,
  "commands": [
    { "name": "linter", "exit_code": 0, "seconds": 0.137,
      "log": "~/.local/state/agent-fox/speca/docrepo-348918aa290c4691/logs/20261112T165927Z-linter-61794451.log" },
    { "name": "all_tests", "exit_code": 0, "seconds": 0.148,
      "log": "~/.local/state/agent-fox/speca/docrepo-348918aa290c4691/logs/20261112T165927Z-all_tests-3039488476.log" }
  ],
  "seconds": 0.285,
  "budget": { "seconds": 120, "exceeded": false },
  "slowest": [
    { "unit": "github.com/acme/search", "seconds": 1.208 },
    { "unit": "github.com/acme/index", "seconds": 0.64 }
  ]
}
```

The same call again, on the same tree (`summary`
`speca: 16_indexed_code_search full gate pass, reused from 4388b94`):

```json
"gate": {
  "level": "full",
  "verdict": "pass",
  "clean_env": false,
  "reused": { "commit": "4388b9495c00bde4933113a81f32c2aeb7e5ea1d", "at": "2026-11-12T16:59:27Z", "seconds": 0.285 },
  "commands": [],
  "seconds": 0,
  "slowest": [
    { "unit": "github.com/acme/search", "seconds": 1.208 },
    { "unit": "github.com/acme/index", "seconds": 0.64 }
  ]
}
```

A task gate whose tests fail reads the failure from `tail`, without opening
the log:

```json
{ "name": "all_tests", "exit_code": 1, "seconds": 0.003,
  "log": "~/.local/state/agent-fox/speca/docrepo-348918aa290c4691/logs/20261112T165928Z-all_tests-250984260.log",
  "tail": "--- FAIL: TestSearch (0.01s)\n    search_test.go:12: want 3 hits, got 2\nFAIL\nFAIL\tgithub.com/acme/search\t0.412s" }
```

### Reusing a pass

Every gate that ends in `pass` in a git work tree is recorded, and a later
gate with the same key is answered from the record: it starts no command and
returns `pass` with `reused`. The key is:

- the **tree id** of the working tree as it is: tracked and untracked files
  included, ignored files left out. `speca` copies the index to a temporary
  file and has git add the whole tree to the copy, writing the new objects to
  a temporary object directory, then `git write-tree --missing-ok`. The user's
  index, the staged changes and everything under `.git`, the modification
  times of the objects included, are left as they were;
- the **directory** the commands run in (`--dir`), as a path under the work
  tree's top level, so that two directories of one repository run gates of
  their own;
- the **level**;
- the exact `linter` and `all_tests` **command strings**, or, for a task gate
  that ran it, the `affected_tests` string as it ran, its base in place;
- whether the environment was **clean**.

Two commits with the same tree are the same code, so committing the change a
pass covered, or merging with `--no-ff` a branch whose tip passed while the
base branch did not move, is answered from that pass, with `reused.commit` the
commit that was checked out when it ran. A clean-environment run is never
answered by a normal one, nor the other way round. A `fail` or a `not_run` is
never recorded: a failure is cheap to run again and must be seen.

### The state directory

What the gate keeps lives outside every repository, in `agent-fox/speca` under
`$XDG_STATE_HOME` when it is set to an absolute path, else under
`~/.local/state`, on Linux and macOS alike
([configuration.md](configuration.md#the-gates-state-directory)). Each
repository has a directory of its own there, named after the base name of its
top level and a hash of the top level's path (of `--dir` outside a git work
tree):

| Path | What it holds |
|---|---|
| `logs/<time>-<command>-<n>.log` | each command's complete output, one file per command and gate |
| `passes/<sha256 of the key>.json` | a recorded pass: its key, the commit checked out, the time, the seconds and the slowest units; written through a temporary file that is renamed |
| `runs.jsonl` | the run log: one line per gate that ended in `pass` or `fail`, reused ones included, with the package's spec id, the time, the commit, the level, `clean_env`, the verdict, the seconds, the run it reused and the slowest units. A `not_run` gate is not logged |

Nothing is written under `--dir`. Two things can keep a gate from being
recorded, and neither stops it: `--dir` is not inside a git work tree or git is
not installed, so there is no tree id; or the state directory cannot be
created or written, in which case the commands also run without a `log`. The
gate runs and answers with its verdict, and a low `gate_not_recorded` warning
says why. A record that cannot be parsed counts as none, and the next pass
replaces it.

The `summary` is `speca: <directory> <level> gate <verdict> in <n>s`,
`speca: <directory> <level> gate pass, reused from <short commit>` for a
reused pass, or `speca: <directory> <level> gate not_run: <reason>`. `next` is
empty: what follows a gate is the caller's decision. The call exits 0 for
every verdict, and fails (exit 1, stage `load`) only when the package cannot
be read.

## The gate report: `speca <spec> --gate-report [--since <commit>]`

The gate report sums the gates recorded for the package, for the merge commit
and an implementer's report: what the gates cost, measured, not remembered.
It reads the run log of the repository and writes nothing.

```sh
speca 16 --gate-report --since 4388b94  # the runs since the baseline's commit
speca 16 --gate-report                  # since the merge base with the base branch
```

A run counts when it is of this package (its `spec_id`), ended in `pass` or
`fail` (a reused one included), and the commit that was checked out when it
ran is `--since` itself, or a descendant of it that `HEAD` reaches, or, while
a merge is uncommitted, that `MERGE_HEAD` reaches
(`git rev-list --ancestry-path <since>..HEAD`, and `..MERGE_HEAD`). Runs are
selected by ancestry, not by time, so a base branch that moved, or a gate on
another branch, changes nothing; `MERGE_HEAD` is included because the gate on
the merged tree runs before the merge is committed. Without `--since` the
commit is the merge base of `HEAD` and the base branch: the branch
`refs/remotes/origin/HEAD` names, else `main`. `af-impl` passes `--since`,
the commit its baseline ran on.

`result.gate_report` holds:

| Field | Meaning |
|---|---|
| `since` | the full id of the commit counted from |
| `full`, `task` | for each level: `runs`, `seconds` (the runs' own, a reused run adding `0`) and `reused` |
| `baseline_full_seconds` | the seconds of the earliest full run counted; a reused run's are those of the run it came from; absent when none is counted |
| `last_full_seconds` | the same, of the latest full run counted |
| `slowest` | the slowest units of the latest full run counted; `[]` when there are none |
| `test_cost` | the latest full run counted against the budget: `budget_seconds`, `exceeded`, `grew_by` (when a growth was measured) and `finding`, true when it exceeded the budget or `grew_by` is more than `0.5`; absent when no full run is counted. A run logged before budgets were recorded is measured against today's budget |

With no run to count (no run log, no run of this package in the range, or only
lines that cannot be parsed, which are skipped) every count is `0`, the two
durations are absent and `slowest` is `[]`. After the full gate at the
baseline, a reused full gate on the same tree and one failing task gate:

```json
"gate_report": {
  "since": "4388b9495c00bde4933113a81f32c2aeb7e5ea1d",
  "full": { "runs": 2, "seconds": 0.285, "reused": 1 },
  "task": { "runs": 1, "seconds": 0.007, "reused": 0 },
  "baseline_full_seconds": 0.285,
  "last_full_seconds": 0.285,
  "slowest": [
    { "unit": "github.com/acme/search", "seconds": 1.208 },
    { "unit": "github.com/acme/index", "seconds": 0.64 }
  ]
}
```

The `summary` is `speca: <directory> gate report: <n> full, <m> task, <k> reused`
(`speca: 16_indexed_code_search gate report: 2 full, 1 task, 1 reused` above);
`next` is empty. A `--since` that names no commit, no `--since` with neither
`refs/remotes/origin/HEAD` nor a branch `main`, and a `--dir` outside a git work
tree are usage errors with no result; a package or a run log that cannot be
read fails the call (exit 1, stage `load`), and so does a `git rev-list` that
fails (stage `git`, category `internal`).

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
      { "id": 3, "title": "Verify every execution path end to end", "kind": "integration", "state": "pending",
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
  "summary": ".specs/09_agent_mode: the task state machine (format v3 §8.1) does not allow this move, so no task was changed: task 1 is in_progress and cannot move to in_progress (it is already there; it may move",
  "error": {
    "stage": "task", "category": "lifecycle", "retryable": false, "resumable": false,
    "message": ".specs/09_agent_mode: the task state machine (format v3 §8.1) does not allow this move, so no task was changed: task 1 is in_progress and cannot move to in_progress (it is already there; it may move to pending or done)",
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
| `--validate` | `speca: <directory> validates`, or `speca: <directory> does not validate (<n> errors: <rules>)`; `1 error` for one; then `; <n> authoring hints` when there are any, `1 authoring hint` for one |
| `--list` | `speca: <n> packages` (`1 package` for one), then, when there is anything to count, ` (` the non-zero counts per status in lifecycle order, then `<n> unreadable`, `)` |
| `--next` | `speca: next is <spec_dir>`, then `; <n> more waiting` when `waiting` is not empty; `speca: every package is done; nothing is left to implement`; or `speca: no package can start; <n> waiting` |
| status form | `speca: activated <name>`, `speca: sealed <name>`, `speca: archived <name>, moved to <path>`, `speca: superseded <name> by <spec_id>, moved to <path>`, and `speca: moved <name> to <path>` for a call that only moves the package; `<name>` is the package's directory name, `<path>` where it went, as `spec_dir` shows it. An archive or a supersede that needs no move has no `, moved to` clause. Under `--dry-run` the sentence ends ` (dry run; nothing written)` (the task form's has a comma). After a move under `archive/` that failed, it says the status was written and the move was not. A refusal has no result, so its `summary` is the refusal's message |
| render form | `speca: <n> written, <n> unchanged, <n> failed`, for one package and for the sweep; ` (dry run, nothing written)` is added under `--dry-run` |
| migrate form | the same shape: `speca: <n> written, <n> unchanged, <n> failed`, for one package and for the sweep, with the dry-run clause |
| gate form | `speca: <directory> <level> gate <verdict> in <n>s`; `speca: <directory> <level> gate pass, reused from <short commit>`; `speca: <directory> <level> gate not_run: <reason>` |
| gate report | `speca: <directory> gate report: <n> full, <m> task, <k> reused` |
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
- after `--next` with a `next_spec`: `speca <spec_dir>`, the report on it, which
  names the task to start with;
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
- after `--validate`, `--render` or `--migrate`, after a refusal or a failure
  other than the one just described, and after a report or a list with nothing
  to name: nothing.

### `error`

| `stage` | `category` | When |
|---|---|---|
| `usage` | `usage` | more than one input; an input with `--list` or `--next`; `--validate` with `--list`; `--next` with any other operation; `--all` without `--list`; `-`, a URL or a multi-line input; a `--dir` that is not a directory; a reference that names no package or two; an undefined flag. For the task form: `--task` without `--state` or the reverse; a `--state` outside the four (`fix_hint` is `{flag: "--state", valid: [the four]}`); a `--task` value that is not an id, `next` or `all`, or `next`/`all` beside anything else; the task form with `--validate` or `--list`; `--force` or `--dry-run` without `--task` or `--status`; and, once the package is loaded, an id it does not have (`fix_hint` is `{flag: "--task", valid: [every id]}`, none when the plan has no tasks), `next` with no next task (`fix_hint` lists the pending ids when they are only blocked), `all` on a plan with no tasks, and a package in a format version other than 3 (the message names `speca <package> --migrate`). For the status form: `--status draft` (`fix_hint` is `{flag: "--status", valid: ["active", "sealed", "superseded", "archived"]}`, as for any value that is not one of the four, an empty one included); `--status superseded` without `--by`; `--by` without `--status superseded`, or with another status, or empty; the status form with `--task`/`--state`, `--validate` or `--list`; a `--by` that the resolver refuses (the message begins `--by:`), that names the package itself, or whose package has the input's `spec_id`. Nothing was written, and the message is also written to stderr |
| `load` | `input` | `afspec.LoadSpec` refused the package (a missing artifact, malformed JSON or YAML), naming the directory and the library's reason; or `--list`, `--next`, `--render` or `--migrate` with no input on a spec root that cannot be read, with `fix_hint.flag` `--specs-dir`. The status form: also the `--by` package (the message names `--by`), a package in a format version other than 3 (the message names `speca <package> --migrate`), and a package that was written but cannot be read back (its `result` carries the changes) |
| `status` | `lifecycle` | the task form or the evidence form on a package that cannot take a write: sealed, superseded or archived, or active with a drifted `## Intent` or with contract files that no longer match its `contract_hash` (the message names each file); or `Save` refused it as such. No `fix_hint`; `--force` and `--dry-run` change nothing. The status form: a move the lifecycle does not allow (`fix_hint` is `{flag: "--status", valid: [the moves allowed]}`, none when there are none); an activation with no `## Intent`; sealing a package whose intent drifted (no `fix_hint`) or that has a task open (`fix_hint.flag` `--force`); a replacing package that is superseded or archived (`fix_hint.flag` `--by`); keys the schema does not allow (`fix_hint.flag` `--force`); or a lifecycle or intent error that reached the write. `--force` reaches only the ones with a `fix_hint.flag` of `--force` |
| `task` | `lifecycle` | the task state machine does not allow a named move. The message names every refused task with its state and the target, cites format v3 §8.1 and says where each may go; `fix_hint` is `{flag: "--state", valid: [the first refused task's targets]}`, none when that task is `dropped`, and the message then says `--force` with `done` or `pending` is the only way out. Nothing was changed |
| `validate` | `invalid_spec` | the task form: the write would delete keys of `progress.json` the schema does not allow, and `--force` was not given. The message lists each key; no `fix_hint`. The status form: an activation of a package that does not validate (the message names the failing rules; `fix_hint.flag` `--force`) |
| `write` | `disk` | `SaveProgress` could not write `progress.json`. Not retryable; see "The write" above. The status form: `Transition` or `Supersede` could not, with the library's message and no `result`. The render form: `spec.md` could not be written |
| `archive` | `disk` | the status form: the archive destination is taken, before anything was written (no `result`, not resumable); or the move under `archive/` failed after the status was written (**resumable**, with a `result` and a `next`) |
| `migrate` | `input` | the migrate form on one package: it could not be read as version 2 or 3, it has a gap the conversion refuses (a criterion no test verifies, a path no smoke test verifies), or it could not be written; the message is the library's reason, and nothing was written. The sweep never fails this way: such a package is an entry |
| `evidence` | `evidence` | the evidence form, or a close in a package that requires evidence: the package does not require evidence or the task is not in progress; a `covers` the task does not own or given twice; a test that broke its rule; a criterion or path with no entry, a test with no failing run, a test that does not pass at the close (`fix_hint.flag` `--force`); or the tests could not be run (no runner, a program not found, a command that cannot be split, a timeout). Every fault is named; nothing was written |
| `graph` | `input` | `--next`: the dependencies of the packages under the spec root form a cycle, which the message names; no `result` |
| `schema` | `internal` | the `--schema` document could not be encoded |

`result` is absent from the usage, `load`, `graph` and `schema` errors. The task form's
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
| `0` | `done` | reported — including a package that does not validate, and `--next` with no package to name, whether all are done or none can start; or the tasks were moved, or none needed to be; or the status was changed, or the package only moved; or `spec.md` was rendered, or was current already; or the package was migrated, or was version 3 already; or the gate ran or was reused, whatever its verdict (`pass`, `fail` or `not_run`); or the gate report summed the runs; or the evidence of a task was recorded; or `--verify-evidence` answered, whatever its verdict |
| `1` | `failed` | the package could not be loaded, or the spec root `--list` reads could not be read; or the task form was refused after the package was read (stages `status`, `task`, `validate`), or its write failed; or the status form was refused (stages `status`, `validate`, `archive`) or failed (stages `write`, `archive`, `load`); or the render form's package could not be loaded (stage `load`) or its `spec.md` written (stage `write`); or the migrate form's package could not be read, converted or written (stage `migrate`); or the spec root a sweep or `--next` reads could not be read (stage `load`); or the packages `--next` reads depend on one another in a cycle (stage `graph`); or the package the gate form or the gate report reads could not be loaded (stage `load`), or the gate report's run log could not be read (stage `load`), or the commits since `--since` could not be listed (stage `git`); or the evidence form, or a close in a package that requires evidence, was refused (stage `evidence`, or `status` for a changed contract) |
| `2` | `usage` | usage error — nothing was written. Among them: a `--gate` other than `task` or `full`, a `--timeout` that is not a duration greater than zero, a `--base` that names no commit or comes without `--gate task`, a package with `affected_tests` gated at the task level outside a git work tree, a `--since` that names no commit, no `--since` and no base branch, a `--gate-report` outside a git work tree, an `--evidence` without one task id or whose file is not an evidence file, `--verify-evidence` with another form, and a `--timeout` with a form that runs no tests |

### Warning codes

| Code | Severity | Stage | When |
|---|---|---|---|
| `specs_dir_unreadable` | low | preflight | the spec root (or, for `--list --all` and `--next`, `archive/`) could not be read. The report goes without `dependents`, the verdict without the cross-spec rules, the list without the archived packages, and `--next` takes no dependency to be finished for being under `archive/` |
| `steering_malformed` | high | preflight | `--validate` only: the spec root's `steering.md` opens with frontmatter that breaks one of its rules ([configuration.md](configuration.md#the-steering)), each named in the message. The verdict is the one an unversioned steering gives |
| `spec_invalid` | high | validate | the package is in a format version other than 3: its progress and tasks are not read, and the warning names `speca <package> --migrate`. For the task form: `--force` writes over keys of `progress.json` the schema does not allow, which the warning names |
| `spec_invalid` | low | validate | the task form went ahead on a package that does not validate, which the warning names by rule. A call raises at most one `spec_invalid` |
| `spec_invalid` | high | validate | the status form: `--force` activated a package that does not validate (naming the rules, and the keys the write drops), or wrote a package that holds keys the schema does not allow (naming them) |
| `tasks_open` | high | status | `--force` sealed a package with a task `pending` or `in_progress`. The warning lists each task with its state |
| `supersedes_not_recorded` | low | status | superseding went through, but the old `spec_id` was not added to the replacing package's `supersedes`: it is sealed, its intent drifted, it holds keys the schema does not allow, or its save failed. The warning names the replacing package's directory, the id and the reason |
| `task_not_ready` | low | task | a task moved to `in_progress` has a dependency that is not `done`, judged on the plan before the call. The warning names the task and the dependencies |
| `task_state_forced` | high | task | `--force` set a state the machine would have refused. One for each such task, naming it and its two states |
| `evidence_command_missing` | low | validate | `--validate` only: a draft, or a package that requires evidence, names no `evidence_tests` and `--dir` holds no `go.mod`, so nothing could run its tests. `valid` is as it would be without it |
| `evidence_log_not_written` | low | evidence | the evidence form, a close on evidence or the verify form ran the tests, but the state directory could not be written, so no log was kept |
| `evidence_no_red` | low | task | a task closed on its evidence with an exempt entry: one per entry, naming the task, the criterion and the reason |
| `task_evidence_forced` | high | task | `--state done --force` closed a task of a package that requires evidence without the checks of its evidence. One for each such task |
| `gate_not_recorded` | low | gate | the gate ran and answered, but its result is not recorded for reuse: there is no tree id (`--dir` is not in a git work tree, or git is not installed), or the state directory cannot be written (the message names the path, or says that neither `$XDG_STATE_HOME` nor the home directory is set; the commands ran without a `log`), or the pass or the run could not be written |

The warnings of the task and status forms are raised when the call goes ahead: a
refusal, or a write that fails, raises none, since they would speak of changes
that were not made. The shell adds `; <n> high-severity warning(s)` to the
`summary` of a success that raised a high warning.

## Flags

| Flag | Default | Effect |
|---|---|---|
| `--validate` | off | give the format's full verdict on the package |
| `--strict-handoffs` | off | with `--validate`: an inbound handoff no task takes is an error, not a warning |
| `--list` | off | list every package under the spec root; takes no input |
| `--next` | off | name the package to implement now, in dependency order; takes no input |
| `--all` | off | with `--list`, also list the packages under `archive/`, marked archived; with `--render` or `--migrate` and no input, also render or convert them |
| `--task` | | the task form: the tasks to move, once or repeated, each value a comma-separated list of ids, `next` or `all` |
| `--state` | | the task form: the state to move them to: `pending`, `in_progress`, `done` or `dropped` |
| `--status` | | the status form: the lifecycle status to move the package to: `active`, `sealed`, `superseded` or `archived`. An archived or superseded package is moved under `archive/` |
| `--by` | | with `--status superseded`: the package that replaces this one, named as the input is |
| `--force` | off | with the task form: set `done` or `pending` where the machine refuses it, write over keys the schema does not allow, and, in a package that requires evidence, close a task without the checks of its evidence. With the status form: skip the checks that an activation validates, that no task is open when sealing, and that no key the schema does not allow would be dropped |
| `--dry-run` | off | with the task form, the evidence form, the status form, the render form or the migrate form: run every check (and the tests the call runs) and report the result, write nothing |
| `--render` | off | the render form: write the package's `spec.md`, its render, and no other file |
| `--migrate` | off | the migrate form: convert a version 2 package to version 3 in place, whatever its status, by format v3 §13; a version 3 package is left alone |
| `--gate` | | the gate form: `task` or `full`, the level of the gate to run |
| `--clean-env` | off | with `--gate`: run the commands with an empty temporary `HOME`, no global or system git configuration and `GIT_TERMINAL_PROMPT=0` |
| `--timeout` | `20m` | with `--gate`: how long one command may run, a Go duration greater than zero; with `--evidence`, `--verify-evidence` and `--state done`: how long the tests may run |
| `--evidence` | | the evidence form, with `--task` and one task id: the evidence file to record |
| `--verify-evidence` | off | the verify form: run every recorded test of every done task again |
| `--base` | `HEAD` | with `--gate task`: the commit a package's `affected_tests` runs from, in place of `{base}` |
| `--gate-report` | off | the gate report: sum the package's gate runs since a commit |
| `--since` | the merge base of `HEAD` and the base branch | with `--gate-report`: the commit the runs are counted from |
| `--specs-dir` | `<dir>/.specs`, or `$AF_SPEC_DIR` | where `NN_name` packages live |
| `--dir` | `.` | the repository the spec root and a relative reference are resolved against |
| `--schema` | | print the self-description below and exit 0 |
| `--version` | | print the build identity and exit 0 |

Flags may come before or after the input. `$AF_SPEC_DIR` is the environment
variable every form reads; the gate form and the gate report also read
`$XDG_STATE_HOME` (else `$HOME`) for the state directory, and the gate's
commands inherit the environment. `speca` reads no credential and no model
variable.

## Self-description (`--schema`)

`speca --schema` prints one JSON object describing the interface and exits 0.
It reads nothing — no package, no `--dir` — so it runs anywhere:

- `tool`, `description` — the command and what it does, with the sentence
  naming `spec`, `specr` and `speca`;
- `input` — what the one positional input may be, with examples;
- `flags` — a standalone JSON Schema (2020-12) document of the flags, each
  with its `type`, `default` and `description`, generated from the flag set;
  `--state` is an `enum` of the four states, `--status` of the four targets and
  `--gate` of the two levels.
  `--schema` and `--version` are not listed;
- `result` — a standalone JSON Schema document of the envelope, with `result`
  replaced by `speca`'s own result type and every field described, generated
  from the Go types; the envelope fields `speca` never writes (`input`,
  `model`, `usage`, `side_effects`) are left out;
- `exit_codes` — the three codes and what each means.

The document is checked in at `cmd/speca/testdata/schema.golden.json`, and a
test fails when a flag or a result field changes without it;
`UPDATE_GOLDEN=1 go test -run '^TestSchema_MatchesTheGoldenFile$' ./cmd/speca`
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
renders that task. `speca --list` shows every package at once, and
`speca --next` names the package to implement next.
```

The same skill's "one task at a time" step claims the task before it renders it
and records it done after:

```markdown
**One task at a time.** Claim the task before you start it:
`speca <package> --task <N> --state in_progress`. A refusal at stage `task`
means another agent holds it; pick another from `result.tasks`. Then work
from the spec scoped to it: run `specr <package> --task <N>` and read
`result.markdown` — the requirements that own the task's criteria, with their
checks, and the paths those checks name in full, everything else by id. When
the checks of its criteria pass as tests, record it:
`speca <package> --task <N> --state done`, and follow `next` to the task
after it. If you stop before it is done, release it with `--state pending`.
Never edit `progress.json` by hand: if `speca` or `specr` is not installed,
read the three artefacts and scope them by hand, and say in your report
which task you finished instead of changing its state.
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
task after it; after the last task of the plan comes the integration task,
`N+1`, claimed and recorded the same way. `--dry-run` shows the effect of a
call before it is made. Two
agents that claim the same task get the machine's answer: the second call is
refused, with the states the task may move to and the package as it is on disk.
The same loop in Go is [`examples/tasks`](../examples/tasks/main.go).

The gate is a call of its own. `speca <package> --gate task` after each
implement task and `--gate full` at the baseline, after the integration task,
after the conformance fixes (`--gate full --clean-env` for the clean-environment
run) and on the merged tree: `result.gate.verdict` is the answer, a failure is
read from `tail`, and a tree that already passed is not checked again.
`speca <package> --gate-report --since <baseline commit>` then gives the
numbers for the merge commit. The verdict is not compared with a baseline:
that, and what to do about a failure, are the caller's.

The lifecycle has the same shape. A draft that validates is told by `next` to
`speca <package> --status active`, whose `next` is the claim of its first task;
an active package whose tasks are all finished is told to
`speca <package> --status sealed`. A call refused for a reason that `--force`
overrides says so in its `fix_hint`; one that is refused for a rule of the format
does not. When a move under `archive/` fails after the status was written, the
envelope is `resumable` and its `next` is the call to make again, which moves the
package and changes nothing else.

## What it leaves out

`speca` is a fast call (the gate form takes as long as the package's commands),
so the parts of `spec`'s shell that serve a long model run are absent: there is no model, no `--preflight`, no `--detail`, no `--output`
and no progress on stderr. (`--dry-run` here is that of the task, status, render and migrate forms, not `spec`'s.)
It changes nothing but the state of the tasks of a package, with the
`updated_at` of its `progress.json`, its lifecycle status (with the
`intent_hash` an activation records and the banner a superseded package is
given) and where it lives, the `supersedes` of a replacing package, its
`spec.md`, which every save writes and the render form writes alone, and the
format version of a version 2 package, which the migrate form converts without
changing what it says; and outside the repository, the gate's logs, passes and
run log in the state directory, which it never prunes: not a package's content (requirements, checks, a task's
title, steps or dependencies), and it creates no package and renders none to
stdout, which are `spec`'s and `specr`'s. It does not move a package back to
`draft`, reopen a sealed one, or take one out of `archive/`; it does not
re-hash a changed `## Intent`, edit `profile` or `source`, or remove an entry
from `supersedes`; it does not copy and delete where a rename crosses
filesystems; and it does not check that the replacing package covers what the
old one did. (A lifecycle move writes every artifact again, as the status
form's section says, but with the content it had; a task move writes
`progress.json` alone.) It takes no lock:
two writers of one package at once get the last writer's files, as they do with
the library, and the state machine is what a second claim, made after the first
was written, meets. The decision to build it is
[ADR 10](adr/10-report-and-change-the-state-of-a-package-with-its-own-command.md).
