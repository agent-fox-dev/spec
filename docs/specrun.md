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
  `ended_at` less `started_at`, in minutes.
- After each step the line is printed short on stderr. When a landed package
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
| `1` | `failed` | a step stopped (stage `land`, `stall`, `interrupt` or `session`), no package can start (`waiting`, or `graph` for a cycle), the preflight failed (`preflight`: the base branch, `git`, `claude`, the log directory, `--strict-skills`), or the state file refused the start (`state`) |
| `2` | `usage` | a flag or variable that cannot hold, a positional argument, two operations, or a log directory inside the working tree |

### Warning codes

| Code | Severity | Stage | When |
|---|---|---|---|
| `skill_mismatch` | high | preflight | the installed af-impl skill's SHA-256 is not the one `specrun` was built with |
| `skill_not_installed` | high | preflight | no af-impl skill is installed where a session here would load it |

## What it leaves out

`specrun` writes nothing to a forge and needs no token: labels, the checks on
issues, the reconcile and repairs come with later packages. It runs one package
at a time, in dependency order, and has no `--schema` self-description.
