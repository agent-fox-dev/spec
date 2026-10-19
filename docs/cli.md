# The `spec` command

```
spec [flags] <input>    a product idea → a validated specification package
```

Its companion `specr`, which renders an existing package as Markdown, shares
this interface and is documented in [specr.md](specr.md).

`spec` turns a product idea into a complete, validated version 2 package under
`.specs/NN_name/`. It takes **exactly one positional input** and writes
**exactly one JSON object** to stdout on every program-driven path. Progress
goes to stderr, so a caller can pipe stdout straight into a parser.

Two paths are for a person and print text instead: `--version` prints the
build identity (the version, then the commit and build time when the build
set them, from the variables in `version.go`), and `-h`/`--help` or a bare invocation prints the help to
stderr. `--help` exits 0 and writes nothing to stdout. A bare invocation exits
2; when stdout is not a terminal it also writes a usage envelope, so a program
still gets one object to parse.

```sh
spec "a cache in front of the widget catalog, with a TTL"
spec ./docs/prds/widget-cache.md --architecture
spec https://github.com/acme/widgets/issues/42 --comment
spec - < idea.md
spec ./idea.md --dry-run
```

## The input

The argument is classified in Go before anything else happens. `--input-kind`
forces the classification instead.

| The argument is | Then it is | What is used |
|---|---|---|
| a GitHub or GitLab issue or pull/merge-request URL, a tab of one (`…/pull/42/files`) included | `issue` | the issue, its body and its comments |
| a path to a readable regular file | `file` | the file's contents |
| `-` | `stdin` | everything piped in |
| anything else | `text` | the text itself |

A path that does not exist is text, not an error, and so is a directory. When
the text is a single line with no whitespace that looks like a path and
nothing exists there, the run says so with a `high` `input_looks_like_path`
warning; `--input-kind file` turns the same mistake into a usage error.

Input is bounded at 256 KB, cut at a line boundary with the cut marked in the
text, and reported as `input.truncated` with an `input_truncated` warning. An
issue with more comments than the forge client reads (five pages) is reported
as `input.comments_truncated`. `--context` is measured against the same bound:
input and context together over it is a usage error.

Flags may come before or after the input.

## How a run works

Every phase reads the repository with the read-only file tools
(`read_file`, `list_files`, `find_files`, `search_files`, `file_outline`,
`find_symbol`, `find_references`) and, when the run's code-search index is
built, `code_search`. No phase has a tool that writes or a shell. The only
thing written is the package, by this program, after the phases.

The run is unattended, and the design follows from that. Nobody is waiting to
answer questions, so the **PRD phase** resolves every open question itself,
records each in a `## Design Decisions` section, and reports the ones it is
least sure of as `result.open_questions`.

Then three **generation phases**, in the order the format fixes, each a fresh
conversation that sees the PRD and the artifacts before it:

1. `requirements.json` — EARS criteria and end-to-end execution paths
2. `test_spec.json` — one flat list of tests
3. `tasks.json` — one flat list of tasks

Each artifact is submitted through a tool whose schema is the format's own JSON
Schema, and whose handler runs every cross-file rule decidable at that point
(`afspec.ValidateGenerationStepComplete`). A violation comes back to the model
naming the rule, and it corrects itself: the repair loop is the agent loop, and
the turn ceiling is the repair budget. One rejection names every problem at
once: the schema errors first, then the cross-file rules that fail as well (run
even when the schema does not pass), then the project audit below. The model
corrects the failing fields with `patch_<artifact>`, JSON-pointer `add`,
`remove` and `replace` operations on the submission that was turned back,
instead of writing the whole artifact again; the patched artifact is checked
exactly like a submission.

An artifact that passes every rule but has warnings on its own file (vague
wording such as "properly" or "as needed", an error outcome without a
`contract`, a scope past the format's limits) is sent back once with them. The
model fixes what it agrees with, or keeps the artifact with a patch of no
operations; the next submission is accepted with whatever warnings remain.

The PRD phase hands on what it learned reading the code as `codebase_notes`
(at most 12000 bytes): the files, the real signatures, the conventions. Every
later phase of the package is shown them, and so is each later scope's PRD
phase in a split, so the same code is not read again phase after phase.

The glossary of `requirements.json` is submitted as a list of
`{term, definition}` entries, because a strict tool schema cannot declare an
object keyed by its data, and is stored as the object the format defines. With `--architecture` a fifth phase writes
`architecture.md`; a failure there is a `architecture_not_written` warning,
not a failed run.

The package is then written, read back from disk, validated, and activated
(`draft → active`, computing the intent hash) if it validates.

### The project audit

The project's language is detected from its manifest (`go.mod`, `Cargo.toml`,
`pyproject.toml`, `package.json`, `pom.xml`, `Gemfile`, …), and a tasks
artifact whose `test_commands` names another ecosystem's runner is refused
before it is written, with the project's real commands in the message. A
repository with several manifests has each of those ecosystems. A command the
table does not know (`./scripts/ci.sh`) is left alone.

