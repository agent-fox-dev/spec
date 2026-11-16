# The conformance pass

What `af-impl` §6 does once every task has landed and before anything is
pushed: the documentation, written once; then what it measures over the whole
diff, what blocks, what is fixed here and not filed, and how the `unmet` list
is built.

## The documentation, once

No task wrote reference documentation (§5, step 7): a document patched after
every task is rewritten as often as the package has tasks. Write it now, from
the finished code, before measuring:

- Take the names the whole diff adds, alters or removes
  (`git diff <base>...HEAD`), and search the reference documentation for
  them; the steering file's documentation table, if there is one, names the
  files. Do not read the documents through.
- Update what the branch makes wrong and add what it makes missing. Every
  quoted string, field name, exit code and sample payload is copied from the
  code or a test, not from the PRD, and you can say which line it came from.
  When the code does not do what the spec asks, the docs say what the code
  does.
- Regenerate the generated documentation where the repository has a target
  for it and the diff changes what it is generated from.
- Commit it as one `docs: <subject>` commit with a `Spec: <NN_name>, docs`
  trailer. It gets no gate of its own: the clean-environment full gate below
  runs on the tree that holds it.

With `--unattended` this session writes it, not a subagent. An ADR that
records a task's deviation is still written in that task (§5, step 8). A
branch being continued that already holds this commit keeps it, and a second
one adds only what it lacks.

## What is measured

Every task was verified by the checks; before anything is pushed, ask what
the checks cannot: does the finished branch do what the spec says? Measure
the whole diff from the base branch:

- **Structure.** The project's formatter and vet over the touched files;
  duplication of code that exists elsewhere; an error discarded under a
  comment that says it is logged; a comment deferring to "a later task"; an
  unexported declaration nothing uses; a function the change wrote or grew
  past a hundred lines; a test with no assertion.
- **Tests.** A spec, requirement, criterion or path id in a test name, a test
  file name or a comment; a test file named after the spec, a task or a check
  kind; two
  tests that assert the same behaviour, or variants of one behaviour that
  are not one table; a test of an artefact rather than a behaviour (that a
  struct has a field, a constant has a value, a file mentions something, an
  import graph holds, a helper exists); a helper copied from the shared test
  helpers; a new test that runs serially without a stated reason, or
  rebuilds a fixture the shared helpers already build once. Each is fixed,
  not listed.
- **Test cost.** Run `speca <package> --gate-report --since <baseline
  commit>`. When `result.gate_report.test_cost.finding` is true (the last
  full gate exceeded the budget, or grew by more than half over the base
  branch's tree), make the slowest tests this branch added faster, from
  `slowest`, in the fix commit: a shared fixture, a shorter timeout passed
  as an option, a table in place of repeated set-up. Never by deleting or
  weakening a test. Only when the finding is still true after the fix
  commit's full gate, record a `test-cost` open item of kind `defect`,
  severity `minor`, its evidence the report's numbers: `budget_seconds`,
  `exceeded`, `grew_by`, `last_full_seconds` and `slowest`.
- **Scope.** Any file outside the tasks' `touches`, the survey's decisions,
  documentation and the test files of the areas the tasks touch is a change
  the spec did not ask for: take it out, or list it as unmet.
- **The package.** `speca <package> --validate` still says `valid`, and
  `speca <package>` counts no task `pending` or `in_progress`.
  `git diff <base> -- <package>` holds `progress.json` and nothing else (a
  new one counts; `git status` shows it); anything else is a change to the
  spec, and it comes out.
- **A clean environment.** Run `speca <package> --gate full --clean-env`: the
  gate once more with an empty `HOME`, no global git configuration and
  `GIT_TERMINAL_PROMPT=0`. A test that passes only on this machine's
  configuration (`git init` without `-b`, a commit with no configured
  identity, a credential a dialog supplied) is a finding; read it from the
  failing command's `tail`.
- **Review.** Read the diff against the spec on a fresh footing — no task's
  report, and a new whole-package `specr <package>` render — and answer for every requirement and criterion of the done tasks,
  every path of the integration task and every survey decision, each criterion through the test its commit maps it to: `implemented`, `partial`, `missing` or `different`;
  `asserts_contract`, `weaker`, `tautological`, `no_assertions` or
  `missing`; `followed` or `not_followed`; cite `file:line` for each, and
  name every documentation statement the code contradicts, found by
  searching the reference documentation for the names the diff adds, alters
  or removes, not by reading it through.

`missing`, `different`, `tautological`, `no_assertions`, `not_followed`, a
contradicted document and a clean-environment failure are blocking. Fix
what can be fixed in one `fix: <subject>` commit with a `Spec: <NN_name>,
conformance` trailer, landed only on a `speca <package> --gate full` that
still passes (and `--gate full --clean-env`, when that run found it); declare what
cannot, with a recorded decision, as above.

**A defect of this branch is fixed here, not filed.** Whatever the review
finds wrong in code this branch wrote (a race, lost data, a wrong result, an
exposure), blocking or not and whatever its severity, goes into the fix
commit, its regression test written and seen to fail first. An issue filed
for it instead is repaired later by a session that starts from nothing, on
code this session has open now. A defect becomes an open item only when:

- its fix was tried and the gate did not pass with it: take the attempt out,
  and say in the entry what was tried and what the check returned;
- the fix needs a decision that is a person's (two readings of the spec,
  each changing the behaviour), or belongs to code outside the branch's
  diff, which this package does not own;
- it is a flaky test of code the branch did not touch
  ([gate.md](gate.md)). Documentation findings and files out of
scope can only be fixed. What remains is the `unmet` list, and the pull
request must say the work is not complete while it is non-empty.

The `unmet` list holds every **open item**: what the branch does not do that
the spec asks (a criterion `missing`, `partial` or `different`, a
deviation that leaves a criterion unsatisfied), and what the run could not
show (a check or test that did not run here, a contract no test asserts, a
finding left as it is). Each is one entry, with what is missing, why, the
ids it concerns (`REQ-3.2`, `PATH-1`, `D-3`) and its evidence
(`file:line`, the check's output), and exactly one **kind**:

- `awaits`: the branch offers something that a named later package must use
  or wire. It is not a fault of this package; the entry names that package.
- `defect`: code this branch delivered behaves differently from what its
  spec asks, or loses data, races, or exposes something, and could not be
  fixed here (above); or a test of the repository is flaky. It carries one
  **severity**: `security`, `data-loss`, `correctness` or `minor`.
- `untested`: the behaviour is believed to work, but a criterion has no test
  that asserts it.
- `deviation`: the code deliberately does something other than the spec,
  recorded in an ADR, and a person must accept it or change the spec.

§7 opens an issue for each, labelled by its kind. A deviation
that is decided, recorded in an ADR (§5, step 8) and leaves every criterion met is
not an open item; it goes with the deviations.
