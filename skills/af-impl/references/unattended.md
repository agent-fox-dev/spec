# The unattended mode

What `{{invoke af-impl}} <package> --unattended` changes: each implement task
of §5 runs in a subagent with a fresh context, so that a turn late in the
package costs what a turn early in it did. specrun asks for it, since nobody
watches its sessions. Without the argument, af-impl runs as it always does,
in one session, for a person watching.

Only where a task's work happens moves. This session, the orchestrating
one, still surveys, claims, records, commits, runs the integration task, the
conformance pass and the landing. Only the implement tasks run in subagents,
one at a time, in this working tree: never in an isolated worktree, whose
work this session's commit would not hold.

## The brief

The survey (§3) writes its brief to
`${TMPDIR:-/tmp}/af-impl-<NN>-brief.md`, outside the repository, and keeps it
within 600 lines. Detail that only one task needs goes into that task's entry
of the brief, not the general part. §3's "show the brief" is this file: say
where it is and what it decides, instead of putting it all in this session's
context. A subagent reads the brief, never this session's transcript, so the
brief says everything a task needs that the scoped render and the code do
not.

What a later task needs to know from an earlier one goes into the earlier
task's commit body, where §5 already puts it: the test each criterion maps
to, a deviation, a decision. The next subagent is pointed at those commits.

## What this session does for each task

1. Claim it: `speca <package> --task <N> --state in_progress`.
2. Render it scoped with `specr <package> --task <N> --markdown-file …`, as
   §5 says.
3. Start one subagent and give it: the path of the brief, the path of the
   scoped render, the files the task `touches`, the progress lines of the
   tasks so far with their commits, and the commit the task starts from;
   and the paths of this skill's `SKILL.md` (§5 and *Never*),
   `references/test-first.md` and `references/gate.md`, which it reads in
   full, since it has none of this session's context. Tell it what it does
   and returns (below).
4. When it returns, check what §5 checks before a task lands: the gate
   verdict against the table of §5, run again here when in doubt; a report
   that answers for every check of the task's criteria and every
   `done_when` entry; and, with `git status` and `git diff`, that it
   changed nothing in the package but, in a package that requires evidence,
   the task's evidence in `progress.json`. On a landable verdict, record the task
   done with `speca <package> --task <N> --state done`, check that the
   package's change is `progress.json` alone, and commit the work and the
   state as one commit, with the subject and body the subagent returned and
   the trailers §5 asks for.
5. A subagent that returns no landable verdict, or nothing, is a failed
   attempt, handled as §5 says: discard, claim again, and start one more
   subagent with the failure in view; a second failure is parked.

Then write the task's progress line, as §5 says, with the commit this
session made, and start the next task in the same turn. A subagent's return
is not a stop.

## What the subagent does and returns

It does steps 3 to 8 of §5 for its one task (orient, test first, implement,
check narrowly, documentation, never the spec), then runs the gate,
`speca <package> --gate task --base <commit>`. It reads the brief and the
earlier tasks' commits, never the orchestrator's transcript.

It does not claim, record or commit, and starts no other task. In a package
that requires evidence it records the failing runs itself, with
`speca <package> --task <N> --evidence <file>` before it implements
(test-first.md): the one write of the package it makes. It returns:

- what the task did, in a line, for the progress line this session writes
  once it has committed;
- the commit's subject and body, the body listing each criterion the task
  owns against the test that covers it, the tests it watched fail first and
  every deviation, with its trailer;
- the gate verdict, with what each command returned, and how each check of
  the task's criteria and each `done_when` entry was answered;
- the criterion-to-test map: in a package that requires evidence, the path
  of the evidence file it recorded.