### More than one spec's worth of work

A spec is one cohesive feature. When the input is more, the PRD phase writes
the PRD for the first scope and reports the full split; `spec` then writes
**every** scope, one package per scope, each later scope getting its own PRD
phase.

The scopes are **pipelined**. Their PRD phases run one after another, each
shown the Intent, Goals and Non-goals of the scopes before it — from `prd.md`
for a package on disk, and from the PRD this run just wrote for one still
being generated. A scope's package (its three generation phases, the write,
the validation) starts as soon as its PRD exists, beside the next scope's PRD
phase, with at most `--max-parallel` packages (default 3) in flight. Package
numbers are reserved in scope order, and the result reports the packages in
plan order, whichever finishes first. `--max-parallel 1` writes the scopes one
after another, each scope's PRD waiting for the package before it. Progress
lines of a scope carry its label (`scope 2 of 4 (issuex_github): …`).

The split is recorded in the spec root as `<first_scope>.split.json` while it
is unfinished. A run that stops early leaves the plan and the packages it
wrote, and **running `spec` on the same input again resumes from the scope
that failed**. A file or issue input is matched by where it came from; text and
stdin by content. A plan for a different input is reported
(`split_plan_foreign`) and left alone. `--dry-run` writes no plan and resumes
none.

A package's number is reserved when it is allocated, by creating its directory
under a short lock (`.spec-number.lock` in the spec root), so a concurrent run
cannot take the same number. A package that does not validate stops nothing:
it is on disk, its errors name the rules, the later scopes are written, and
the run exits 1 with `category: "invalid_spec"`. Any other failure — a PRD or
generation phase, a `--total-budget` stop before a scope's PRD phase — stops
the run starting anything new; the packages already being generated finish
and are recorded in the plan. The failed scopes are marked `failed` in
`result.split`, the error names the first of them, and the next run on the
same input writes the scopes still without a package. `--total-budget` counts
the phases that have finished, so packages in flight can take a run past it.

## Flags

### spec's own

| Flag | Default | Effect |
|---|---|---|
| `--specs-dir` | `<dir>/.specs`, or `$AF_SPEC_DIR` | where `NN_name` packages live |
| `--name` | the model's choice | override the spec name; must match `[a-z][a-z0-9]*(_[a-z0-9]+)*`; ignored, with a warning, while resuming a split |
| `--architecture` | off | also write the optional `architecture.md` |
| `--no-activate` | off | leave a valid package in `draft` |
| `--comment` | off | post the finished PRD back to the issue the input came from; needs an issue input and a forge token (`GITHUB_TOKEN`/`GH_TOKEN` or `GITLAB_TOKEN`). An invalid package's PRD is not posted |
| `--max-parallel` | 3 | how many packages of a split are generated at once, beside the next scope's PRD phase; `1` writes the scopes one after another; less is a usage error |

### The model and the ceilings

| Flag | Default | Effect |
|---|---|---|
| `--dir` | `.` | the repository to work in; the file tools cannot reach outside it |
| `--model` | `$AF_MODEL`, else `$AGENTKIT_MODEL`, else `STANDARD` | a tier (`SIMPLE`, `STANDARD`, `ADVANCED`) or a model id from AgentKit's catalog (an `anthropic/` prefix is accepted). An id the catalog does not list is refused, since it would have no price and no budget could stop it |
| `--effort` | `$AF_MODEL_EFFORT`, else each phase's tier effort | `off`, `low`, `medium`, `high`, `xhigh`, `max`, for every phase. An effort a phase's model does not take is lowered to the highest it does, with an `effort_clamped` warning naming the phases |
| `--phase-model` | none | `PHASE=MODEL[:EFFORT]`, repeatable: run one phase — `prd`, `requirements`, `test_spec`, `tasks` or `architecture` — on another model (a tier or a catalog id) and, when given, effort. Without an effort the phase takes `--effort`, else the tier's effort for that phase, else none. A later value for the same phase wins. A phase or effort that does not exist is a usage error; an unknown model is a `model` failure |
| `--max-turns` | 60 | per-phase turn ceiling, also the repair budget |
| `--max-tokens` | 65536 | the output token cap of one model response in every phase. The model's thinking is spent from the same cap as its reply, so a phase on a high effort can use it up before it calls its submit tool; that fails as `no_result`, and this is the flag to raise. The model's own output ceiling still applies, so a larger value than it allows is lowered to it. Not `specr --max-tokens`, which is a render budget |
| `--budget` | 5.00 | per-phase spend ceiling, in dollars |
| `--phase-timeout` | none | wall-clock ceiling on one phase |
| `--total-budget` | none | ceiling on the whole run. Checked before each scope's PRD phase after the first (a split that stops there keeps its plan); an input that does not split has no interior boundary, so there it lowers the one phase's ceiling like `--budget` |
| `--repo-map-tokens` | 6000 | token budget of the repository map (tracked files and their top-level declarations) in every phase's prompt; `0` disables it. A map that cannot be built is a `repo_map_build_failed` warning |
| `--trust-project` | off | admit the repository's `AGENTS.md` and `CLAUDE.md` into every phase's system prompt. `.specs/steering.md` is always given to the phases |
| `--context` | none | additional context, repeatable; each value is one paragraph of a labelled block shown beside the input |

