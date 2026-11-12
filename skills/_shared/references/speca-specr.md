# Reading `speca` and `specr`

The two commands of the `spec` repository that do a package's bookkeeping.
Each writes exactly one JSON object to stdout, reads the spec root, and —
except in the forms that change a task's state or the package's status, write
its `spec.md`, or migrate it — writes nothing. Read their JSON; do not work
out a package's state, scope or verdict by reading its files by hand while the
commands are there to ask.

## Installing them

`make build` in a checkout of `github.com/agent-fox-dev/spec` installs `spec`,
`specr` and `speca` together.

## Exit codes

Both commands use the same three:

| Code | Meaning |
|---|---|
| 0 | The command ran. A package that does **not** validate still exits 0: the verdict is in the JSON, not in the code. |
| 1 | The package could not be read at all — a missing artifact, malformed JSON or YAML. `error.message` says which. |
| 2 | The reference names no package, or names two. The output lists the packages the spec root holds. |

## `speca`

- `speca <package>` — the state: `spec_dir`, `spec_id`, `spec_name`, `title`,
  `status`, `profile`, `created_at`, `updated_at` (when a task last changed
  state, from `progress.json`), `valid` with `failing_rules`, `intent_drift`,
  `depends_on`, the `tasks` list with each task's `kind` (`implement`, or
  `integration` for the derived task N+1 of a `full` package), `state`,
  `depends_on` and `ready`, `task_counts`, and `next_task`, the task to work
  on now: the one in progress, else the first ready one.
- `speca <package> --validate` — the format's whole verdict:
  `result.validation.errors`, each naming its `check`, `artifact` and
  `entity`, `result.validation.warnings` in the same shape, and
  `result.traceability` with the criteria and paths covered and the criteria
  no task owns. `result.valid` is the answer. Beside the verdict,
  `result.hints` lists the authoring hints in the same shape:
  `vague_language`, `line_reference` and `foreign_test_command`. They never
  change `valid`.
- `speca --list [--all]` — every package under the spec root in number order,
  with its status, verdict, task counts and next task. `--all` includes the
  archived ones.
- `speca --next` — the package to implement now: `result.next_spec`, the
  first open package (draft or active, with a task pending or in progress) in
  the order of the cross-spec `dependencies`, ties broken by number, whose
  every dependency is finished (sealed, under `archive/`, or with no task
  open). `result.all_done` is true when nothing is left to implement;
  otherwise, with no `next_spec`, every open package waits, and
  `result.waiting` gives each with its `waits_on` and `reason`. A dependency
  cycle fails the call at stage `graph`, exit `1`, naming the cycle.
- `speca <package> --task N --state S [--dry-run]` — moves one task through
  the state machine, the integration task included. `result.changes` lists
  what changed.
- `speca <package> --status S` — moves the package through its lifecycle.
- `speca <package> --render [--dry-run]` — writes `spec.md`, the package's
  render, and no other file; `speca --render [--all]` does it for every
  package. `result.rendered` says `written`, `unchanged` or `failed`.
- `speca <package> --migrate [--dry-run]` — converts a version 2 package to
  version 3 in place; `speca --migrate [--all]` does it for every package.
  A package in another format version is reported with one `schema_version`
  error naming this call, and nothing else of it is read.
- `speca <package> --gate task|full [--clean-env] [--timeout <d>]` — runs
  the package's `linter`, then its `all_tests`, with no shell, and answers in
  `result.gate`: `verdict` (`pass`, `fail` or `not_run`, with `reason`), each
  command that ran with its `exit_code`, `seconds`, `log` and, when it failed,
  `tail` (its last 40 lines), and for a full gate `slowest`. A tree that
  already passed the same gate is answered from the record, with `reused`
  (`commit`, `at`, `seconds`), and runs nothing. Exit code 0 carries every
  verdict. It writes nothing in the repository; a `gate_not_recorded` warning
  says the result was not recorded for reuse.
- `speca <package> --gate-report [--since <commit>]` — sums the package's
  gates since the commit in `result.gate_report`: `full` and `task` (`runs`,
  `seconds`, `reused`), `baseline_full_seconds`, `last_full_seconds` and
  `slowest`. It writes nothing.
