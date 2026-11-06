# Reading `speca` and `specr`

The two commands of the `spec` repository that do a package's bookkeeping.
Each writes exactly one JSON object to stdout, reads the spec root, and —
except in the forms that change a task's state or the package's status, or
write its `spec.md` — writes nothing. Read their JSON; do not work out a package's state, scope or
verdict by reading its four files by hand while the commands are there to ask.

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
  `status`, `created_at`, `updated_at`, `valid` with `failing_rules`,
  `intent_drift`, `depends_on`, the `tasks` list with each task's `state`,
  `depends_on` and `ready`, `task_counts`, and `next_task`, the task to work
  on now: the one in progress, else the first ready one.
- `speca <package> --validate` — the format's whole verdict:
  `result.validation.errors`, each naming its `check`, `artifact` and
  `entity`, `result.validation.warnings` in the same shape, and
  `result.traceability` with the criteria and paths covered. `result.valid`
  is the answer.
- `speca --list [--all]` — every package under the spec root in number order,
  with its status, verdict, task counts and next task. `--all` includes the
  archived ones.
- `speca <package> --task N --state S [--dry-run]` — moves one task through
  the state machine. `result.changes` lists what changed.
- `speca <package> --status S` — moves the package through its lifecycle.
- `speca <package> --render [--dry-run]` — writes `spec.md`, the package's
  render, and no other file; `speca --render [--all]` does it for every
  package. `result.rendered` says `written`, `unchanged` or `failed`.
- The envelope's `next` suggests the calls that usually follow.

## `specr`

- `specr <package>` — renders the package as Markdown: the PRD, the
  architecture where there is one, the requirements with each criterion as its
  EARS sentence and its contract, the tests under what they verify, and the
  tasks with what each owns.
- `specr <package> --task N` — the same, scoped to one task: the requirements
  that own its criteria, its tests, the paths those verify, and its `steps`,
  `touches` and `done_when`. Every other requirement, test and task appears by
  id and title, as context.
- `specr <package> --section prd|requirements|tests|tasks` — one part of it.
- `result.valid` is the format's validation; exit 0 does not imply it, and an
  invalid package still renders with a `spec_invalid` warning naming the rules.
  `result.tasks` lists every task with its `state` and `depends_on`, and in the
  scoped form `result.task` is the one task it was rendered for.
- Do not pass `--max-tokens`: it drops the architecture and each test's
  `given`, `when` and `pseudocode`.

## Writing their output to files

Send the Markdown and the envelope to files **outside the repository**, so the
working tree stays clean, and send stdout to a file too, because the envelope
repeats the whole Markdown:

```sh
specr <package> --markdown-file "${TMPDIR:-/tmp}/<name>.md" > "${TMPDIR:-/tmp}/<name>.json"
```

## When they are not installed

Neither command is required for the method, only for its bookkeeping. Without
them: read the four artifacts of the package directly, work out the package's
scope, each task's readiness and the cross-file rules by hand, validate against
the JSON Schemas in `specification/schemas/*.v2.json` of a `spec` checkout with
any JSON Schema validator, and change a task's `state` value in `tasks.json` by
hand and nothing else. Say in the report that you did, and that no verdict from
the format itself was taken.

## What a `speca` write does to the package

The task form and the status form write through the library's `Spec.Save`,
which rewrites **every** artifact of the package, writes `spec.md`, its render,
beside them, and sets `updated_at` in the frontmatter of `prd.md`. So after a
claim and a `done`, `git diff -- <package>` should hold the task's `state`,
`updated_at` and the same change in `spec.md`, and nothing else. A package
`spec` itself wrote is already in the library's encoding, so that is the whole
diff; if other lines changed, the package was not in that encoding, and when
the content is the same it can be committed with the first task and named in
the report. A change of **content** is a blocker.

A refusal names its stage: `task` means the task's state is not what the report
said, `status` that the package can no longer take a write (sealed, superseded,
archived or drifted), `validate` that a write would drop keys the schema does
not allow. Never get past one with `--force`.

## What `--validate` checks

`speca <package> --validate` gives the format's whole verdict in one call: the
four JSON Schemas, the cross-file rules C1 to C11, and the rules across the
whole spec root. Work in that order, because the stages gate each other — the
cross-file rules do not run while any file fails its schema, and the
cross-spec rules are checked by nothing else.

The cross-file rules are: `spec_id` and `spec_name` agree across the four files
and the directory name; every `verifies`, `criteria`, `tests` and `depends_on`
entry resolves; every criterion has a test and every test has a task; every
criterion is covered by an `implement` task; the last task is the one
`integration` task and owns every `smoke` test; and every `unwanted` criterion
has a `contract`.

Rule `rendered` compares `spec.md` with the package's render: missing is a
warning; different is an error for a draft or active package and a warning
for a sealed one. `speca <package> --render` fixes it.

The rules across packages are: a dependency on a package that does not exist,
a dependency cycle, a dependency whose execution paths share no actor with this
package's, and a glossary term two packages define differently.

A rule that fails is a defect in the spec, and the spec is not the
implementer's to edit.
