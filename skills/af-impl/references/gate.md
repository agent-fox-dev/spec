# The gate

What `af-impl` runs to decide whether work may land, at which moments, and
how it reads the answer. The gate is the package's `test_commands.linter`,
then its `test_commands.all_tests`, run by `speca`, not by hand, while `speca`
is installed.

## The two levels

| When | Call |
|---|---|
| the baseline (§2), on the unchanged base branch | `speca <package> --gate full` |
| after every implement task (§5) | `speca <package> --gate task` |
| after the integration task (§5) | `speca <package> --gate full` |
| the clean-environment run of the conformance pass (§6) | `speca <package> --gate full --clean-env` |
| after the conformance fix commit, when there is one (§6) | `speca <package> --gate full` |
| on the merged tree, before the merge is committed (§7) | `speca <package> --gate full` |

Note the commit the baseline ran on (`git rev-parse HEAD` just before it): the
gate report of §7 counts from it, and §4 does not change it when a branch is
continued.

## Reading the verdict

`result.gate.verdict` is the answer, and the exit code is 0 for every verdict.
`pass` is green, `fail` is red, and `not_run` is "a check could not run": its
`reason` names the command and why. The verdict table of §5 applies to both
levels, each gate compared with the baseline.

Read a failure from the failing command's `tail`, its last 40 lines. Open its
`log` only when the tail does not explain the failure, and quote what explains
it, not the log.

A gate whose tree, directory, level, commands and environment match a
recorded pass answers `pass` with `reused` and runs nothing. It is a pass. Report it as
"reused the gate of `<commit>`, `<date>`", from `reused.commit` (short) and
`reused.at`. A `gate_not_recorded` warning means the verdict is true and only
the reuse and the report miss this run; say so in the report.

## The thorough target

The gate runs the spec's `test_commands` and nothing else. A more thorough
repository target, such as `make check` (formatting, vet, lint, tests), runs
**once**, on the merged tree, after its full gate and before the merge is
committed (§7). This order takes precedence over a project instruction to
substitute the thorough target for every check: formatting and tidiness cannot
change a test's result, and the gate on the merged tree decides what lands. A
thorough target that fails on the merged tree is a verdict that is not
landable, like a failing gate.

## The report

Before committing the merge, run
`speca <package> --gate-report --since <baseline commit>` and copy from
`result.gate_report` into the merge commit's verification section and into the
report of §8: the full gate's seconds at the baseline (`baseline_full_seconds`)
and on the merged tree (`last_full_seconds`), the slowest units (`slowest`),
and the full and task runs with how many were reused.

Without `speca`, run `linter` and `all_tests` by hand at the same moments, as
[speca-specr.md](speca-specr.md) says, and say so in the report.
