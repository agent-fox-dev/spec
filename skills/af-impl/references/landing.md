# Landing the branch

What `af-impl` §7 does: the issue every open item gets, the merge into the
base branch and its commit message, and the fallbacks when the forge tooling
is missing.

- **Push the branch** to origin (`git push -u origin <branch>`), unless the
  repository's instructions or the user say the branch stays local. The
  issues below link to it, and it stays on the remote after the merge.
- **Track the open items** before merging, so the merge commit can carry
  their links. Every entry of the `unmet` list gets an issue, so the gap is
  tracked after the merge, not left in a commit message nobody reads again.
  For each:
  1. Look for one that exists: `issue --list --state all --limit 100`,
     followed through its `next` while `result.incomplete` is true, and
     reuse an issue whose title is the one below (a run continued after a
     gap may already have opened it).
  2. Write its body to a file outside the repository: what is missing and
     why, in the words of the entry; the ids it concerns, with each
     criterion's text from the render; the evidence (`file:line`, the
     check's output); the ADR that records the decision, when there is one;
     what would close it; and the references: the branch by name and by its
     URL on the forge, and the spec by its `NN_name`, its `spec_dir` and a
     link to that directory on the base branch, where it is after the merge.
  3. `issue --create --title "<NN_name>: <what is missing>" --body-file
     <file> --label af:<kind> --label af:from-<NN> [...] --dry-run`, then the
     same call without `--dry-run`. The labels are `af:<kind>` and
     `af:from-<NN>`, this package; for `awaits` also `af:awaits-<MM>`, the
     package that must take it up, and for a `defect` its
     `af:severity-<security|data-loss|correctness|minor>`. A label the
     repository lacks is created first, with `issue --create-label
     <name>`. The title says the gap in under 60 characters.
     `result.issue.html_url` is the issue. The body's "What closes it"
     names, for `awaits`, the receiving package by id.

  Entries that concern one criterion may share one issue; an entry that is
  fixed before the merge is not an open item and gets none.

  `issue` writes need a token: `GITHUB_TOKEN` or `GH_TOKEN` for GitHub,
  `GITLAB_TOKEN` for GitLab; the repository is the one of the origin
  remote. `go install github.com/agent-fox-dev/aflib/cmd/issue@latest`
  installs `issue`. Without it, use the forge's own tooling the same way
  (`gh issue create`, `glab issue create`, the forge tools of the session).
  An issue that cannot be opened at all, for a missing token or a refusal,
  stops nothing. The entry stays in the merge commit with
  `(no issue: <the reason>)`, and the report gives the `issue --create`
  call that opens it.
- **Write the merge commit message** to a file outside the repository. Its
  subject is `feat: implement <NN_name>`. Its body holds: the open items
  first, if any, under the one heading `## Not complete: open items`, each
  ending with its issue (`→ #42`, or the URL); the spec's deviations and how
  the tasks handled them, grouped by the survey's kinds; one entry per task
  with its commit, files changed, test verdicts, the test each of its
  criteria maps to, and any test-first deviation; the verification, before
  and after, as measured, with the gate report: the full gate's seconds at
  the baseline and on the merged tree, the slowest units, and the full and
  task runs with how many were reused, a reused gate written as "reused the
  gate of `<commit>`, `<date>`"; and the conformance findings and what was done
  about each. Every claim in it comes from something that ran. It ends with
  a `Spec: <NN_name>` trailer.
- **Merge.** Switch to the base branch and bring it up to date with
  `git pull --ff-only`. Then `git merge --no-ff --no-commit <branch>`. A
  conflict stops the landing: `git merge --abort`, stay on the base branch,
  and report the conflicting files. Without a conflict, run
  `speca <package> --gate full` on the merged tree (often `reused` from the
  branch tip's full gate), then the repository's thorough target, such as
  `make check`, where it has one ([gate.md](gate.md)). Then
  `speca <package> --gate-report --since <baseline commit>`, and put its
  numbers in the message's verification section. Only a landable verdict
  from the §5 table, for the gate and the thorough target, commits the
  merge, with `git commit -F <message file>`; any other verdict is
  `git merge --abort` and a report of what the checks returned, with the
  branch left as it is.
- **Push the base branch** (`git push origin <base>`), unless the branch
  stays local. A rejected push is never forced: leave the merge commit in
  place and report the rejection. Stay on the base branch for the next
  task.
- Do not seal the spec, though `speca <package>` now suggests
  `speca <package> --status sealed`: the lifecycle belongs to the spec's
  owner. Name that call in the report.

If a push or the merge fails, say exactly what failed and leave the branch
and its commits in place.
