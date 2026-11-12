# The `specrun` command

```
specrun [--max N] [--stall D] [--log-dir DIR] [--strict-skills]   implement every open package, one session each
specrun --dry-run                                                 the next step, and nothing changed
specrun --ledger                                                  every step so far, as a table
specrun --reset-state                                             discard the state file
```

`specrun` is the implementation loop. It runs `/af-impl` on one spec package
after another, each in a fresh headless Claude Code session, until every
package is done, a step stops, or `--max` packages have landed. It chooses the
next package with the same code as `speca --next` (`afspec.ChooseNext`),
starts and watches the session, and then checks from the repository that the
package landed instead of trusting the session's account. It keeps its state
in one file outside the repository, so that it resumes after a crash, ends a
session that has stopped making progress, and keeps every session's full
stream and a ledger of what each step took. The decision is
[ADR 23](adr/23-drive-the-implementation-loop-with-specrun.md).

It runs in the repository it is started in (the git work tree of the working
directory), with the spec root `afspec` resolves there (`.specs`, or
`$AF_SPEC_DIR`). It writes one JSON envelope to stdout when it ends, through
`aflib/envelope`, whose JSON is the envelope `spec`, `specr` and `speca`
write, and one line per step to stderr as it goes. It calls no model API: every session is a
`claude -p` process, and the command links nothing of `agentkit-go`.

```sh
specrun                         # implement every open package, in dependency order
specrun --max 1                 # land one package, then stop
specrun --dry-run               # which package would be next, or why none can start
specrun --stall 45m             # end a session that writes nothing for 45 minutes
specrun --ledger                # what every step took
specrun --reset-state           # forget the step that was running
```

`--help` prints the help to stderr, `--version` the build identity and
`--ledger` the ledger table to stdout; none of the three writes an envelope.

## The forge

`specrun` reaches the forge of the repository's origin remote, GitHub or
GitLab, through `aflib/issuex`, with the token the `issue` command reads
(`GITHUB_TOKEN` or `GH_TOKEN`, `GITLAB_TOKEN`): the sessions open their open
items as issues with the same token, and the label rules decide what the
loop does next. Without a client (no token, no origin remote naming a GitHub
or GitLab repository, a forge that is not supported) a run refuses to start
at stage `preflight`, naming the variables or the remote. A dry run goes on
with a low `forge_unavailable` warning, and writes nothing to a forge in any
case.

Before its first step, once the base branch is pulled, a run creates every
label of the scheme the repository lacks; one that exists is left as it is
(`issuex` lists no labels, so a conflict on creating one is how it is found),
and a label that cannot be created otherwise refuses the run at stage
`preflight`:

| Label | Meaning |
|---|---|
| `af:awaits`, `af:defect`, `af:untested`, `af:deviation` | the kind of an open item; an issue carries exactly one |
| `af:severity-security`, `af:severity-data-loss`, `af:severity-correctness`, `af:severity-minor` | the severity of a `defect` |
| `af:accepted`, `af:rejected`, `af:repair-failed` | what a person decided about a deviation, and a repair that did not land |
| `af:from-<NN>`, `af:awaits-<NN>` | for every package under the spec root and `archive/` that loads: the package that opened an item, and the package an `awaits` item waits for |

## The loop

Each step is **implement the next package**:

1. Check out the base branch and pull it with `--ff-only`. The base branch is
   the branch `refs/remotes/origin/HEAD` names, else `main`.
2. Ask `afspec` for the next package: the first open package in dependency
   order, ties broken by number, whose every dependency is finished.
3. Start the session in the repository:

   ```
   claude -p "/af-impl <spec_dir>" --output-format stream-json --verbose \
     --permission-mode bypassPermissions --disallowedTools ScheduleWakeup,CronCreate
   ```

   in a process group of its own, with `CLAUDE_CODE_PRINT_BG_WAIT_CEILING_MS`
   set to the stall time in milliseconds, so that a session never waits on its
   subagents longer than `specrun` waits on the session. Timers are disallowed:
   a pending one would keep a finished headless session alive.
