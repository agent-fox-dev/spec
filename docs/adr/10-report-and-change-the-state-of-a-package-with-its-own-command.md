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
  output type and the readiness rule. The forms that move a task, and the ones
  that move a package through its lifecycle, are added under this decision by
  the scopes that follow.
- **`next` only suggests calls that exist.** A suggestion made before the form
  exists would send an agent into a usage error. Today it points at
  `specr <spec> --task N`; the later scopes add the claim, activation and seal
  suggestions when they add the forms.
- **A package that does not validate is a successful call.** The question was
  whether it validates, and the answer is in `valid`. Exit 1 is for a call that
  could not do its job: the package could not be loaded, or `--list` could not
  read its spec root.
- **Readiness follows the format.** `(*TasksV2Json).IsReady` and `NextTask` are
  in `afspec`. A task's dependencies are its `depends_on` when present, an
  empty list meaning none, otherwise the task before it (§8.3); only `done`
  satisfies a dependency. `speca` uses both, `specr` shows `IsReady` on each task
  it lists and `examples/tasks` picks its task with `NextTask`, so "ready" and
  "next" mean one thing.
- **`speca` writes no file, makes no model or network call and reads no
  credential.** It runs in milliseconds on the files alone.

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
  writer's `tasks.json`, as they do with the library.
- A smaller binary. Like `specr`, `speca` links `internal/cli` and so the model
  stack (ADR 05, Consequences), and never uses it. Splitting the envelope out of
  that stack is still left for when it matters.

## Consequences

- `make build` installs three commands. `speca` is stamped the same way as the
  other two.
- The README's "no CLI surface" sentence is now true of changing a package, not
  of reading its state; it is corrected, and corrected again when the forms that
  change state arrive.
- `specr`'s tasks carry `ready`, the one change to its output. A field on the
  shared `TaskInfo` never means something different depending on which command
  wrote it.
- `speca --validate` and `speca --list` load every package of the spec root.
  For `--validate` that is the price of the cross-spec rules, which cannot be
  applied to one package alone; `--list` loads each package to say whether it
  validates and what its next task is.
