# The repair mode

What `{{invoke af-impl}} --repair <issue>` does: no package is implemented
and no task state moves. specrun starts this session for one open issue, a
defect or an untested item, to fix it on a branch of its own, merge that
branch into the base branch and close the issue. The defect is the scope:
the session may change the code of a package that landed long ago, and only
what the fix needs.

specrun judges the session by what it leaves, not by what it says: the base
branch checked out, the tree clean, and the issue either closed or open with
`af:repair-failed`. Anything else stops the loop.

## Reading the issue

Read the issue with `issue <ref>`: its body says what is wrong, the evidence
and what closes it. Refuse, and say why, an issue that is closed or that
carries neither `af:defect` nor `af:untested`: change nothing, label nothing.

The issue's package is the one its `af:from-<NN>` label names, under the spec
root or its archive. Read the package's `tasks.json` for its
`test_commands`, and the code the body names.

Start from the base branch, up to date with its remote and clean, and create
`fix/<issue>-<slug>`, the slug a few words of the title.

## A defect

1. Write the regression test first: the behaviour the issue describes, at the
   level where it lives, named for the behaviour, in the test file of the
   area it exercises, as [test-first.md](test-first.md) says. Run it on the
   unchanged code and keep the command and its output.
2. When it fails, fix the code until it passes, touching only what the fix
   needs. Commit as `fix: <subject>`, the body naming the issue, the test and
   its failure before the fix.
3. When it passes on the unchanged code, the defect is already gone: fix
   nothing, commit only the test as `test: <subject>`, the body saying the
   defect was already gone and the test is the evidence. The outcome is
   `already_fixed`.

## An untested item

Write the test the criterion is missing, on the same kind of branch, and run
it on the unchanged code.

- When it passes, commit it as `test: <subject>`, and land it as below with
  outcome `already_fixed`.
- When it fails, the item is a defect: relabel the issue
  (`issue <ref> --edit --add-label af:defect --add-label
  af:severity-correctness --remove-label af:untested`), comment with the
  failure (`issue <ref> --comment --body …`), and go on as the repair of that
  defect in this session, from step 2 of *A defect*.

## Landing

1. Pass the full gate for the issue's package, `speca <package> --gate full`,
   which runs its `test_commands`; when the package has none, run the
   repository's own most thorough suite. A gate that does not pass is a
   failed attempt.
2. Merge the fix branch into the base branch with `git merge --no-ff`, and
   pass the gate again on the merged tree. The merge commit message ends
   with the trailer

   ```
   AF-Run: {"repair":{"issue":<number>,"outcome":"fixed"}}
   ```

   with `already_fixed` for a defect that was already gone or an untested
   item whose test passed at once.
3. Push the base branch, and the fix branch.
4. Close the issue with `issue <ref> --close --body …`, the comment naming
   the merge commit and the test that is the evidence.

End on the base branch, checked out and clean.

## Failing

- When the test cannot reproduce the issue as it is described, for example
  because it names code that no longer exists, do not guess at another
  reading.
- When the defect is not fixed after two attempts, stop.

Either way: comment on the issue with what was tried and what each attempt
returned (`issue <ref> --comment --body …`), add `af:repair-failed`
(`issue <ref> --edit --add-label af:repair-failed`), and leave the issue
open. Discard the fix branch's uncommitted work, check out the base branch
and leave it clean, with nothing merged. A person takes the issue from
there; specrun never queues it again.

## Never, in this mode

- Implement a package, claim or move a task, or edit a spec package.
- Touch an issue other than the one you were given, short of relabelling it
  as *An untested item* says.
- Merge on a gate that did not pass, or weaken a test to make it pass.
- Leave the session on another branch, or with uncommitted work.