A tier chooses a model and an effort per phase. The PRD and the requirements
are where a spec's judgment is made; the test spec, the tasks and the
architecture turn them into structure the validator checks, and every turn of
a phase pays for its effort, the turns that only read a file included. So the
later phases run lower:

| Tier | `prd` | `requirements` | `test_spec`, `tasks`, `architecture` |
|---|---|---|---|
| `SIMPLE` | `claude-sonnet-5-5` `medium` | `claude-sonnet-5-5` `medium` | `claude-sonnet-5-5` `low` |
| `STANDARD` | `claude-sonnet-5-5` `high` | `claude-sonnet-5-5` `high` | `claude-sonnet-5-5` `medium` |
| `ADVANCED` | `claude-opus-5-5` `xhigh` | `claude-opus-5-5` `high` | `claude-sonnet-5-5` `high` |

A model id given to `--model` runs every phase, at `--effort` or none. To run
everything at one effort, give `--effort`; to change one phase, give
`--phase-model`:

```sh
spec ./idea.md --model ADVANCED --phase-model tasks=claude-haiku-4-5:medium
spec ./idea.md --phase-model prd=ADVANCED      # STANDARD, with the PRD on ADVANCED's
```

### The run

| Flag | Default | Effect |
|---|---|---|
| `--input-kind` | guess | force the input's classification: `file`, `text`, `issue` or `stdin`; a mismatch is a usage error raised before anything is read or fetched |
| `--dry-run` | off | write nothing to disk or to a forge: no package, no plan, no comment. The phases still run and the package that would have been written is reported, its `spec_package` artifact marked `"dry_run": true` |
| `--preflight` | off | run every check that would refuse the run, then stop before any model phase (see below) |
| `--detail` | `summary` | `summary` prints the trimmed `result`, `full` everything computed |
| `--output` | none | also write the envelope stdout gets to this file, atomically and before stdout; relative to the working directory. `-` and a directory are usage errors; a failed write is an `output_not_written` warning |
| `--verbose` | off | trace tool calls, turns, phase ceilings and any response cut off at the output token limit on stderr |
| `--quiet` | off | print nothing on stderr |
| `--show-text` | off | stream the model's prose to stderr |
| `--version` | | print the build identity and exit |

A negative `--budget`, `--total-budget`, `--max-turns`, `--max-tokens`,
`--phase-timeout` or `--repo-map-tokens` is a usage error.

## Preflight (`--preflight`)

`--preflight` performs every check the ordinary run makes before its first
model call — flag combinations, `--dir`, the input (classified and fetched),
the model, its credential, `--name`, `--comment`'s preconditions, the schemas,
and any split plan to resume — reports them, and stops. A run that would be
refused is refused identically, with the same stage, category, message and
exit code. One whose checks pass exits 0 with `result.stage` `preflight`,
`result.preflight` (one `{check, ok, detail}` per check, including the
informational `symbol_backend`, `code_search_index` and `phase_models` — the
model and effort each phase will run on, e.g.
`prd claude-opus-5-5/xhigh; requirements claude-opus-5-5/high; …`) and
`result.estimate`:

```json
"estimate": {
  "phases": 4,
  "max_turns_per_phase": 60,
  "max_budget_per_phase_usd": 5,
  "max_total_usd": 20
}
```

`phases` is the PRD phase plus one per generation step, plus one with
`--architecture`, for the next package; when a split is being resumed, that
figure times the scopes still to write. A split a not-yet-written PRD calls
for is the model's decision, so the figure is a lower bound.

