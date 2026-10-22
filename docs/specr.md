# The `specr` command

```
specr [flags] <spec>    a specification package → the package as Markdown
```

`specr` renders a version 2 package with `afspec`'s render functions: the
whole package, or the package scoped to one task. It is built for an agent
to call — from a prompt, or from a skill's instructions — and follows the
same interface as `spec` and agent-fox's tools: **exactly one positional
input**, **exactly one JSON object** on stdout on every program-driven path,
the help on stderr, and one exit-code table. Different forms of the render
are flags, not subcommands.

It reads the package and changes nothing. The only file it can write is the
one `--markdown-file` names.

`spec` writes a package from an idea, `specr` renders one, `speca` reports and
changes its state. Each command's help and `specr --schema` say so, in the sentence
`cli.SiblingCommands`; and `specr`'s `next` names the `speca` call that records a
task as claimed or done, where it would succeed (see [`artifacts` and
`next`](#artifacts-and-next)). `specr` itself never writes a package: recording
progress is `speca`'s.

```sh
specr .specs/16_indexed_code_search              # the whole package
specr 16 --task 3                                # scoped to task 3
specr 16 --section tasks                         # the plan and its states only
specr 16 --task 3 --max-tokens 8000              # dropping detail to fit a budget
specr 16 --task 3 --markdown-file /tmp/task-3.md # also as a file
specr --schema                                   # the interface, as JSON Schema
```

Two paths are for a person and print text instead: `--version` prints the
build identity (the version, then the commit and build time when the build
set them, from the variables in `version.go`), and `-h`/`--help` or a bare invocation prints the help to
stderr. `--help` exits 0 and writes nothing to stdout. A bare invocation exits
2; when stdout is not a terminal it also writes a usage envelope, so a program
still gets one object to parse.

## The input

The argument is a reference to one package, not a document. It is tried in
this order:

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
render when given by path. A reference that matches two packages is refused
naming both. `-`, a URL and a multi-line argument are usage errors: `specr`
reads a package from disk.

## The forms

| Flags | What `result.markdown` holds |
|---|---|
| none | the whole package: `afspec.Spec.RenderCombined`, byte for byte — `# PRD` (with the frontmatter as a list), `# Architecture` when there is one, `# Requirements`, `# Test Specification`, `# Tasks` |
| `--task N` | the package scoped to task N: `afspec.Spec.RenderIndividualScoped`, each section under the same headings, after a one-line note naming the task. The requirements that own the task's criteria, the task's tests and the execution paths they verify are in full; every other requirement, test and task is one line, by id and title. The PRD, the architecture, the glossary, the external APIs and the test commands are unfiltered |
| `--section a,b` | only the named sections — `prd`, `architecture`, `requirements`, `test_spec`, `tasks` — in that order, whichever order they were given in; with or without `--task` |
| `--max-tokens N` | the library's budget: when the estimated tokens (bytes / 4) are over N, first the architecture section is dropped, then each test's `given`, `when`, `real_components` and `pseudocode`. What was dropped is `result.reduced`; a render still over N after both carries an `over_budget` warning. Cannot be combined with `--section` |

`--task` names an id from `result.tasks`. An id the plan does not have is a
usage error whose `fix_hint.valid` lists the ids, or that says the plan has no
tasks.

`specr` reads format version 2 only. A package whose PRD frontmatter declares
another `schema_version` — such as the version 1 packages under
`.specs/archive/`, whose plan is `task_groups` — still loads, but none of its
tests or tasks are read. Rendered whole, it carries a high-severity
`spec_invalid` warning saying so; `--task` on it is a usage error naming the
version, since it has no task to scope to. Format v2 §13 maps each v1
construct to its v2 form.

## The output

```jsonc
{
  "tool": "specr",
  "version": "v0.4.0",
  "ok": true,
  "status": "done",
  "exit_code": 0,
  "summary": "specr: rendered 09_agent_mode scoped to task 2 of 3 (prd, requirements, test_spec, tasks, ~1191 tokens)",
  "result": {
    "spec_dir": ".specs/09_agent_mode",
    "spec_id": "09",
    "spec_name": "agent_mode",
    "title": "Agent mode for the spec CLI",
    "status": "draft",
    "valid": true,
    "scope": "task",
    "task": { "id": 2, "title": "Error envelope and closed-pipe handling", "kind": "implement",
              "state": "pending", "depends_on": [1] },
    "sections": ["prd", "requirements", "test_spec", "tasks"],
    "tokens": 1191,
    "tasks": [ { "id": 1, "title": "Detect agent mode and keep stdout JSON-only", "kind": "implement", "state": "pending", "ready": true }, … ],
    "markdown": "> The package `09_agent_mode`, scoped to task 2: …"
  },
  "next": [ { "tool": "specr", "input": ".specs/09_agent_mode", "flags": ["--task", "3"],
              "why": "task 3 (Verify agent mode end to end) is the next task not done" } ],
  "duration_ms": 7,
  "started_at": "2026-10-14T06:23:30Z"
}
```

`ok`, `status`, `exit_code`, `summary`, `error` and `warnings` mean what they
mean for `spec` (see [cli.md](cli.md#the-output)). `<`, `>` and `&` are written
as they are, not as `<` escapes, so the Markdown reads the same in the
raw JSON.

### `result`

| Field | Meaning |
|---|---|
| `spec_dir` | the package directory, relative to `--dir` when inside it; runnable as `specr`'s input |
| `spec_id`, `spec_name`, `title`, `status` | from the PRD frontmatter |
| `valid` | whether the package passes the format's validation. An invalid package still renders in full, with a `spec_invalid` warning naming the failing rules |
| `scope` | `spec` (the whole package) or `task` |
| `task` | the task the render is scoped to: `id`, `title`, `kind`, `state`, `optional`, `depends_on`, `ready`; present exactly when `scope` is `task` |
| `sections` | the sections the Markdown holds, in order |
| `max_tokens` | the `--max-tokens` budget, when there was one |
| `reduced` | what the budget dropped: `architecture`, `test_detail`; absent when nothing was |
| `tokens` | the Markdown's estimated tokens, bytes / 4 |
| `tasks` | every task of the plan, in order, without its body: the ids `--task` takes, and their states. A pending task whose dependencies are all done carries `"ready": true` (`afspec`'s `IsReady`, the same rule [`speca`](speca.md) uses); the field is absent otherwise |
| `markdown_file` | the absolute path `--markdown-file` wrote |
| `markdown` | the render. It is the last field, so a reader meets what it is before what it says |

### Reading it from Go

`envelope.DecodeSpecr` reads the object into `envelope.RenderEnvelope`, whose
`Result` is an `*envelope.RenderResult` with the fields above; the package is
described in [api.md](api.md#envelope).

```go
env, err := envelope.DecodeSpecr(stdout)
if err != nil {
    return err
}
if !env.OK {
    return env.Error
}
md := env.Result.Markdown
```

### `artifacts` and `next`

`artifacts` holds one `{"kind": "markdown_file", "path"}` when
`--markdown-file` was written, and is absent otherwise.

`next` lists the calls to make after this render, in the order an agent makes
them. The last is the render of the next task that is not `done` or `dropped`:
after a whole render, the first such task; after `--task N`, the first one after N.
Its rule, flags and `why` are the same whatever else `next` holds. Before it, where
the call would succeed, comes a call of `speca` that records the progress:

| After | When | The `speca` entry |
|---|---|---|
| a whole render (with or without `--section` or `--max-tokens`) | the package can take a write, no task is in progress, and the first task not done is `pending` and ready | `speca <spec_dir> --task M --state in_progress`, the claim of that task (`M` is also the task the render suggestion names, and the task `speca <spec>` suggests claiming) |
| `--task N` | the package can take a write and `N` is `pending` and ready | the claim of `N`, even if another task is in progress: the caller chose `N`, and the claim is the compare-and-set that settles a race |
| `--task N` | the package can take a write and `N` is `in_progress` | `speca <spec_dir> --task N --state done` |

There is no `speca` entry for a task `done` or `dropped`, a `pending` task that is
not ready, a package that cannot take a write (sealed, superseded, archived, or
active with a drifted `## Intent`), a whole render with a task in progress, or when
no task is left. `specr` suggests no release (`--state pending`), which it cannot
know an agent wants, and no lifecycle call: those are `speca`'s own `next`. The
`speca` entry comes first, with the same `input` as the render suggestion and four
`flags`. `next` is absent when there is no task left to render and no call to make,
and when the package could not be loaded; it is there after a failed
`--markdown-file` write, as after a good one. The writability behind it is kept in a
field of `specr`'s result that is never written.

A call of `speca` is offered where `speca` would take the package: a draft, or an
active package whose intent has not drifted. A package that holds a key the schema does
not allow, or is in another format version, is offered the call all the same, as
`speca <spec>` offers its claim, and `speca` refuses it with the reason.

A whole render of the example, where no task is claimed and task 1 is ready:

```jsonc
"next": [ { "tool": "speca", "input": ".specs/09_agent_mode", "flags": ["--task", "1", "--state", "in_progress"],
            "why": "task 1 (Detect agent mode and keep stdout JSON-only) is the next ready task; claim it before working on it so that no other agent takes it" },
          { "tool": "specr", "input": ".specs/09_agent_mode", "flags": ["--task", "1"],
            "why": "task 1 (Detect agent mode and keep stdout JSON-only) is the first task not done; render the package scoped to it" } ]
```

And `specr 09 --task 1` once task 1 is in progress:

```jsonc
"next": [ { "tool": "speca", "input": ".specs/09_agent_mode", "flags": ["--task", "1", "--state", "done"],
            "why": "record task 1 (Detect agent mode and keep stdout JSON-only) done once its work is finished and its tests pass" },
          { "tool": "specr", "input": ".specs/09_agent_mode", "flags": ["--task", "2"],
            "why": "task 2 (Error envelope and closed-pipe handling) is the next task not done" } ]
```

### `error`

| `stage` | `category` | When |
|---|---|---|
| `usage` | `usage` | a flag that cannot hold, a reference that names no package or two, an unknown task, `--task` on a package in an older format. Nothing was rendered |
| `load` | `input` | `afspec.LoadSpec` refused the package: a missing artifact, malformed JSON or YAML |
| `write` | `disk` | `--markdown-file` could not be written; `result` is still present |

`retryable` and `resumable` are always false: nothing `specr` does can succeed
on a second identical try.

### Exit codes

| Code | `status` | Meaning |
|---|---|---|
| `0` | `done` | rendered |
| `1` | `failed` | the package could not be read, or `--markdown-file` could not be written |
| `2` | `usage` | usage error — nothing was read |

### Warning codes

| Code | Severity | Stage | When |
|---|---|---|---|
| `spec_invalid` | low, or high for a package in an older format | validate | the package does not validate; it is rendered as it is. An older-format package's tests and tasks are not read, so the render is missing them |
| `section_absent` | low | render | `--section architecture` on a package without `architecture.md` |
| `over_budget` | low | render | the render is still over `--max-tokens` with everything droppable dropped |

## Flags

| Flag | Default | Effect |
|---|---|---|
| `--task` | `0` | render the package scoped to this task id; `0` renders the whole package; negative is a usage error |
| `--section` | every section | render only these sections; comma-separated or repeated; an unknown name is a usage error |
| `--max-tokens` | `0` | drop detail until the render fits; `0` means no budget; negative is a usage error |
| `--markdown-file` | none | also write the Markdown to this file, atomically (a temp file renamed over it), creating a missing parent directory; relative to the working directory. `-` and a directory are usage errors |
| `--specs-dir` | `<dir>/.specs`, or `$AF_SPEC_DIR` | where `NN_name` packages live |
| `--dir` | `.` | the repository the spec root and a relative reference are resolved against |
| `--schema` | | print the self-description below and exit 0 |
| `--version` | | print the build identity and exit 0 |

Flags may come before or after the input.

## Self-description (`--schema`)

`specr --schema` prints one JSON object describing the interface and exits 0.
It reads nothing — no package, no `--dir` — so it runs anywhere:

- `tool`, `description` — the command and one sentence on what it does;
- `input` — what the one positional input may be, with examples;
- `flags` — a standalone JSON Schema (2020-12) document of the flags, each
  with its `type`, `default` and `description`, generated from the flag set,
  so a framework can use it as a tool's input schema with the positional
  input added as a required `input` property. `--schema` and `--version` are
  not listed;
- `result` — a standalone JSON Schema document of the envelope, with `result`
  replaced by `specr`'s own result type and every field described, generated
  from the Go types; the envelope fields `specr` never writes (`input`,
  `model`, `usage`, `side_effects`) are left out;
- `exit_codes` — the three codes and what each means.

The document is checked in at `cmd/specr/testdata/schema.golden.json`, and a
test fails when a flag or a result field changes without it;
`UPDATE_GOLDEN=1 go test -run '^TestSchemaGolden$' ./cmd/specr` regenerates it
deliberately.

## Calling it from an agent

The help, `--schema` and `next` are what make `specr` discoverable: an agent
that knows only the command's name can run `specr --help` or `specr --schema`,
and one that rendered a whole package is told to claim the task it will work on
and which `--task` to render next.

In a skill's instructions, for example agent-fox's `af-impl` at its
"one task at a time" step, which claims the task before the render and records it done
after it:

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

Where a JSON parser is not at hand, `--markdown-file` puts the Markdown in a
file of its own:

```sh
specr .specs/16_indexed_code_search --task 3 --markdown-file /tmp/task-3.md
```

## What it leaves out

`specr` is a fast, read-only call, so the parts of `spec`'s shell that serve a
long model run are absent: there is no model, no `--preflight`, no
`--dry-run`, no `--detail` (the Markdown is the result), no `--output` (stdout
cannot be lost to a run that takes milliseconds), and no progress on stderr.