- The envelope's `next` suggests the calls that usually follow; it is empty
  after the gate and the report.

## `specr`

- `specr <package>` — renders the package as Markdown: the PRD, the
  architecture where there is one, the requirements with each criterion as its
  EARS sentence, its contract and its check, each path with its steps and its
  smoke check, and the tasks with what each owns, the derived integration task
  last. No task state is rendered.
- `specr <package> --task N` — the same, scoped to one task: the requirements
  that own its criteria, the paths a `same_as` of those criteria names, and
  its `steps`, `touches` and `done_when`. Every other requirement, path and
  task appears by id and title, as context. For the integration task, N+1,
  it is every path with its smoke check and a `## Contracts` section listing
  every criterion that carries a contract.
- `specr <package> --section prd|architecture|requirements|tasks` — one part
  of it.
- `result.valid` is the format's validation; exit 0 does not imply it, and an
  invalid package still renders with a `spec_invalid` warning naming the rules.
  `result.tasks` lists every task, the integration task included, with its
  `kind`, `state` and `depends_on`, and in the scoped form `result.task` is
  the one task it was rendered for.
- Do not pass `--max-tokens`: it drops the architecture and then the `setup`
  and `pseudocode` of every check and smoke check (`check_detail`).

## Writing their output to files

Send the Markdown and the envelope to files **outside the repository**, so the
working tree stays clean, and send stdout to a file too, because the envelope
repeats the whole Markdown:

```sh
specr <package> --markdown-file "${TMPDIR:-/tmp}/<name>.md" > "${TMPDIR:-/tmp}/<name>.json"
```

## When they are not installed

Neither command is required for the method, only for its bookkeeping. Without
them: read the three artifacts of the package directly, work out the package's
scope, each task's readiness and the cross-file rules by hand, validate against
the JSON Schemas in `specification/schemas/*.v3.json` of a `spec` checkout with
any JSON Schema validator, and change a task's `state` entry in `progress.json`
by hand and nothing else. Run `linter` and `all_tests` by hand, in that order,
at the moments the gate would run, the clean-environment run with an empty
`HOME`, no global git configuration and `GIT_TERMINAL_PROMPT=0`; there is no
reuse and no gate report. Say in the report that you did, and that no verdict
from the format itself was taken.

## What a `speca` write does to the package

The task form writes `progress.json` through the library's `Spec.SaveProgress`
— the task states and the file's `updated_at` — and **no other file**. So
after a claim and a `done`, `git status -- <package>` and
`git diff -- <package>` should show `progress.json`, new on a package's first
move, and nothing else. Any other file changed is a blocker.

The status form writes through the library's `Spec.Save`, which rewrites
**every** artifact of the package in its canonical encoding, `progress.json`
with it, and writes `spec.md`, its render, beside them. The contract files
change by a lifecycle move and by nothing else after activation.

A refusal names its stage: `task` means the task's state is not what the report
said, `status` that the package can no longer take a write (sealed, superseded,
archived or drifted), `validate` that a write would drop keys the schema does
not allow. Never get past one with `--force`.

## What `--validate` checks

`speca <package> --validate` gives the format's whole verdict in one call: the
three JSON Schemas (and `progress.json`'s, when the file exists), the cross-file
rules C1 to C8, and the rules across the whole spec root. Work in that order,
because the stages gate each other — the cross-file rules do not run while any
file fails its schema, and the cross-spec rules are checked by nothing else.

The cross-file rules are: `spec_id` and `spec_name` agree across the files and
the directory name (C1); every id matches its format and is unique (C2); every
`same_as` names a criterion whose check is its own, or a path (C3); every
`criteria` and `depends_on` entry resolves (C4); every criterion is owned by a
task (C5); every `unwanted` criterion has a `contract` (C6); a `full` package
has a path and a `lite` one none and at most three requirements (C7); every
entry of `progress.json` names a task once (C8).

Rule `rendered` compares `spec.md` with the package's render: missing is a
warning; different is an error for a draft or active package and a warning
for a sealed one. `speca <package> --render` fixes it.

The rules across packages are: a dependency on a package that does not exist,
a dependency cycle, and a dependency whose execution paths share no actor with
this package's.

A rule that fails is a defect in the spec, and the spec is not the
implementer's to edit.
