# The survey brief

What `af-impl` §3 produces in one read-only pass over the spec and the
repository, before any branch exists. Every task is given this brief, so it is
written once and read at every step that needs it.

- **Where things live.** For each module, type, command and file the spec
  names: the file it is in, its callers, its tests. Where the spec expects
  something that does not exist, where it should be created and what it
  should look like to fit.
- **Conventions.** The test framework and where tests live, how errors are
  handled, how packages are wired, what the linter enforces, what the
  Makefile does. Read a neighbouring test file rather than guessing a style.
- **Where the tests go.** For each test the spec names, the production area it
  exercises and that area's existing test file, or where one would be created;
  the existing tests that already assert part of what it asks for, by file and
  name; and the repository's shared test helpers, which the steering file may
  name, so that no task copies one.
- **Drift.** Every place the spec's assumptions and the code disagree: what
  the spec assumes, what the code does, and the decision, numbered `D-1`,
  `D-2`, … and written in the past tense as it will read in the pull request
  ("Followed the code: …"). Prefer the code's reality when the spec is merely
  wrong about a detail; prefer the spec's intent when the code is what the
  spec changes.
- **What the tasks do not say.** Per task, what a coder needs to know that
  `steps` and `touches` leave out.
