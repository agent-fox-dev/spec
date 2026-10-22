# 10. Report and change the state of a package with its own command

**Status:** accepted
**Date:** 2026-10-22
**Related:** [ADR 05](05-render-a-package-with-its-own-command.md),
[ADR 01](01-run-the-spec-command-on-the-agentkit-driver.md),
[PRD 02](../prd/02-change-the-state-of-a-package-with-its-own-command.md)

## Context

A package has two kinds of state: its lifecycle status, in the PRD
frontmatter, and the state of each task, in `tasks.json`. `afspec` implements
both state machines and the validation that tells whether a package is sound,
and none of it can be reached from a shell. An agent that wants to know which
task is next reads `tasks.json` and picks one; an agent that wants to know
whether a package validates writes a Go program. `README.md` and
`architecture.md` both said lifecycle management "has no CLI surface".

ADR 05 gave the read side of rendering a command of its own, `specr`, for the
same reason, and made its interface the one `spec` and agent-fox's tools share.
The state of a package is the next thing an agent with a shell needs, and it
needs it before it renders anything: which package, which task.

The library could not say which task to work on. `examples/tasks` decides it in
`pick`, which treats a task without `depends_on` as having no dependencies;
format v2 §8.3 says it means the previous task.

## Decision

A third command, `speca`, the spec admin command, in `cmd/speca`. It reports
and, in later scopes, changes a package's state through `afspec`, with the same
guards the library gives an embedder. Its interface is `specr`'s, for the same
reason: an agent that can drive one command can drive all of them.

- **One positional input**, a reference to a package, resolved as `specr`
  resolves it (none with `--list`); **one JSON envelope** on stdout on every
  program-driven path; the help on stderr; `--schema`, checked in as a golden
  file; `--version`; the exit codes 0, 1 and 2.
- **Forms are flags, and a bare `speca <spec>` is the report.** An accidental
  call never changes anything, and an agent that knows only the command's name
  learns the package's state from it. At most one operation is named per call.
- **The read-only forms come first.** `speca <spec>` reports the package's
  identity, whether it validates, whether its intent has drifted, every task
  with its state and readiness, the next task, the lifecycle moves allowed from
  its status, and the packages it depends on and that depend on it.
  `speca <spec> --validate` is the format's full verdict, with the cross-spec
  rules that concern the package. `speca --list` is every package under the
  spec root, with `--all` adding `archive/`. They need no write, and they
  establish what the forms that change state build on: the shared resolver, the
  output type and the readiness rule. The forms that move a task (spec 11) and
  that move a package through its lifecycle (spec 12) were added under this
  decision by the scopes that followed.
- **`next` only suggests calls that exist.** A suggestion made before the form
  exists would send an agent into a usage error. It began with the render,
  `specr <spec> --task N`; the task form (spec 11) added the claim before it, and
  the status form (spec 12) the suggestions to activate a draft that validates
  and has an intent, and to seal an active package with no task open, whose
  conditions are the checks of the status call, so that it is never refused.
- **A package that does not validate is a successful call.** The question was
  whether it validates, and the answer is in `valid`. Exit 1 is for a call that
  could not do its job: the package could not be loaded, or `--list` could not
  read its spec root. A task-form call that is refused after the package was read
  (a move the machine does not allow, a package that can no longer take a write,
  keys a save would delete, a failed write) is exit 1 as well.
- **Readiness follows the format.** `(*TasksV2Json).IsReady` and `NextTask` are
  in `afspec`. A task's dependencies are its `depends_on` when present, an
  empty list meaning none, otherwise the task before it (§8.3); only `done`
  satisfies a dependency. `speca` uses both, `specr` shows `IsReady` on each task
  it lists and `examples/tasks` picks its task with `NextTask`, so "ready" and
  "next" mean one thing.
- **The read-only forms write no file, and no form makes a model or network call
  or reads a credential.** `speca` runs in milliseconds on the files alone. The
  first form that writes is the task form (spec 11), below.

### The task form

`speca <spec> --task T --state S` moves tasks through the library's task state
machine and saves the package with `Spec.Save`. It is the form the first
decision above made room for, and it keeps the decision's shape:

- **The library makes every change; the command adds checks.** A move is
  `TransitionTask`, `--force`'s bypass is `CompleteTaskStates` or
  `ResetTaskStates` (the library has no setter for `in_progress` or `dropped`, so
  `--force` covers neither), and the write is `Save`. One state machine cannot
  drift from a copy of itself.