4. When the session ends, **whatever its exit code**, check that the package
   landed:

   | Check | Reason when it fails |
   |---|---|
   | the base branch is checked out | `not on the base branch` |
   | the working tree is clean | `working tree not clean` |
   | `HEAD` is not the commit it was before the step | `HEAD did not move` |
   | the package has no task `pending` or `in_progress`, read through `afspec` | `tasks still open` |

   The first check that fails stops the loop with exit code 1, and
   `result.stopped.reason` names it.

The loop ends with exit code 0 when no package is open (`all_done`), or once
`--max` packages have landed in this run. When packages are open and none can
start, because each waits on a dependency that is not finished or does not
load, it stops with exit code 1 at stage `waiting`, and `result.waiting` gives
each package's reason as `speca --next` words it; when the dependencies form a
cycle it stops at stage `graph`, naming the cycle. A base branch that cannot be
checked out or pulled, and a missing `git` or `claude`, stop it at stage
`preflight` before any session.

### The check after a landing

When a package has landed, `specrun` lists its open issues: those labelled
`af:from-<NN>`, and those whose title starts with the package's directory
name and a colon (`01_cache: …`), as af-impl titles an open item. Each must
carry exactly one kind, `af:from-<NN>`, for a `defect` exactly one severity,
and for `awaits` an `af:awaits-<MM>`. The rule `incomplete-labels` makes an
issue that lacks any of these a `deviation`: its other kind labels are
removed, `af:deviation` and, when missing, `af:from-<NN>` are added, and a
comment names the rule, what was missing and the commit of the base branch,
so that a person decides it:

```
specrun, rule incomplete-labels, on 4b1f0c2…: it has no kind; it lacks af:from-01. It is now af:deviation, for a person to decide.
```

A complete issue, an issue of another package and a closed issue are not
touched, and the rule changes nothing the second time it runs. The check is
owed in the state (`label_check`) from the landing until it is done, and a
change's comment is owed (`pending_comments`) from before its labels change
until it is posted. A forge call that fails stops the loop at stage `forge`,
after the package's ledger line is written with outcome `landed`, and a crash
leaves the same: the next start posts the owed comments and applies the rule
again before it chooses a step. A landing the next start finds in the
repository (see *State and resume*) gets its trailer read and its issues
checked the same way. A package that has gone under `archive/` since is
checked there; one found nowhere is left unchecked, with a note.

### Items that await a finished package

