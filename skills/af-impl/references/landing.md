# Landing the branch

What `af-impl` §7 does: the pull request and its body, an issue for every open
item the pull request lists, and the fallbacks when the forge tooling is
missing.

- Push the branch and open a pull request against the base branch, unless
  the repository's instructions or the user say the branch stays local:
  `issue --create-pr --head <branch> --base <base> --title <title>
  --body-file <file>` where `issue` is installed, the forge's own tooling
  otherwise. Write the body to a file outside the repository first. It
  holds: the open items first, if any, under the one heading
  `## Not complete: open items`; the spec's deviations and how the tasks
  handled them, grouped by the survey's kinds; one entry per task with its
  commit, files changed, test verdicts, the test each of its criteria maps
  to, and any test-first deviation; the
  verification, before and after, as measured; and the conformance findings
  and what was done about each. Every claim in it comes from something that
  ran. `result.ref` is the pull request (`acme/widgets!31`) and
  `result.pull_request.html_url` its URL; both are needed below.
- **Track the open items.** Every entry under `## Not complete: open items`
  gets an issue, so the gap is tracked after the pull request is merged,
  not left in a description nobody reads again. For each:
  1. Look for one that exists: `issue --list --state all --limit 100`,
     followed through its `next` while `result.incomplete` is true, and
     reuse an issue whose title is the one below (a run continued after a
     gap may already have opened it).
  2. Write its body to a file: what is missing and why, in the words of the
     entry; the ids it concerns, with each criterion's text from the render;
     the evidence (`file:line`, the check's output); the ADR that records
     the decision, when there is one; what would close it; and the references: the pull request by
     its URL, and the spec by its `NN_name`, its `spec_dir` and a link to
     that directory on the base branch, where it stays after the merge.
  3. `issue --create --title "<NN_name>: <what is missing>" --body-file
     <file> --dry-run`, then the same call without `--dry-run`. The title
     says the gap in under 60 characters. `result.issue.html_url` is the
     issue.

  Then put the links in the pull request: rewrite its body file so that each
  entry ends with its issue (`→ #42`, or the URL), and
  `issue <pr ref> --edit --body-file <file>`. Read `side_effects` to see
  every write was made. Entries that concern one criterion may share one
  issue; an entry that is fixed before the pull request goes up is not an
  open item and gets none.

  `issue` writes need a token: `GITHUB_TOKEN` or `GH_TOKEN` for GitHub,
  `GITLAB_TOKEN` for GitLab; the repository is the one of the origin
  remote. `go install github.com/agent-fox-dev/aflib/cmd/issue@latest`
  installs `issue`. Without it, use the forge's own tooling the same way
  (`gh issue create`, `glab issue create`, the forge tools of the session).
  An issue that cannot be opened at all, for a missing token or a refusal,
  stops nothing. The entry stays in the pull request with
  `(no issue: <the reason>)`, and the report gives the `issue --create`
  call that opens it.
- Do not merge it. Do not seal the spec, though `speca <package>` now
  suggests `speca <package> --status sealed`: a review that rejects the pull
  request may reopen a task, which a sealed spec cannot record. Name that
  call in the pull request instead, for after the merge.

If a push or the pull request fails, say exactly what failed and leave the
branch and commits in place.
