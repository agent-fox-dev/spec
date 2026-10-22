---
spec_id: "13"
spec_name: "state_command_cross_links"
title: "Make spec, specr and speca point at one another"
status: "active"
created_at: "2026-10-22T07:45:48.451036Z"
updated_at: "2026-10-22T07:45:48.451036Z"
intent_hash: "a476ea09d5610fc30f80a29f167924f82c4dc496597eb74bb57fe470c261c3c7"
schema_version: 2
source: "docs/prd/02-change-the-state-of-a-package-with-its-own-command.md"
---
## Intent

`spec`, `specr` and `speca` are three commands on one interface. An agent that finds one of them should learn about the other two from the command itself, not from this repository. This spec makes each command's help, and each `--schema` description, name all three in one sentence. It makes `specr`'s `next` suggest the `speca` call that claims a task or records it done, wherever that call would succeed. It also adds those calls around the render in the agent workflow the docs quote, so that an agent records its progress through the library and never edits `tasks.json` by hand.

## Goals

- `spec --help`, `specr --help` and `speca --help` each print the same sentence naming the three commands, on its own line before `Usage:`. The sentence comes from one constant.
- `specr --schema` and `speca --schema` each end their `description` with that sentence, and both golden files are regenerated.
- After a whole render, `specr`'s `next` holds the claim `speca <spec_dir> --task M --state in_progress` exactly when `speca <spec>` would suggest the same claim. M is also the task that `specr`'s own render suggestion names.
- After `specr <spec> --task N`, `next` holds the claim of task N when N is pending and ready, or `speca <spec_dir> --task N --state done` when N is in progress. In both cases this happens only when the package can take a write. `specr`'s existing render suggestion is kept unchanged.
- "Can take a write" (intent drift and writability) is decided by one function that both `specr` and `speca` call.
- The agent-fox `af-impl` "one task at a time" example in `docs/specr.md` and `docs/speca.md` claims the task before the render and records it done after.

## Non-goals

- Giving `spec` a `--schema` flag. `spec` has none (ADR 05: "`--schema`, which `spec` left behind"), so it names its siblings in its help and package doc comment only.
- Changing `spec`'s `next`, which suggests agent-fox's `impl`.
- Changing any of `speca`'s own `next` rules: the report, list and post-move suggestions from `10_speca_state_report` and `11_speca_task_states`, and the activate/seal suggestions from `12_speca_lifecycle`.
- Having `specr` suggest lifecycle calls (`--status active`, `--status sealed`) or a release (`--state pending`). `specr` cannot tell that an agent gave up a task, and the lifecycle suggestions belong to `speca`'s report.
- Any change to `envelope`: no new field on `RenderResult` or `Next`, no new code, no new category.
- Checking whether a sibling command is installed. `make build` installs all three, so a suggestion names the command regardless.
- Editing agent-fox's `af-impl` skill itself, which lives in another repository. Only the copy quoted in this repository's docs changes.
- Merging the three commands into one, or adding verbs. That was rejected in the source PRD's decision 14.
- A new ADR.

## Background

The three commands:

- `cmd/spec/main.go` builds `spec` on `cli.App`. Its help is the `usage` raw-string constant, which `App.Main` prints followed by the flag defaults on `-h`, `--help` and a bare invocation. `spec` has no `--schema`.
- `cmd/specr/main.go` has its own `usage` constant and a `description` constant that `describe(fs)` puts into the `--schema` document. That document is checked in at `cmd/specr/testdata/schema.golden.json`, guarded by `TestSchemaGolden`.
- `speca` (`cmd/speca`, from `10_speca_state_report`) is planned with a help line and a `--schema` description containing "spec writes a package from an idea, specr renders one, speca reports its state". Its writing forms (`11_speca_task_states`, `12_speca_lifecycle`) make "reports its state" an understatement.

Neither `spec`'s help nor `specr`'s help mentions another command. `docs/cli.md` mentions `specr` as a companion, and nothing mentions `speca` outside its own docs.

`specr`'s `next` (`(*Result).Next` in `cmd/specr/render.go`) suggests only `specr <spec_dir> --task M`. M is the first task not `done` or `dropped`: after a whole render, from the start of the plan; after `--task N`, after N. An agent that renders a task learns nothing about claiming it or recording it done. Until `speca` exists, there is no call to suggest.