After the check above, `specrun` lists every open issue labelled
`af:awaits`, whatever package opened it. One whose `af:awaits-<NN>` names a
package that is finished (sealed, superseded or archived, under `archive/`,
or with no task pending or in progress, as `afspec`'s `IsFinished` says), or
names no package at all, will never be taken up: the rule `awaits-finished`
removes `af:awaits` and `af:awaits-<NN>`, adds `af:defect` and
`af:severity-correctness`, and comments, naming the package and the commit of
the base branch. An item awaiting a package that is still open, or that is there
but does not load (whether it is finished is not known), a closed issue, and
one that is not `af:awaits` are not touched; the rule changes
nothing the second time, and its forge failures stop the loop as the other
rule's do.

### Stopping on a serious defect

After a package lands and the label rules ran, `specrun` lists its open
issues labelled `af:defect` and `af:from-<NN>`. When one carries a severity in
`--stop-on` (default `security,data-loss`), the loop stops with exit code 1 at
stage `defect`: `result.stopped.reason` is `serious defect`, and
`result.stopped.issues` names each issue (`number`, `url`, `title`,
`severity`); the package's ledger line, outcome `landed`, holds them too. A
deviation, an untested item and a defect of another severity never stop it,
and `--stop-on ''`, or `SPECRUN_STOP_ON` set to the empty string, stops on
none. A stop at the next start reports the same `result.stopped` as the one
after the landing.

The state records the last package that landed (`last_landed`), and every
start makes the same check for it before it chooses a step, so starting
`specrun` again is not the way past a stop. To go on, a person closes the
issue, changes its severity label, or runs with a `--stop-on` that no longer
holds it.

## The reconcile

A reconcile is a step of the loop that brings every open item in line with
the base branch: a session for what needs judgement, rules for the rest. It
runs after every `--reconcile-every` landed packages (default 5), before the
next package, and once more when every package is done, unless a reconcile
was the last step and no package landed since (on a repository whose ledger
records no reconcile, that is one session even when nothing was open,
[docs/errata/20_specrun_driver.md](errata/20_specrun_driver.md)). The state
counts the packages since the last one (`since_reconcile`), so a restart
continues the count, and a reconcile recorded as running is run again first.
`--reconcile-every 0` leaves only the one at the end.

**The session.** `specrun` selects the issues the session may adopt: the
open issues with no label starting `af:` whose title starts with the
directory name of a package under the spec root or its archive and a colon,
as af-impl titled an open item before the label scheme. It writes them as a
JSON array of `{number, title, url, package}` to `<step>-adopt.json` in the
log directory (`[]` when there are none) and starts

```
claude -p "/af-impl --reconcile <log dir>/<step>-adopt.json --report <log dir>/<step>-report.md" …
```

with the flags, process group, stream files and stall of every session. The
session adopts the issues of the file (their kind and labels), closes the
`af:*` issues whose closing condition holds on the base branch, and writes the
report; it changes no code. When it ends, the base branch must be checked out,
the tree clean and `HEAD` where it was: a session that changed the repository
stops the loop (outcome `stopped`, or `stalled`), and no rule is applied. A
missing report is a low `reconcile_report_missing` warning.

**The rules**, each change with a comment naming the rule and the commit of
the base branch:

| Rule | What it does |
|---|---|
| `adoption-unsettled` | an issue of the adopt file still open with no kind gets `af:deviation` and `af:from-<NN>` of the package its title names |
| `deviation-accepted` | an open `af:deviation` with `af:accepted` (and not `af:rejected`) is closed, the comment naming the first `docs/adr/….md` its body cites, or saying it cites none |
| `deviation-rejected` | an open `af:deviation` with `af:rejected` (and not `af:accepted`) loses both labels and gets `af:defect` and `af:severity-correctness` |
| `incomplete-labels`, `awaits-finished` | as after a landing, for every package under the spec root and its archive |

A deviation with both `af:accepted` and `af:rejected` is nobody's decision: it
is left as it is, with a low `deviation_conflict` warning naming it. A person
settles a deviation where it is tracked, by adding `af:accepted` or
`af:rejected`; the next reconcile does the rest.

The reconcile's ledger line has kind `reconcile`, outcome `reconciled`, and
`adopted` (issues of the adopt file that the session gave a kind), `closed`
(issues open before the step and closed after it) and `settled` (deviations
accepted or rejected). `result.open_deviations` lists the open deviations
nobody has settled (`number`, `title`, `url`, `created_at`), oldest first,
and stderr prints them. A dry run names a due reconcile as its next step
(`next_step.kind` `reconcile`), lists the issues it would hand over in
`result.would_adopt`, and adds the deviation rules' changes to
`result.would_change`.

## What it runs

At the start of a run `specrun` logs on stderr, and puts in
`result.versions`: its own version; the af-impl `SKILL.md` a session in this
repository loads (`<repository>/.claude/skills/af-impl/SKILL.md` when it
exists, else `~/.claude/skills/af-impl/SKILL.md`) with its SHA-256; the hash
it was built with; and what `claude --version` and `issue --version` print. An
`issue` that is not installed is `not installed`, and stops nothing.

`make build` embeds the SHA-256 of the af-impl `SKILL.md` as
`internal/skillgen` renders it for Claude Code from the tree being built
(`go run ./internal/skillgen --source skills --sha256 af-impl`), which is what
`make install-skills` writes. When the installed skill's hash differs from it
the run warns `skill_mismatch`; when no af-impl skill is installed,
`skill_not_installed`; the run goes on. With `--strict-skills` either, and a
`specrun` built with no embedded hash (`go build`, not `make build`; its
`built_skill_sha256` is `unknown`), refuses to start at stage `preflight`.

## State and resume

The state is one JSON file, `agent-fox/specrun/<repository name>.json` under
the user's state directory (`$XDG_STATE_HOME`, else `~/.local/state`), written
through a temporary file that is renamed after every change. It holds the
steps that landed and the step running, with its package, when it started, the
commit `HEAD` was at and the process id of `specrun`:

```json
{
  "done": [
    { "kind": "implement", "subject": "01_cache", "ended_at": "2026-11-12T18:00:47Z" }
  ],
  "running": {
    "kind": "implement",
    "subject": "02_search",
    "spec_dir": ".specs/02_search",
    "started_at": "2026-11-12T18:00:47Z",
    "head": "4b1f0c2…",
    "pid": 0
  }
}
```

A step that stops or stalls stays as `running`, its `pid` 0, so that the next
run takes it up. From its first step to its end, a loop also records itself as
`owner`, so that a second `specrun` is refused between steps too. Started again, after a stop, a crash, a kill or a reboot,
`specrun` reads the file and, once the base branch is up to date, checks the
running step against the repository before it chooses one:

- a package with no open task, whose base branch moved past the recorded
  commit, landed: its ledger line is written with outcome `landed` and no
  session starts for it (it is not counted in this run's `landed`, nor against
  `--max`);
- a package that still exists is run again: af-impl continues its branch
  `feature/<NN>-<spec_name>` when there is one;
- a package that no longer exists is dropped, and the next one is chosen.

A state file that cannot be parsed refuses the start at stage `state`, naming
the file and `--reset-state`; so does one whose running step, or whose
`owner`, records a process that is alive and is a `specrun` (`ps` names it),
since two loops would switch branches under each other. A recorded process
that has ended, or whose id now belongs to another program, as after a reboot,
refuses nothing. The file is left as it is.

The state file and the default log directory are named after the repository's
directory, as the spec asks: two clones whose directories have the same name
share them, and should each be given `--log-dir`, and run one at a time. `specrun --reset-state` removes it and does nothing
else; with no file it says so.

## Watching a session

`specrun` reads the session's stdout line by line as it arrives. When no line
arrives for `--stall` (default 30 minutes), it ends the session's process
group, stops reading its output (even when a process that left the group still
holds it open), records the step as `stalled`, and stops the loop with exit
code 1 at stage `stall`. An interrupt or a termination of `specrun` (Ctrl-C,
`SIGTERM`) ends the session's process group the same way, records the step as
`stopped` with the reason `interrupted`, and stops at stage `interrupt`; the
step is taken up by the next run. The session's exit code decides nothing: the
landing checks do. What a session leaves running when it exits is ended with
its group.

## Logs and the ledger

The log directory is `--log-dir`, else `agent-fox/specrun/<repository name>`
under the user's cache directory (`$XDG_CACHE_HOME`, else the platform's:
`~/Library/Caches` on macOS, `~/.cache` on Linux). A directory inside the
repository's working tree is refused as a usage error, exit code 2, since
af-impl refuses a dirty tree; one that cannot be created or written stops the
run at stage `preflight`.

