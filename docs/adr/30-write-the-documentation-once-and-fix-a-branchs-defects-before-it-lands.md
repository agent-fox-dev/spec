# 30. Write the documentation once and fix a branch's defects before it lands

**Status:** accepted
**Date:** 2026-11-16
**Related:** [ADR 22](22-run-the-gate-with-speca.md), [ADR 23](23-drive-the-implementation-loop-with-specrun.md), [ADR 28](28-size-a-package-by-the-time-it-takes-to-land.md)

## Context

One `specrun` run on `parley` (run `20261115T034226Z-08ad`) landed nine
packages in 27 hours. Its step files and the `result` events of its sessions
say where the time went:

| Step | Count | Minutes | Share |
|---|---|---|---|
| Implement, finished | 9 | 1,318 | 81% |
| Implement, still running | 1 | 84 | 5% |
| Repair | 21 | 215 | 13% |
| Reconcile | 10 | 13 | 1% |

Inside the implement sessions the model's API time was 1,005 of the 1,318
minutes, 76%, at about 100 output tokens a second in every package; 38% of
the 6.2 million output tokens were thinking. The other 24% was tools,
mostly test runs. In `25_optimistic_landing`, the slowest at 226 minutes,
the task subagents ran for 201 minutes, 8 to 21 minutes each, so no step of
the orchestrating session and no single task is where the time is.

Three things in the run cost time and bought nothing:

- **Repairs of defects known at the landing.** Eight of the nine landed
  packages were followed by repair sessions, two on average, ten minutes
  each. Package 25 filed four defects of its own branch at the landing and
  four repairs followed within minutes, 43 minutes in all. One took 21
  minutes because the suite ran 121.5 seconds against a budget of 120. Each
  repair is a session that starts from nothing, on code the implementing
  session had open.
- **Documentation patched task by task.** A task updates the reference
  documentation as its step 7. In package 25 the shell patches to
  `docs/architecture.md` came to 50 KB, more than twice those to the
  largest code file among the session's biggest commands.
- **Tests run again to no purpose.** The merged tree's full gate was reused
  from the branch tip, and the suite was then run on it directly as well,
  as part of the thorough target. Tests that flake under load were rerun
  with no rule for what a rerun may excuse.

## Decision

- **The reference documentation is written once per package.** No task
  writes it. Before the conformance pass, `af-impl` writes it from the whole
  diff as one `docs:` commit with a `Spec: <NN_name>, docs` trailer. It gets
  no gate of its own: the clean-environment full gate of the conformance
  pass runs on the tree that holds it. An ADR that records a task's
  deviation is still written in that task.
- **A defect of the branch is fixed in the conformance pass, not filed.**
  Whatever the review finds wrong in code the branch wrote goes into the fix
  commit, test first, whatever its severity. It becomes an open item only
  when its fix was tried and did not pass the gate, when it needs a person's
  decision, or when it is in code the branch does not own. A test-cost
  finding is answered the same way: the branch's slowest new tests are made
  faster first, and the item is filed only if the finding remains.
- **A reused gate is a pass, and the thorough target does not run the tests
  again.** On the merged tree the thorough target runs without the step the
  gate has answered for, where it can be taken apart.
- **A flaky test has a rule.** When a gate fails only in tests of code the
  change did not touch, each failing test is run alone, once. If all pass,
  the gate is run once more, a pass stands, and each such test is filed as a
  minor defect so that a repair makes it deterministic. It never excuses a
  test the change wrote or a test of a file it touched.

## Consequences

- These act on the repairs (13% of the run) and on part of the tool time.
  They do not act on the 76% that is the model writing: that share moves
  only with the speed of the output, the amount of thinking, or work done in
  parallel, none of which this decision changes.
- The conformance pass grows by the fixes it used to file. It runs with the
  code open, so a fix there should cost less than a repair session, but the
  run that measures it has not happened yet.
- Between the last task and the documentation commit, the branch's
  documentation is stale. Nothing reads it then: a task subagent is told not
  to orient itself by the reference documentation.
- The flaky-test rule files an issue per flaky test, so the first runs after
  it open repairs of their own. Each is paid once.
- A repository whose thorough target cannot be taken apart still runs the
  suite twice on the merged tree.
- Not done: letting a clean-environment pass answer for the ordinary full
  gate of the same tree. It would save one suite run per package, about two
  minutes on `parley`, and it changes what `speca` reuses.
