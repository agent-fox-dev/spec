# 28. Size a package by the time it takes to land

**Status:** accepted
**Date:** 2026-11-15
**Related:** [ADR 19](19-migrate-the-format-to-version-3.md), [ADR 23](23-drive-the-implementation-loop-with-specrun.md)

## Context

The format allowed a package up to 10 requirements and 12 tasks before the
validator warned. Those limits came from v2 and were never set against what
a package costs to implement.

specrun's ledger of `parley` measures that cost. Across 21 packages landed by
`af-impl`, wall time grows by about 12.4 minutes per task:

| Package | Tasks | Requirements | Minutes |
|---|---|---|---|
| `01_hub_skeleton` | 3 | 5 | 30 |
| `07_workspace_access_rule` | 5 | 7 | 35 |
| `16_task_fork_lifetime` | 7 | 7 | 100 |
| `18_git_push_rules` | 9 | 9 | 131 |
| `20_scm_read_api` | 12 | 10 | 187 |

Almost every package was written up to the old requirement limit. 15 of the
21 hold 9 or 10 requirements and 8 to 12 tasks, and most of those took one
to two hours to land. `20_scm_read_api`, at the task limit, took over 3 hours.

Within a package, the tasks run one after another. Each task's time is about
three quarters model turns and a quarter gates and tests, so a faster gate
does not change the shape. A package lands once, at the end. Until then,
nothing it built is on the base branch. A stop late in a long package, such as
a base branch rewritten in the third hour, puts the whole run at risk. The
conformance review also reads the whole diff, and the diff grows with the
package.

## Decision

- **A package holds at most 6 requirements and at most 6 tasks.** At about
  12 minutes a task, plus the integration task, a package lands in about
  an hour and a half. The validator's `scope_limit` warning fires past 6 of
  either. The limits of 8 criteria per requirement and 12 steps per task are
  unchanged, and so is the `lite` profile's 3 requirements.
- **The split comes earlier.** The PRD phase of `spec`, `af-spec` and the
  PRD guidance of `af-prd` split work that exceeds 6 requirements, or that
  has two or more areas that could be built and tested on their own. Before,
  the threshold was three such areas. The prompts and the skills say why the
  limit is what it is, so a model that weighs a borderline case weighs the
  landing time.
- **It stays a warning.** A package past the limits still validates, and
  `af-impl` still implements it. The packages already written to the old
  limits now carry `scope_limit` warnings and need no change.

## Consequences

- A feature becomes more packages. Each one adds its survey, conformance and
  landing, and specrun's reconcile step after it: work done once per package,
  not per task. The ledger shows no clear fixed cost (the fit's intercept is
  near zero), and per-task time rose with package size: 7 to 10 minutes in
  the two smallest packages, 15.6 in `20_scm_read_api`. So smaller packages
  should not cost more in total.
- More packages mean more `handoffs` between them, and more `af:awaits-*`
  items while a feature is half built.
- The limits are constants of `afspec/validate.go`. A repository whose tasks
  cost much more or less than 12 minutes would want them configurable. That
  is left until one does.