## The output

```jsonc
{
  "tool": "spec",
  "version": "v0.3.0",
  "ok": true,
  "status": "done",
  "exit_code": 0,
  "summary": "spec: wrote .specs/07_widget_cache; 2 open questions",
  "warnings": [ { "code": "project_language_unknown", "severity": "low", "stage": "preflight", "message": "…" } ],
  "result": { /* see below */ },
  "artifacts": [ { "kind": "spec_package", "path": ".specs/07_widget_cache", "id": "07", "valid": true } ],
  "side_effects": [ { "action": "comment", "kind": "prd", "target": "acme/widgets#42", "url": "…", "ok": true } ],
  "next": [ { "tool": "impl", "input": ".specs/07_widget_cache", "flags": [], "why": "the package validates and is ready to implement" } ],
  "input": { "kind": "issue", "origin": "https://github.com/acme/widgets/issues/42", "bytes": 3184 },
  "model": { "spec": "STANDARD", "id": "claude-sonnet-5-5", "effort": "high",
             "phases": { "prd": { "id": "claude-sonnet-5-5", "effort": "high" },
                         "tasks": { "id": "claude-sonnet-5-5", "effort": "medium" }, … } },
  "usage": { "input_tokens": 48211, "output_tokens": 31004, "cache_read_tokens": 212000,
             "cache_creation_tokens": 18400, "cost_usd": 1.19, "turns": 23,
             "phases": [ { "name": "prd", "scope": "widget_cache", "model": "claude-sonnet-5-5", "effort": "high",
                           "turns": 9, "stop_reason": "tool_terminate", … } ] },
  "duration_ms": 214003,
  "started_at": "2026-10-13T13:20:30Z"
}
```

`ok` is the one field a caller has to read: true only when the whole job was
done, never beside a non-zero `exit_code` or an `error`. `status` is derived
from the exit code. `summary` is one sentence of at most 200 characters, with a
clause counting high-severity warnings on a success.

`usage.input_tokens` is net of the prompt cache; the cache figures are what
make it agree with `cost_usd`. Each phase in `usage.phases[]` carries the same
figures, its `scope` (the spec it worked on), the `model` and `effort` it ran
on, `tool_calls` and `tool_errors` by tool. `model.id` and `model.effort` are
what `--model` resolved to; `model.phases` is what each phase was given. Five or more failed tool calls in one phase raise a `tool_errors`
warning.

