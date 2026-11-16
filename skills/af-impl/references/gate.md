# The gate

What `af-impl` runs to decide whether work may land, at which moments, and
how it reads the answer. The gate is the package's `test_commands.linter`,
then its `test_commands.all_tests`, run by `speca`, not by hand, while `speca`
is installed.

## The two levels

| When | Call |
|---|---|
| the baseline (§2), on the unchanged base branch | `speca <package> --gate full` |
| after every implement task (§5) | `speca <package> --gate task --base <commit>` |
| after the integration task (§5) | `speca <package> --gate full` |
| the clean-environment run of the conformance pass (§6) | `speca <package> --gate full --clean-env` |
| after the conformance fix commit, when there is one (§6) | `speca <package> --gate full` |
| on the merged tree, before the merge is committed (§7) | `speca <package> --gate full` |

`<commit>` is the commit the task started from: `git rev-parse HEAD` before
its claim, the last task's commit. When the package's `test_commands` name
`affected_tests`, the task gate runs the linter and that command from this
commit, the tests the task's change can affect, and not `all_tests`; the full
gate after the integration task runs the whole suite and catches a
regression outside them before anything is merged. Without `affected_tests`
the task gate runs `all_tests`, and `--base` changes nothing.

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
recorded pass answers `pass` with `reused` and runs nothing. It is a pass:
never run the linter or the suite by hand to confirm it. Report it as
"reused the gate of `<commit>`, `<date>`", from `reused.commit` (short) and
`reused.at`. A `gate_not_recorded` warning means the verdict is true and only
the reuse and the report miss this run; say so in the report.

## A test that fails and is not the change's

A gate that fails only in tests of code the change did not touch may have
met a flaky test. Do not run the gate or the suite again to find out: run
each failing test alone, once, in the suite's targeted form
(`go test ./pkg -run Name`).

- One that fails alone is a real failure, and the verdict stands.
- When every one of them passes alone, run the gate once more. A pass is the
  verdict, and each of those tests is flaky: name it, with the gate's `tail`,
  in the task's report and commit body. A second failure is a failed gate,
  whatever passes alone.

This never applies to a test the change wrote or to a test of a file it
touched: that failure is the change's until shown otherwise, and a test of
the branch's own that needs a rerun is fixed in the task, not rerun. At the
landing each flaky test not already tracked becomes one `defect` item of
severity `minor`, titled with the test's name, so that a repair makes it
deterministic once and no later gate meets it.

## The thorough target

The gate runs the spec's `test_commands` and nothing else. A more thorough
repository target, such as `make check` (formatting, vet, lint, tests), runs
**once**, on the merged tree, after its full gate and before the merge is
committed (§7). The gate has run the tests on that tree, or reused their
pass, so the thorough target is not what runs them again: where it is made of
steps (`check: fmt vet lint test`), run every step but the one that runs the
gate's `all_tests` (`make fmt vet lint`); run the whole target only when it
cannot be taken apart, or its test step runs more than `all_tests` does. This order takes precedence over a project instruction to
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
