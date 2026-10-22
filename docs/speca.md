# The `speca` command

```
speca [flags] <spec>    a specification package → its state
speca --list [--all]    every package under the spec root → one entry each
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

`spec` writes a package from an idea, `specr` renders one, `speca` reports its
state.

It reads the packages and changes nothing: it writes no file, makes no model
call and no network call, and reads no credential. Changing a package's state —
a task's, or the package's own lifecycle status — is not a form of this
command yet; it is library API in [`afspec`](../afspec/README.md).

```sh
speca 16                                # the state of package 16
speca .specs/16_indexed_code_search     # the same, by path
speca 16 --validate                     # the format's full verdict on it
speca --list                            # every package under the spec root
speca --list --all                      # and those under archive/
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

A call names one operation. `--validate` with `--list`, a positional input with
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
| `validation` | `valid`, `error_count`, `warning_count`, and `errors` and `warnings`, each finding with its `check` (the rule), `artifact`, `entity` and `message`. The package's own findings come first, then the cross-spec findings that concern it |
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
  "next": [ { "tool": "specr", "input": ".specs/09_agent_mode", "flags": ["--task", "2"],
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

`ok`, `status`, `exit_code`, `summary`, `error` and `warnings` mean what they
mean for `spec` (see [cli.md](cli.md#the-output)). The envelope fields `speca`
never writes (`input`, `model`, `usage`, `side_effects`) are absent.

### `summary`

One sentence per form, which the shell caps at 200 characters:

| Form | Shape |
|---|---|
| report | `speca: <directory> <status>, <done> of <total> tasks done`, then `; next task N` when there is one, then `; does not validate (<rules>)` when `valid` is `false` |
| `--validate` | `speca: <directory> validates`, or `speca: <directory> does not validate (<n> errors: <rules>)` |
| `--list` | `speca: <n> packages (` the non-zero counts per status in lifecycle order, then `<n> unreadable`, `)` |

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
  says whether the task is in progress or is the next ready task;
- after `--list`: the same for the first active package, in list order and not
  under `archive/`, that has no task in progress and has a ready task N;
- after `--validate`, and after a report or a list with nothing to name:
  nothing.

### `error`

| `stage` | `category` | When |
|---|---|---|
| `usage` | `usage` | more than one input; an input with `--list`; `--validate` with `--list`; `--all` without `--list`; `-`, a URL or a multi-line input; a `--dir` that is not a directory; a reference that names no package or two; an undefined flag. Nothing was read, and the message is also written to stderr |
| `load` | `input` | `afspec.LoadSpec` refused the package (a missing artifact, malformed JSON or YAML), naming the directory and the library's reason; or `--list` on a spec root that cannot be read, with `fix_hint.flag` `--specs-dir` |
| `schema` | `internal` | the `--schema` document could not be encoded |

`result` is absent from each. `retryable` and `resumable` are always false.

### Exit codes

| Code | `status` | Meaning |
|---|---|---|
| `0` | `done` | reported — including a package that does not validate |
| `1` | `failed` | the package could not be loaded, or the spec root `--list` reads could not be read |
| `2` | `usage` | usage error — nothing was read |

### Warning codes

| Code | Severity | Stage | When |
|---|---|---|---|
| `specs_dir_unreadable` | low | preflight | the spec root (or, for `--list --all`, `archive/`) could not be read. The report goes without `dependents`, the verdict without the cross-spec rules, the list without the archived packages |
| `spec_invalid` | high | validate | the package is in a format version other than 2: its tests and tasks are not read |

## Flags

| Flag | Default | Effect |
|---|---|---|
| `--validate` | off | give the format's full verdict on the package |
| `--list` | off | list every package under the spec root; takes no input |
| `--all` | off | with `--list`, also list the packages under `archive/`, marked archived |
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
  with its `type`, `default` and `description`, generated from the flag set.
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

Reading the state of a package is the first half of working through it; the
second, recording a task as claimed or done, is not a form of `speca` yet. An
agent with a shell uses the library for that (see
[`examples/tasks`](../examples/tasks/main.go)).

## What it leaves out

`speca` is a fast, read-only call, so the parts of `spec`'s shell that serve a
long model run are absent: there is no model, no `--preflight`, no
`--dry-run`, no `--detail`, no `--output` and no progress on stderr. It does
not change a package, create one or render one: the first is the library's, the
second is `spec`'s, the third is `specr`'s. The decision to build it is
[ADR 10](adr/10-report-and-change-the-state-of-a-package-with-its-own-command.md).