`speca` defines a writable package as a draft, or an active package without intent drift (`11_speca_task_states`' glossary). It suggests a claim from its report only for a writable package with no task in progress whose `next_task` is ready. The intent-drift check is planned as a private helper in `cmd/speca`, so `specr` cannot ask the same question.

`docs/specr.md` §"Calling it from an agent" quotes agent-fox's `af-impl` "one task at a time" step. The step renders the task and stops there. The source PRD's background describes what agents do next: they mark the task done with `sed` on `tasks.json`.

## Requirements

### 1. One sentence names the three commands

`internal/cli` gains an exported string constant, `SiblingCommands`, whose value is exactly:

`spec writes a package from an idea, specr renders one, speca reports and changes its state.`

It is the only place this sentence is written. All three commands import `internal/cli` already, and each one uses the constant rather than a copy.

### 2. Every command's help names the other two

- The help of `spec`, `specr` and `speca` (the text printed on `-h`, `--help` and a bare invocation) holds a line equal to `cli.SiblingCommands`. It sits after the help's title line and before its `Usage:` line, with a blank line on each side.
- `speca`'s help line, written as a literal by `10_speca_state_report`, is replaced by the constant. Any `speca` test that asserts the old wording ("speca reports its state") is changed to assert the constant.
- `specr`'s help paragraph about the output says that `next` names the task to render after this one and, when the package can take the change, the `speca` call that claims the rendered task or records it done.
- Nothing else in any help changes. `--help` still exits 0 with nothing on stdout. A bare invocation still exits 2 and writes a usage envelope when stdout is not a terminal.
- The package doc comment of each of `cmd/spec`, `cmd/specr` and `cmd/speca` names the other two commands in one sentence, so `go doc` and `docs/generated/cmd/` show the same thing.

### 3. Every `--schema` description names the other two

- `specr --schema`'s `description` is its current sentence, then one space, then `cli.SiblingCommands`.
- `speca --schema`'s `description` is its own sentence, then one space, then `cli.SiblingCommands`, replacing the literal sibling sentence `10_speca_state_report` put there.
- `cmd/specr/testdata/schema.golden.json` and `cmd/speca/testdata/schema.golden.json` are regenerated with `UPDATE_GOLDEN=1`. The only diff in each is the `description` value. No flag, result field or exit code changes.

### 4. One answer to "can this package take a write"

- `internal/specpkg` gains two functions:
  - `IntentDrift(spec *afspec.Spec) bool`: true exactly when the package's status is `active`, its `IntentHash` is set, and `afspec.ComputeIntentHash(spec.PRDBody)` either returns an error or returns a different hash.
  - `Writable(spec *afspec.Spec) bool`: true exactly when the status is `draft`, or the status is `active` and `IntentDrift` is false.
  A nil spec gives false from both.
- `cmd/speca` calls these wherever it decides intent drift or writability: the report's `intent_drift`, the task form's status refusal, and the report's and list's claim suggestions. Any private copy of the drift check in `cmd/speca` is removed. `speca`'s output does not change.
- `specr` calls `specpkg.Writable` once per call, after loading the package. It keeps the answer in an unexported field of its `Result`, which never appears in the JSON output or in `--schema`.

### 5. `specr`'s `next` after a whole render

A whole render is any render without `--task`, with or without `--section` or `--max-tokens`. M is the task `specr`'s own suggestion names today: the first task in plan order that is not `done` or `dropped`.

- `next` gains `{tool: "speca", input: <spec_dir>, flags: ["--task", "M", "--state", "in_progress"]}` when all of these hold:
  - the package is writable;
  - no task in the plan is `in_progress`;
  - M exists, is `pending`, and is ready (`TaskInfo.ready`).

  Under these conditions M is also `speca`'s `next_task`, so `specr` and `speca <spec>` suggest the same claim.
- The entry's `why` names the task and its title, says it is the next ready task, and says to claim it before working on it so that no other agent takes it.
- In every other case, `next` holds no `speca` entry: a task in progress, a first unfinished task that is not ready, a package that cannot take a write, or no unfinished task at all. Suggesting a claim the state machine would refuse, or one `speca` would not suggest itself, would send an agent into a failure.

### 6. `specr`'s `next` after a task render

After `specr <spec> --task N` with the package writable:

- **N is `pending` and ready.** `next` gains the claim `{tool: "speca", input: <spec_dir>, flags: ["--task", "N", "--state", "in_progress"]}`. Its `why` says to claim task N before working on it. Another task being in progress does not withhold this claim: the caller chose N, and the claim is the compare-and-set that settles a race.
- **N is `pending` and not ready.** `next` gains no `speca` entry.
- **N is `in_progress`.** `next` gains `{tool: "speca", input: <spec_dir>, flags: ["--task", "N", "--state", "done"]}`. Its `why` says to record task N done once its work is finished and its tests pass.
- **N is `done` or `dropped`.** `next` gains no `speca` entry.

When the package is not writable (sealed, superseded, archived, or active with intent drift), no `speca` entry is added in any case.

### 7. Shape and order of `next`

- The `speca` entry, when there is one, comes first, and `specr`'s render suggestion second. The order is the order an agent makes the calls: claim, then render; or record done, then render the next task.
- `specr`'s render suggestion keeps its rule, flags and `why` unchanged. When it is absent (no unfinished task after N), `next` holds only the `speca` entry, or nothing.
- Each `speca` entry's `input` is `result.spec_dir`, the same input as the render suggestion. Its `flags` are four elements, each value separate.
- `next` is still produced from `Result.Next()` in `cmd/specr/render.go`. It is present whenever a result is present, including a `--markdown-file` write failure, and absent when the package could not be loaded. Nothing in `envelope` or `internal/cli` changes, and `specr` still changes nothing on disk.

### 8. Documentation

As the steering requires, in the same change:

- **`docs/specr.md`**:
  - its introduction names `spec` and `speca`;
  - §"`artifacts` and `next`" describes the `speca` entries of requirements 5–7;
  - the JSON example's `next` matches what the example's render would now produce;
  - §"Calling it from an agent" replaces the `af-impl` block with the one below.
- **`docs/speca.md`**: its section on calling it from an agent carries the same `af-impl` block, and says that `specr`'s `next` suggests the claim and done calls.
- **`docs/cli.md`**: the companion paragraph near the top names both `specr` and `speca` and links both docs.
- **`README.md`**: the sentence on `specr`'s `next` mentions the claim and done suggestions.
- **`docs/architecture.md`**: one sentence that the three commands name one another through `cli.SiblingCommands`, and that `internal/specpkg` holds the writability rule both `specr` and `speca` use.
- **`docs/api.md`**: no change, because no public API changes.
- **`docs/generated/`**: regenerated with `make doc`. The command package doc comments change.

The `af-impl` block, in both docs:

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

No test checks the content of any doc.

## Design Decisions

1. **One constant in `internal/cli`, used by every help and description.** Three literal copies of one sentence would drift. `internal/cli` is the one package all three commands already import, and a Go `const` can be concatenated into each raw-string `usage` constant.
2. **The wording is the source PRD's: "speca reports and changes its state".** Once `11_speca_task_states` and `12_speca_lifecycle` land, "reports its state", which `10_speca_state_report` wrote, no longer describes the command. This scope owns the sentence, so it replaces that literal and its test assertion.
3. **The line sits between the title and `Usage:`.** "Near the top" becomes a position a test can check, and it is the first thing a reader of `--help` sees after what the command itself does.
4. **`spec` gets no `--schema` in this scope.** Adding one would be a new interface surface with its own golden file and result description, well beyond a cross-link. Its help and package doc comment carry the sentence.
5. **Each `--schema` description is the command's own sentence followed by the sibling sentence.** A framework that declares one tool reads what that tool does first, then that two siblings exist.
6. **After a whole render, the claim follows `speca`'s report rule exactly: writable, nothing in progress, first unfinished task pending and ready.** Under that rule the claimed task is both `specr`'s render target and `speca`'s `next_task`. The two commands never suggest different claims, and the claim and the render suggestion always name one task.
7. **After `--task N`, the claim requires N to be ready but not that nothing else is in progress.** An explicit `--task` is the caller's choice, and the claim is a safe compare-and-set. A task whose dependencies are not done would raise `task_not_ready`, and suggesting it would advise against the plan.
8. **`done` is suggested only after a render of an in-progress task.** That is when the agent holds the task. Suggesting `done` after a whole render would assume work the call knows nothing about.
9. **No `speca` suggestion for a package that cannot take a write.** `speca` refuses those writes at stage `status`, and `next` should never name a call that is certain to fail.
10. **The `speca` entry comes before the render suggestion.** `next` drives the caller's next call, and the claim (or the done) is that call. The render suggestion keeps its content, so a program that filters `next` by `tool` sees no change. See open questions.
11. **Writability moves into `internal/specpkg` as `IntentDrift` and `Writable`.** `specr` and `speca` must agree on whether a package takes a write, for the same reason `10_speca_state_report` shared `TaskInfo` and `ready`: a rule written twice drifts.
12. **`specr` keeps writability in an unexported field, not in `RenderResult`.** The source PRD says nothing in the envelope changes. Adding `intent_drift` or `writable` to `specr`'s public result would widen its interface only to feed `next`.
13. **`specr` never suggests a release (`--state pending`).** Nothing in a render tells it that an agent gave up. The `af-impl` block teaches the release instead.
14. **The `af-impl` fallback forbids hand edits of `tasks.json`.** The source PRD exists because agents edited it with `sed`. When `speca` is missing, reporting the finished task is the safe substitute.
15. **No new ADR.** The decision to build three commands on one interface, and to close the discoverability gap with help lines and crossing `next` entries rather than a merge, belongs to `speca`'s ADR from `10_speca_state_report`. This scope implements a consequence of it.

## Dependencies

| Spec | Reason |
|---|---|
| `10_speca_state_report` | Provides `cmd/speca` with its help, `description`, `--schema` golden file and `docs/speca.md`; `internal/specpkg`, where `IntentDrift` and `Writable` go; and `TaskInfo.ready`, which `specr`'s `next` reads. |
| `11_speca_task_states` | Provides `speca <spec> --task N --state S`, the claim and done calls that `specr` suggests and the `af-impl` block uses; defines the writable-package rule and the report's claim suggestion that this spec makes `specr` match; and holds the private drift check this spec moves into `internal/specpkg`. |
| `12_speca_lifecycle` | Edits `speca`'s help, `--schema` golden file and `docs/speca.md` before this spec does. This spec's changes to them are applied on top, and the golden file is regenerated after them. |