- Each session's stream is saved, every line in order, as `<step>.jsonl`,
  where `<step>` is the step's number in the ledger, its kind and its subject
  (`0001-implement-01_cache`). `<step>.log` holds the `result` text of the
  stream's last event of type `result`, and is empty when there is none.
  `<step>.stderr` holds what the session wrote to stderr: a `claude` that
  fails at once, for want of a login, says so there.
- Each step appends one JSON line to `ledger.jsonl`:

  ```json
  {"kind":"implement","subject":"01_cache","started_at":"2026-11-12T18:00:47Z","ended_at":"2026-11-12T18:00:47Z","wall_minutes":0,"outcome":"landed"}
  ```

  `outcome` is `landed`, `stopped` or `stalled`; `wall_minutes` is
  `ended_at` less `started_at`, in minutes. A landed package's line also holds
  `gate` and `open_items`, copied as they are from the `AF-Run:` trailer of
  its merge commit (the tip of the base branch after the landing):

  ```
  AF-Run: {"gate":{"full":{"runs":3,"seconds":412.5,"reused":1},"task":{"runs":9,"seconds":95,"reused":0},"baseline_full_seconds":130.2,"last_full_seconds":141},"open_items":{"opened":{"awaits":1,"defect:minor":2},"closed":{},"passed_on":{},"reclassified":{}}}
  ```

  `gate` is what `speca <package> --gate-report` counted; `open_items` holds
  `opened`, `closed`, `passed_on` and `reclassified`, each counts by kind, a
  defect as `defect:<severity>`. Other keys of the trailer are ignored. A
  merge commit with no `AF-Run:` trailer, or one whose value is not one JSON
  object, still counts as landed: its line holds neither object, and a low
  `trailer_missing` warning names the commit.
