# The conformance pass

What `af-impl` §6 measures over the whole diff, once every task has landed and
before anything is pushed: the checklist, what blocks, and how the `unmet` list
is built.

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
  helpers. Each is fixed, not listed.
- **Scope.** Any file outside the tasks' `touches`, the survey's decisions,
  documentation and the test files of the areas the tasks touch is a change
  the spec did not ask for: take it out, or list it as unmet.
- **The package.** `speca <package> --validate` still says `valid`, and
  `speca <package>` counts no task `pending` or `in_progress`.
  `git diff <base> -- <package>` holds `progress.json` and nothing else (a
  new one counts; `git status` shows it); anything else is a change to the
  spec, and it comes out.
- **A clean environment.** Run the gate once more with an empty `HOME`, no
  global git configuration and `GIT_TERMINAL_PROMPT=0`. A test that passes
  only on this machine's configuration (`git init` without `-b`, a commit
  with no configured identity, a credential a dialog supplied) is a finding.
- **Review.** Read the diff against the spec on a fresh footing — no task's
  report, and a new whole-package `specr <package>` render — and answer for every requirement and criterion of the done tasks,
  every path of the integration task and every survey decision, each criterion through the test its commit maps it to: `implemented`, `partial`, `missing` or `different`;
  `asserts_contract`, `weaker`, `tautological`, `no_assertions` or
  `missing`; `followed` or `not_followed`; cite `file:line` for each, and
  name every documentation statement the code contradicts.

`missing`, `different`, `tautological`, `no_assertions`, `not_followed`, a
contradicted document and a clean-environment failure are blocking. Fix
what can be fixed in one `fix: <subject>` commit with a `Spec: <NN_name>,
conformance` trailer, landed only on checks that still pass; declare what
cannot, with a recorded decision, as above. Documentation findings and files out of
scope can only be fixed. What remains is the `unmet` list, and the pull
request must say the work is not complete while it is non-empty.

The `unmet` list holds every **open item**: what the branch does not do that
the spec asks (a criterion `missing`, `partial` or `different`, a
deviation that leaves a criterion unsatisfied), and what the run could not
show (a check or test that did not run here, a contract no test asserts, a
finding left as it is). Each is one entry, with what is missing, why, the
ids it concerns (`REQ-3.2`, `PATH-1`, `D-3`) and its evidence
(`file:line`, the check's output). §7 opens an issue for each. A deviation
that is decided, recorded in an ADR (§5, step 8) and leaves every criterion met is
not an open item; it goes with the deviations.