- **All moves or none.** Every named move is checked before any is made. A refused
  move is a lifecycle refusal (category `lifecycle`, not `usage`: the package was
  read and the call was well-formed), carrying the states the task may move to.
- **The state machine is the compare-and-set, and there is no lock.** A claim of
  a task already in progress is not a move, so the second of two agents is
  refused and learns the task is taken, when its call comes after the first was
  written. Two calls that read the package at the same moment are not told apart.
- **A refusal carries the package as it is on disk,** without a summary of its own
  and without `next`, so the caller sees the states and is not pointed at a task
  another agent holds.
- **`Save` is not trusted to refuse.** The command refuses a package `Save` would
  refuse (sealed, superseded, archived, or intent drifted) before looking at any
  move, so that `--dry-run` reports the refusal the real call would meet; and it
  refuses a write that would delete keys the schema does not know, which `Save`
  does silently, unless `--force` says so. The keys of the three JSON artifacts
  are the library's to report; the frontmatter of `prd.md` is checked by the
  command, because the library does not. What `Save` does to a package, the
  command inherits: every artifact is written again, in the canonical encoding,
  and a YAML comment in the frontmatter is not kept.

### The status form

`speca <spec> --status S [--by <spec>]` moves a package through its lifecycle
(spec 12) and keeps the shape of the task form: the library makes every change,
and the command adds the checks an operator would make before it.

- **The status and the move are two calls.** `Transition` and `Supersede` write
  the status; `MoveUnderArchive` only moves the directory and rewrites no file.
  That keeps a superseded package `superseded` under `archive/`, tells a failed
  status write from a failed move, and makes the resume a plain move-only call.
  `MoveToArchive` is built on them and looks at the destination before it writes.
- **Checks before any write, in a fixed order,** and the first refusal ends the
  call with no result: the load (a version 1 package is refused), the move (with
  the shortest chain of allowed moves when it is not allowed), the target's own
  checks, the keys a write would drop, and the archive destination. `--force`
  reaches three of them (an activation that does not validate, a sealing with a
  task open, keys that would be dropped) and the rules of the format it does not:
  the lifecycle, a missing or drifted intent, the replacing package's status, a
  taken destination.
- **Superseding writes the step most likely to fail last:** the banner and the
  status, then the id in the replacing package's `supersedes`, then the move. A
  replacing package that cannot be saved is a warning, not a failure: the change
  asked for is the old package's.
- **A failed move is resumable, not retryable.** The envelope's `retryable` is
  decided by the category and stays false; `resumable` is true, and `next` holds
  the call that finishes the work. The result is the package where it is, with
  what was written.

### Shared code

What `spec`, `specr` and `speca` each need and none should copy lives in
`internal/specpkg`: the resolver of a package reference, the listing of the
spec root, the display rule for a path, the task summary and the converters of
a verdict and of derived coverage into the envelope's types. It is not in
`envelope`, which must stay standard-library only, and not in `internal/cli`,
which does not import `afspec`.

`AdminResult` is one result type for every form, in `envelope`. A field a form
does not produce is absent, so one decoder, `DecodeSpeca`, reads all of them,
and the later scopes add their fields to the same type.

## What it does not carry

- Editing a package's content, creating a package, or rendering one. `spec`
  creates, `specr` renders, and the content of a package is not `speca`'s to
  change.
- A lock between agents. Two agents that write one package at once get the last
  writer's files, as they do with the library.
- A smaller binary. Like `specr`, `speca` links `internal/cli` and so the model
  stack (ADR 05, Consequences), and never uses it. Splitting the envelope out of
  that stack is still left for when it matters.

## Consequences

- `make build` installs three commands. `speca` is stamped the same way as the
  other two.
- The README's "no CLI surface" sentence, which was true of reading a package's
  state, of moving its tasks and of changing its lifecycle status, was corrected
  for each as its form arrived, and is gone.
- `specr`'s tasks carry `ready`, the one change to its output. A field on the
  shared `TaskInfo` never means something different depending on which command
  wrote it.
- `speca --validate` and `speca --list` load every package of the spec root.
  For `--validate` that is the price of the cross-spec rules, which cannot be
  applied to one package alone; `--list` loads each package to say whether it
  validates and what its next task is.
