# The reconcile mode

What `{{invoke af-impl}} --reconcile <adopt file> --report <report file>` does: no
package is implemented. specrun starts this session every few packages and
at the end of a run, to bring every open item in line with the base branch.
It does only what needs judgement; specrun applies the rules that follow from
labels and git afterwards, and stops the loop if this session changed the
repository.

Read the two paths from the arguments. When either is missing, write what is
missing to the report path if there is one, say so, and stop.

## Adopting

The adopt file is a JSON array of `{number, title, url, package}`: open
issues titled `<NN_name>: …` that carry no `af:` label, opened before the
label scheme existed. For each:

1. Read the issue's body (`issue <url>`): it says what is missing, why, the
   evidence and what closes it.
2. Assign its kind, as the conformance pass does: `awaits` (a named later
   package must use or wire what the branch offers), `defect` (with a
   severity: `security`, `data-loss`, `correctness` or `minor`), `untested`,
   or `deviation`. An issue whose kind the body does not settle is a
   `deviation`, so that a person decides it.
3. Add its labels with `issue <ref> --edit --add-label`: `af:<kind>`,
   `af:from-<NN>` for the package of the entry, `af:awaits-<MM>` when it
   awaits a package, `af:severity-<s>` for a defect; and a comment saying the
   issue was adopted and why it got that kind.

Touch no issue that is not in the adopt file and carries no `af:` label.

## Checking closing conditions

List every open `af:*` issue (`issue --list --state open`). For each, decide
whether the condition its "What closes it" names holds on the base branch as
it is. Close those that hold with `issue <ref> --close --body`, the comment
giving the evidence: the production caller as `file:line`, and the test that
exercises it. Leave the others open.

Change no code, create no branch and commit nothing: specrun checks that the
base branch is checked out, the tree is clean and `HEAD` did not move.

## The report

Write Markdown to the report path:

- what was adopted, each issue with the kind it got;
- what was closed, each issue with its evidence;
- the rest by kind and severity, the deviations oldest first.