`artifacts` lists one `spec_package` per package written (and a `comment` per
PRD posted), built from facts the program holds, never from the model's
account. `side_effects` lists every write to a forge. `next` suggests `impl`
(agent-fox's implementation tool) on every package that validates, in split
order, and `spec` again on the same input while a split is unfinished; an
input that was raw text or stdin is named `<same input>`.

### Reading it from Go

The shape of this object is a public Go package,
[`envelope`](api.md#envelope), which depends on the standard library only.
`envelope.DecodeSpec` reads what `spec` wrote into `envelope.SpecEnvelope`,
and the command builds its output from the same types, so a program that
decodes with them cannot drift from it:

```go
env, err := envelope.DecodeSpec(stdout)
if err != nil {
    return err // not an envelope
}
if !env.OK {
    return env.Error // an *envelope.ErrorInfo; it is an error
}
for _, p := range append([]envelope.Package{env.Result.Package}, env.Result.FollowOnSpecs...) {
    fmt.Println(p.SpecDir, p.Validation.Valid, len(p.OpenQuestions))
}
```

The result of `--detail summary` decodes into the same `envelope.SpecResult`,
with the fields the summary leaves out zero; `Detail` says which view it was.

### `error`

Present exactly when `ok` is false:

```jsonc
"error": { "stage": "requirements", "category": "budget",
           "message": "…", "retryable": false, "resumable": false,
           "fix_hint": { "flag": "--budget", "current": 5, "suggest": 10 } }
```

`stage` names the step: `usage`, `input`, `preflight`, `prd`, `plan`,
`requirements`, `test_spec`, `tasks`, `scaffold`, `write`, `validate`,
`budget`. `resumable` is true while an unfinished split plan exists (never at
stage `preflight`). `fix_hint` names a mechanical remedy where one is known: the
flag to raise for a ceiling, the environment variables for a missing
credential, the tiers for an unknown model.

A run that fails while it generates a package (stage `requirements`,
`test_spec` or `tasks`) has written nothing: the package directory is removed.
Its `result` keeps what the PRD phase decided (`spec_id`, `spec_name`, `title`,
`open_questions`) and claims no package: `spec_dir` and `status` are empty,
`artifacts` is `[]`, there is no `spec_package` entry in the envelope's
`artifacts`, and `summary` is the error. A failure after the package is
written, such as `invalid_spec`, still names the package on disk.

| Category | Means | `retryable` |
|---|---|---|
| `usage` | the invocation was wrong; nothing was fetched or written | no |
| `input` | the input could not be read (a private issue, an unreadable file) | no |
| `auth` | no credential for the model, or for the forge under `--comment` | no |
| `model` | the model could not be resolved | no |
| `api` | a provider or transport failure | yes |
| `budget`, `max_turns` | a phase stopped at its ceiling | no |
| `aborted` | cancelled (Ctrl-C) or past `--phase-timeout` | yes |
| `refusal` | the model declined the request | no |
| `no_result` | a phase ended without an accepted submission; the message says why and what to change: a ceiling to raise, or, when a response hit the output token limit while the model was still thinking, `--max-tokens` to raise or the effort to lower | no |
| `disk` | the filesystem refused a write | no |
| `invalid_spec` | the package was written and does not validate | no |
| `internal` | a bug in spec | no |

### Exit codes

| Code | `status` | Meaning |
|---|---|---|
| `0` | `done` | a valid package was written |
| `1` | `failed` | failed; the stage is named. An `invalid_spec` failure still leaves the package on disk; a split that stopped early keeps its plan to resume from |
| `2` | `usage` | usage error — nothing was fetched, nothing was written |

### `result`

`result` describes the first package this run wrote: `spec_dir`, `spec_id`,
`spec_name`, `title`, `status`, `source`, the `artifacts` written, the counts
(`requirements`, `criteria`, `execution_paths`, `tests`, `tasks`), and:

- `validation` — the format's verdict, every error naming its rule (`C1`…`C11`,
  `json_schema`, `completeness`)
- `traceability` — derived, never stored: `criteria_covered`,
  `criteria_uncovered`, `paths_covered`, `paths_uncovered`, `tests_unowned`
- `open_questions` — the decisions made under uncertainty
- `relevant_files` — the files the PRD phase pointed the later phases at
- `comment_url` — the PRD comment, under `--comment`

When the input was more than one spec's worth of work: `follow_on_specs` (the
further packages, each with the same fields), `split` (every scope with its
`status`: `done`, `invalid`, `failed` or `pending`, and for the packages this
run wrote `valid`, `error_count`, `open_questions_count`, `trace_gaps`) and
`split_plan` (present only while the split is unfinished).

Under `--detail summary` (the default) `result` keeps `spec_dir`, `spec_id`,
`spec_name`, `status`, `artifacts`, `validation` without its warnings,
`traceability`, `open_questions`, `split`, and on a `--preflight` run `stage`,
`preflight` and `estimate`. `result.detail` says which view was printed.

### Warning codes

| Code | Severity | Stage |
|---|---|---|
| `input_truncated` | high | input |
| `input_looks_like_path` | high | input |
| `comments_unreadable` | low | input |
| `comments_truncated` | low | input |
| `effort_clamped` | low | preflight |
| `code_search_unavailable` | low | preflight |
| `specs_dir_unreadable` | low | preflight |
| `project_language_unknown` | low | preflight |
| `name_flag_ignored` | low | usage |
| `repo_map_build_failed` | low | repo_map |
| `scope_renamed` | low | prd |
| `scope_count_mismatch` | low | prd |
| `relevant_files_unavailable` | low | prd |
| `tool_errors` | low | phase |
| `split_plan_foreign` | low | split |
| `split_plan_unreadable` | low | split |
| `split_plan_stale` | high | split |
| `split_plan_update_failed` | high | split |
| `split_plan_not_removed` | low | split |
| `architecture_not_written` | low | write |
| `activation_failed` | high | activate |
| `comment_not_posted` | low | report |
| `output_not_written` | low | emit |

## What changed from agent-fox's `spec`

The command was moved here from agent-fox, where it shared a shell with
`triage`, `fix` and `impl`. The pipeline, its flags, its result and its exit
codes are the same. What a single command did not need was left behind:
`--schema` and its self-description document, the events file and
`--emit-events`, the report file and `--report-file`, `schema_version`,
`session_id`, `timings`, `x-trust` and `untrusted_fields`, and `--vendor` (the
tiers resolve to Claude models only). `--effort` lost `minimal`, which AgentKit
no longer sends. [ADR 01](adr/01-run-the-spec-command-on-the-agentkit-driver.md)
records the decision.