- After each step the line is printed short on stderr, a landed package's
  with its trailer's counts (`landed in 12.0 min; gate 3 full in 412.5s, 1
  reused; 3 opened`). When a landed package
  took more than twice the median wall minutes of the packages that landed
  before it in the ledger, a warning line follows; stopped and stalled steps
  are no part of the median.

`specrun --ledger` prints the ledger as a table on stdout, skipping a line that
does not parse, and starts nothing:

```
STEP  KIND       SUBJECT    STARTED               MINUTES  OUTCOME
0001  implement  01_cache   2026-11-12T18:00:47Z  0.0      landed
0002  implement  02_search  2026-11-12T18:00:47Z  0.0      landed
```

With no ledger it prints one sentence: `no step is recorded in <path>`.

A run over two packages, the second depending on the first, writes on stderr
(the installed skill being older than the tree `specrun` was built from):

```
specrun: v1.5.1-57-g25ce0b9 (commit 25ce0b9, built 2026-11-12T18:00:42Z); af-impl ~/.claude/skills/af-impl/SKILL.md sha256 cd159ab9… (built with d41f18c7…); claude 2.1.322 (Claude Code); issue issue 76b8216 (commit 76b8216, built 2026-10-23T09:42:50Z)
specrun: warning: the installed af-impl skill is not the one specrun was built with; make install-skills installs this tree's: ~/.claude/skills/af-impl/SKILL.md has sha256 cd159ab9…, specrun was built with d41f18c7…
specrun: 0001-implement-01_cache landed in 0.0 min
specrun: 0002-implement-02_search landed in 0.0 min
```

## Configuration

Every setting is a flag, and an environment variable of the same name in upper
case with `-` as `_`, prefixed `SPECRUN_`, sets its default; the flag wins.

| Flag | Variable | Default | Effect |
|---|---|---|---|
| `--max N` | `SPECRUN_MAX` | no limit | land at most N packages in this run; a whole number greater than zero |
| `--stall D` | `SPECRUN_STALL` | `30m` | end a session that writes no event for D, a Go duration greater than zero |
| `--log-dir DIR` | `SPECRUN_LOG_DIR` | `agent-fox/specrun/<repository>` under the cache directory | where the streams, the final messages and the ledger go; never inside the working tree |
| `--reconcile-every N` | `SPECRUN_RECONCILE_EVERY` | `5` | run a reconcile after every N landed packages; `0` leaves only the reconcile at the end |
| `--stop-on LIST` | `SPECRUN_STOP_ON` | `security,data-loss` | stop after a package that left an open defect of one of these severities (`security`, `data-loss`, `correctness`, `minor`, comma-separated); `''` never stops |
| `--strict-skills` | `SPECRUN_STRICT_SKILLS` | off | refuse to start when the installed af-impl skill is not the one `specrun` was built with |
| `--dry-run` | | | say what the next step would be and change nothing |
| `--ledger` | | | print the ledger as a table and start nothing |
| `--reset-state` | | | remove the state file and do nothing else |
| `--version` | | | print the build identity |

A variable whose value its flag would refuse is a usage error naming the
variable (`SPECRUN_STALL=soon`, a `SPECRUN_LOG_DIR` inside the working tree). An unknown flag, a positional argument, a
`--max` or `--stall` that is not greater than zero, and two of `--dry-run`,
`--ledger` and `--reset-state` together are usage errors with exit code 2.

### The dry run

`specrun --dry-run` changes nothing: no branch is checked out, nothing is
pulled, no session starts, and no state or log is written. It reports the next
step and the package `afspec` would choose, or the waiting packages, and exits
0:

```json
{
  "tool": "specrun",
  "ok": true,
  "status": "done",
  "exit_code": 0,
  "summary": "specrun: dry run; the next step is implement .specs/01_cache",
  "result": {
    "landed": [],
    "all_done": false,
    "waiting": [
      {
        "spec_dir": ".specs/02_search",
        "spec_id": "02",
        "waits_on": ["01"],
        "reason": "it depends on packages that are not finished: 01 is not finished"
      }
    ],
    "dry_run": true,
    "next_step": { "kind": "implement", "spec_dir": ".specs/01_cache" }
  }
}
```

With a forge client, a dry run also reports what the label rules would
change as the labels now stand, for the last package that landed and for
every `af:awaits` issue, and the issues the stop rule would name, writing
nothing: `result.would_change` lists each change with `issue`, `rule`
(`incomplete-labels`, `awaits-finished`), `add` and `remove`, and
`result.would_stop` the defects as `result.stopped.issues` names them; both
are `[]` when there are none, and absent without a forge.

## The output

The run above ends with:

```json
{
  "tool": "specrun",
  "version": "v1.5.1-57-g25ce0b9 (commit 25ce0b9, built 2026-11-12T18:00:42Z)",
  "ok": true,
  "status": "done",
  "exit_code": 0,
  "summary": "specrun: every package is done, landed 2",
  "warnings": [
    { "code": "skill_mismatch", "severity": "high", "stage": "preflight",
      "message": "the installed af-impl skill is not the one specrun was built with; make install-skills installs this tree's: ~/.claude/skills/af-impl/SKILL.md has sha256 cd159ab9…, specrun was built with d41f18c7…" }
  ],
  "result": {
    "landed": ["01_cache", "02_search"],
    "all_done": true,
    "versions": {
      "specrun": "v1.5.1-57-g25ce0b9 (commit 25ce0b9, built 2026-11-12T18:00:42Z)",
      "skill_path": "~/.claude/skills/af-impl/SKILL.md",
      "skill_sha256": "cd159ab9…",
      "built_skill_sha256": "d41f18c7…",
      "claude": "2.1.322 (Claude Code)",
      "issue": "issue 76b8216 (commit 76b8216, built 2026-10-23T09:42:50Z)"
    }
  },
  "duration_ms": 405,
  "started_at": "2026-11-12T18:00:47Z"
}
```

(The samples are the command's output, run against the fake `claude` of the
tests; the home directory is written `~` and the hashes are cut short.)

| Field | Meaning |
|---|---|
| `landed` | the packages that landed in this run, by directory name, in order |
| `all_done` | true when no package is open |
| `waiting` | the open packages that cannot start, each with `spec_dir`, `spec_id`, `waits_on` and `reason` |
| `stopped` | the step the loop stopped at: `step` (`0003-implement-19_speca_gate`), `kind`, `subject` and `reason` (a landing check, `stalled`, `interrupted`, or `the session could not start`) |
| `dry_run`, `next_step` | under `--dry-run`: `true`, and the step it would take, `kind` and `spec_dir` |
| `versions` | what the run runs: `specrun`, `skill_path`, `skill_sha256`, `built_skill_sha256`, `claude`, `issue` |

A stopped run's `summary` is `specrun: stopped at <step>: <reason>; landed <n>`,
and its `error` names the stage: `land` for a landing check, `stall`,
`interrupt`, or `session`.

### Exit codes

| Code | `status` | Meaning |
|---|---|---|
| `0` | `done` | every package is done, `--max` packages landed, the dry run reported, the ledger was printed, or the state file was removed |
| `1` | `failed` | a step stopped (stage `land`, `stall`, `interrupt` or `session`), a label rule's forge call failed (`forge`), a landed package left a serious defect open (`defect`), no package can start (`waiting`, or `graph` for a cycle), the preflight failed (`preflight`: the base branch, `git`, `claude`, the log directory, `--strict-skills`), or the state file refused the start (`state`) |
| `2` | `usage` | a flag or variable that cannot hold, a positional argument, two operations, or a log directory inside the working tree |

### Warning codes

| Code | Severity | Stage | When |
|---|---|---|---|
| `skill_mismatch` | high | preflight | the installed af-impl skill's SHA-256 is not the one `specrun` was built with |
| `skill_not_installed` | high | preflight | no af-impl skill is installed where a session here would load it |
| `forge_unavailable` | low | preflight | a dry run has no forge client; it writes nothing to a forge anyway |
| `reconcile_report_missing` | low | reconcile | the reconcile session wrote no report |
| `deviation_conflict` | low | reconcile | a deviation carries both `af:accepted` and `af:rejected`, and is left alone |
| `trailer_missing` | low | land | a landed package's merge commit has no `AF-Run:` trailer holding one JSON object |

## What it leaves out

The reconcile, repairs, and the rules that read the labels beyond the check
after a landing come with later packages. It runs one package
at a time, in dependency order, and has no `--schema` self-description.
