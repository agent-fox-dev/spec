// Command speca reports the state of a version 3 specification package, moves
// the state of its tasks, moves the package through its lifecycle, converts a
// version 2 package to version 3, and runs the package's gate.
//
//	speca [flags] <spec>
//	speca <spec> --validate
//	speca --list [--all]
//	speca --next
//	speca <spec> --task T --state S [--force] [--dry-run]
//	speca <spec> --status S [--by <spec>] [--force] [--dry-run]
//	speca <spec> --render [--dry-run]
//	speca --render [--all] [--dry-run]
//	speca <spec> --migrate [--dry-run]
//	speca --migrate [--all] [--dry-run]
//	speca <spec> --gate task|full [--base <commit>] [--clean-env] [--timeout <duration>]
//	speca <spec> --gate-report [--since <commit>]
//	speca <spec> --task N --evidence <file> [--dry-run] [--timeout <duration>]
//	speca <spec> --verify-evidence [--timeout <duration>]
//
// It takes one input, a reference to a spec package, and writes exactly one
// JSON object to stdout: the package's state, its verdict under --validate,
// with --list and no input, every package under the spec root, with --next the
// package to implement now, or, in the task
// and status forms, the package's state after the change and the changes made,
// under --render what it did to the package's spec.md, and under --migrate
// what it did to the package's format version.
// The report, --validate, --list and --next read the packages and change nothing: they
// write no file. The task form writes progress.json and no other file. The
// status form writes the package through the library, which rewrites every
// artifact of it and its spec.md, and moves an archived or superseded package
// under archive/. The render form writes spec.md, the package's render, and no
// other file. The migrate form rewrites a version 2 package as version 3 and
// removes its test_spec.json. The gate form runs the package's linter and
// all_tests commands and the gate report sums the gates that ran; neither
// writes under --dir, and what the gate keeps (the commands' logs, the passes
// it reuses, the run log) goes to the user's state directory. In a package
// activated with a contract hash, the evidence form runs a task's tests and
// writes their failing runs to progress.json, a close runs them again and
// writes their passing runs, and the verify form runs every recorded test
// again and writes nothing to the package (ADR 26). No form makes a
// model or network call or reads a credential. Its siblings are spec, which writes a package from an idea, and
// specr, which renders one as Markdown.
package main

import (
	"bytes"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"slices"
	"strings"

	"github.com/agent-fox-dev/spec"
	"github.com/agent-fox-dev/spec/afspec"
	"github.com/agent-fox-dev/spec/internal/cli"
	"github.com/agent-fox-dev/spec/internal/gate"
)

const tool = "speca"

// description is the one sentence --schema gives, with the sentence naming the
// three commands.
const description = "Reports the state of a specification package: its lifecycle status, whether it " +
	"validates, every task with its state and whether it is ready, the next task to work on, the " +
	"lifecycle moves allowed from its status, and the packages it depends on and that depend on it. " +
	"With --validate, the format's full verdict on the package; with --list, every package under the " +
	"spec root; with --next, the package to implement now, the first open package in dependency order " +
	"whose dependencies are finished, or that every package is done. With --task and --state it moves one or more tasks through the task state machine, " +
	"every one or none, and writes the package's progress.json and no other file. With --status it " +
	"moves the package through its lifecycle: activates, seals, archives " +
	"or supersedes it (with --by, the package that replaces it), and moves an archived or superseded " +
	"package under archive/. With --render it writes the package's spec.md, its render, and no other " +
	"file. With --migrate it converts a version 2 package to version 3 in place. With --gate task or " +
	"full it runs the package's linter, then its all_tests command, with no shell, and answers with one " +
	"verdict, pass, fail or not_run, reusing a pass recorded for the same tree; with --gate-report it sums " +
	"the package's gate runs since a commit. In a package activated with a contract hash, the hash of its " +
	"requirements.json and tasks.json, a task closes on evidence: with --task N --evidence <file> it runs the " +
	"task's tests and records the run in which each failed, --state done runs them again and closes the task " +
	"only when each passes, and --verify-evidence runs every recorded test of every done task again and " +
	"answers with one verdict; while the contract files no longer match the hash, the package's tasks and its " +
	"sealing are refused. Without these it changes nothing. " + cli.SiblingCommands

// inputDescription is what --schema says the one positional input may be.
const inputDescription = "Exactly one reference to a spec package: a path to its directory or to a file " +
	"inside it, or a directory name, spec number or spec name under the spec root " +
	"(16_indexed_code_search, 16, indexed_code_search). None with --list or --next, and none with --render or --migrate for every package; " +
	"--gate, --gate-report, --evidence and --verify-evidence need one."

const usage = `speca — report the state of a specification package

` + cli.SiblingCommands + `

Usage:
  speca [flags] <spec>
  speca --list [--all] [flags]
  speca --next [flags]
  speca <spec> --task T --state S [flags]
  speca <spec> --status S [--by <spec>] [flags]
  speca <spec> --render [--dry-run] [flags]
  speca --render [--all] [--dry-run] [flags]
  speca <spec> --migrate [--dry-run] [flags]
  speca --migrate [--all] [--dry-run] [flags]
  speca <spec> --gate task|full [--base <commit>] [--clean-env] [--timeout <duration>] [flags]
  speca <spec> --gate-report [--since <commit>] [flags]
  speca <spec> --task N --evidence <file> [--dry-run] [--timeout <duration>] [flags]
  speca <spec> --verify-evidence [--timeout <duration>] [flags]

The spec is one reference to a package, tried in this order:
  a path to the package directory, or to a file inside it
  a directory name, number or name under the spec root
      16_indexed_code_search, 16, indexed_code_search
A package under archive/ in the spec root is reached by its path.

Forms:
  speca <spec>             the package's state: its status, whether it
                           validates, every task with its state and whether it
                           is ready, the next task to work on, the lifecycle
                           moves allowed from its status, and the packages it
                           depends on and that depend on it
  speca <spec> --validate [--strict-handoffs]
                           the format's full verdict: the schemas, the
                           cross-file rules C1 to C8 and the cross-spec rules
                           that concern the package, each finding naming its
                           rule, and the derived traceability. A handoff
                           another package addresses to this one that no task
                           takes is a handoff_not_taken warning, an error with
                           --strict-handoffs, which stores nothing
  speca --list             every package under the spec root, in number order,
                           each with its status, whether it validates, its task
                           counts and its next task; --all adds archive/
  speca --next             the package to implement now: the packages under
                           the spec root are ordered by the dependencies their
                           tasks.json declare, ties broken by number, and the
                           first that is open (draft or active, with a task
                           pending or in progress) and whose every dependency
                           is finished (sealed, under archive/, or with no task
                           open) is next_spec. all_done is true when nothing is
                           left to implement; waiting lists the open packages
                           that cannot start yet, and those that do not load
                           or are in another format version, each with why. A
                           cycle among the dependencies fails the call (stage
                           graph) and names it
  speca <spec> --task T --state S
                           the task form: move tasks T to state S. T is a task
                           id, several ids (--task 1,2 or --task 1 --task 2),
                           next (the task to work on now) or all (every task
                           not already in S and not dropped); S is pending,
                           in_progress, done or dropped. Every move must be one
                           the task state machine (format v3 §8.1) allows, or
                           none is made, and the result lists the changes.
                           The integration task of a full package, which the
                           plan derives, is moved like any other
                           Claiming a task that is in_progress is refused, which
                           is how a second agent learns it is taken (calls made
                           at the same moment are not locked against each other)
                           In a package that requires evidence (active, with a
                           contract_hash), done closes a task only when every
                           criterion it owns (for the integration task, every
                           path) has an evidence entry, every test of it has a
                           failing run or is exempt (not for the integration
                           task), and every test, run now, passes; the passing
                           runs are written with the state. Otherwise the call
                           is refused at stage evidence; --force closes without
                           the checks (a high task_evidence_forced warning).
                           Each exempt entry of a task that closes is a low
                           evidence_no_red warning. A move to pending removes
                           the task's evidence. While the package's contract
                           files no longer match its contract_hash, every call
                           of the task form is refused (stage status)
  speca <spec> --task N --evidence <file>
                           the evidence form: run every test of the evidence
                           file once, through the package's evidence_tests or,
                           in a Go module that names none, through go test, and
                           record in progress.json, for task N in progress of a
                           package that requires evidence, the run in which
                           each test failed, before the task is implemented. A
                           test every entry of which has no_red, and every test
                           of the integration task, must pass or fail and gets
                           no failing run; any other test must fail, or already
                           have failed. The file is {"evidence": [{"covers",
                           "tests", "no_red"}]}, at most 1 MiB, each covers a
                           criterion (or path) task N owns. A refusal (stage
                           evidence) names every offending test or covers; the
                           run's log goes to the state directory (a low
                           evidence_log_not_written warning when it cannot)
  speca <spec> --status S [--by <spec>]
                           the status form: move the package through its
                           lifecycle. S is active, sealed, archived or
                           superseded; superseded needs --by, the package that
                           replaces this one, and its supersedes list gets this
                           package's id (a low supersedes_not_recorded warning
                           says when it cannot, and the call still succeeds).
                           Activation computes the intent hash; an archived or
                           superseded package is moved under archive/. A move
                           the lifecycle does not allow is refused with the
                           moves that are allowed, and every check is made
                           before anything is written. If the move under
                           archive/ fails after the status was written, the call
                           is resumable: running it again only moves the package
  speca <spec> --render    the render form: write the package's spec.md, the
                           render every save writes beside its artifacts,
                           whatever its status, and no other file (updated_at
                           is not touched). Each package is reported written,
                           unchanged (spec.md was its render already, and is
                           not written again) or failed
  speca --render           the sweep: the render form for every package under
                           the spec root, in number order; --all adds archive/.
                           A package that cannot be loaded or written is
                           reported failed with its reason, and the others are
                           rendered all the same
  speca <spec> --migrate   the migrate form: convert a version 2 package to
                           version 3 in place, by the rules of format v3 §13,
                           whatever its status: requirements.json takes the
                           checks of test_spec.json, which is removed,
                           progress.json takes the task states, and spec.md is
                           rendered again. A version 3 package is reported
                           unchanged; a version 2 package with a criterion no
                           test verifies, or a path no smoke test covers, is
                           reported failed and left as it is
  speca --migrate          the sweep: the migrate form for every package under
                           the spec root, in number order; --all adds archive/.
                           A package that cannot be converted is reported
                           failed with its reason, and the others are converted
                           all the same
  speca <spec> --gate task|full
                           the gate form: run the package's linter command,
                           then its all_tests command, from its test_commands,
                           in --dir, whatever its status. Each command string
                           is split on white space, a quoted stretch kept as
                           one argument, and started with no shell; the gate
                           stops at the first command that does not exit 0.
                           The verdict is pass, fail or not_run (a command
                           could not start); a failing command's last 40
                           lines are in tail, its whole output in its log.
                           A pass is recorded under the tree id of the working
                           tree (untracked files in, ignored ones out), --dir
                           within it, the level, the two command strings and
                           --clean-env, and
                           the same gate is then answered from it, reused,
                           running nothing; a failure is never reused. A full
                           gate keeps the five slowest Go test packages. The
                           logs, the passes and the run log are kept in
                           agent-fox/speca under $XDG_STATE_HOME, else
                           ~/.local/state; nothing is written under --dir.
                           Without a tree id (no git work tree, no git) or a
                           state directory that can be written, the gate runs
                           and a gate_not_recorded warning says it was not
                           recorded
    --base <commit>        with --gate task: when the package has
                           affected_tests, the task gate runs it in place of
                           all_tests, with the full id of this commit (default
                           HEAD) in place of {base}; the command as it ran is
                           part of the recorded key, and result.gate.base is
                           the id. A --base that names no commit, a --dir
                           outside a git work tree for such a package, and
                           --base with --gate full or without --gate are
                           usage errors
    --clean-env            with --gate: an empty temporary HOME, no global or
                           system git configuration, GIT_TERMINAL_PROMPT=0
    --timeout <duration>   with --gate: how long one command may run (default
                           20m); it is then ended with every process it
                           started, and the gate fails. With --evidence,
                           --verify-evidence and --state done, how long the
                           tests may run
  speca <spec> --gate-report [--since <commit>]
                           the gate report: the package's gate runs that ended
                           in pass or fail, counted when the commit checked
                           out at the run is --since or a descendant of it
                           that HEAD reaches, or MERGE_HEAD while a merge is
                           uncommitted: per level the runs, their seconds and
                           how many were reused, the full gate's seconds at
                           the baseline and on the last tree, and the last full
                           gate's slowest units. Without --since, the merge
                           base of HEAD and the base branch (the one
                           refs/remotes/origin/HEAD names, else main). It
                           writes nothing
  speca <spec> --verify-evidence
                           the verify form: run every test recorded as evidence
                           for every done task once, on the tree as it is, and
                           answer in evidence_check with one verdict, pass,
                           fail or not_run (no done task has evidence, or the
                           tests could not be run), and each test's status,
                           whatever the package's status; it writes nothing to
                           the package
    --dry-run              with the task, evidence, status, render or migrate
                           form, run every check (and the tests the call
                           runs) and report what would change; write nothing
    --force                with the task form and done or pending, set a state
                           the machine refuses, through the library's own
                           bypasses (a high task_state_forced warning names
                           each task so set); also write over keys the schema
                           does not know. With the status form, skip the check
                           that an activation validates (a high spec_invalid
                           warning), that no task is open when sealing (a high
                           tasks_open warning) and that no key the schema does
                           not know would be dropped (a high spec_invalid
                           warning). It skips nothing else, and cannot set
                           in_progress or dropped. In a package that requires
                           evidence, with done it also closes without the
                           checks of the evidence (a high task_evidence_forced
                           warning); it never gets past a changed contract

Output is one JSON object on stdout; next names the call to make after this
one. --schema prints the flags and the shape of the output as JSON Schema.

The report, --validate, --list and --next change nothing. The task form changes the
state of the tasks and writes progress.json, with its updated_at, and no other
file. The status form changes the lifecycle status and saves the package
through the library, which rewrites every artifact of it (prd.md,
requirements.json, tasks.json, progress.json and architecture.md if there is
one) in its canonical encoding, and writes spec.md, the package's render,
beside them, through temporary files it renames, so that formatting the
encoding does not keep (a YAML comment in the frontmatter of prd.md, say) is
lost and the files are left with mode 0600. The status form also moves the
package directory under archive/ and, when superseding, adds an id to the
replacing package's supersedes. The render form writes spec.md alone, through
a temporary file it renames. The migrate form writes every artifact of a
version 2 package as version 3 and removes its test_spec.json. With --dry-run
each writes nothing. speca makes no model call and no network call, and reads
no credential.

Exit codes:
  0  reported — a package that does not validate is reported too, in valid
     and validation; or the tasks were moved; or the status was changed; or
     spec.md was rendered; or the package was migrated
  1  failed: the package could not be loaded, or the spec root --list reads
     could not be read; or the task form was refused after the package was
     read: the machine does not allow a move (stage task), the package can no
     longer take a write (stage status), a save would delete keys the schema
     does not know (stage validate), or the write failed (stage write). The
     result then holds the package as it is on disk. The status form is refused
     the same way, with no result: the lifecycle does not allow the move, an
     activation has no intent or does not validate, a task is open, keys would
     be dropped, the replacing package is superseded or archived (stage status
     or validate), or the archive destination is taken (stage archive). If only the move under archive/ failed, the status is
     written, the result says so and the call is resumable. The render form
     fails when the package cannot be loaded (stage load) or its spec.md cannot
     be written (stage write), and the sweep when the spec root cannot be read
     (stage load); a package the sweep cannot render is an entry, not a failure.
     --next fails when the spec root cannot be read (stage load) or the
     dependencies of its packages form a cycle (stage graph); a package that
     cannot start, or does not load, is an entry of waiting, not a failure
     The migrate form fails when the package cannot be read, has a gap the
     conversion refuses or cannot be written (stage migrate), and the sweep
     when the spec root cannot be read (stage load); a package the sweep
     cannot convert is an entry, not a failure
     The gate form exits 0 whatever its verdict (pass, fail or not_run), and
     fails only when the package cannot be loaded; the gate report fails when
     the package or the run log cannot be read (stage load), or the commits
     since --since cannot be listed (stage git)
     The evidence form, and a close in a package that requires evidence, are
     refused at stage evidence when a test breaks its rule, a criterion has no
     entry, the task does not take evidence, or the tests cannot be run, and
     at stage status when the contract files no longer match the
     contract_hash; nothing is written. The verify form exits 0 whatever its
     verdict, and fails only when the package cannot be loaded
  2  usage error — the reference names no package, or two; two operations in
     one call; a flag that cannot hold; an id the plan does not have; --status
     draft, or a --by that names the package itself, no package or one with
     its spec_id; a --gate other than task or full, or a --timeout that is not
     a duration greater than zero; a --since that names no commit, no --since
     and no base branch, or a --gate-report outside a git work tree; an
     --evidence without --task and one task id, with --state, or whose file is
     missing, over 1 MiB or not an evidence file; a --timeout with a form that
     runs no tests. Nothing was written

Examples:
  speca 16
  speca .specs/16_indexed_code_search --validate
  speca --list
  speca --list --all
  speca --next
  speca 16 --task next --state in_progress
  speca 16 --task 4 --state done
  speca 16 --task 1,2 --state in_progress --dry-run
  speca 16 --status active
  speca 16 --status sealed --dry-run
  speca 03 --status superseded --by 12
  speca 16 --render
  speca --render --all --dry-run
  speca 09 --migrate --dry-run
  speca --migrate --all
  speca 16 --gate full
  speca 16 --gate task --timeout 10m
  speca 16 --gate full --clean-env
  speca 16 --gate-report --since 6b61d8e
  speca 16 --task 2 --evidence map.json
  speca 16 --verify-evidence

Flags:
`

// options are speca's flags.
type options struct {
	dir            string
	specsDir       string
	validate       bool
	strictHandoffs bool
	list           bool
	next           bool
	all            bool
	tasks          taskFlag
	state          string
	status         givenFlag
	by             givenFlag
	force          bool
	dryRun         bool
	render         bool
	migrate        bool
	gate           givenFlag
	cleanEnv       bool
	timeout        givenDuration
	base           givenFlag
	// The gate report.
	gateReport bool
	since      givenFlag
	// The evidence form.
	evidence givenFlag
	// The verify form.
	verifyEvidence bool
	schema         bool
	version        bool
}

// taskForm reports whether the call asks for the form that changes tasks. A
// call with --evidence asks for the evidence form instead, which takes --task.
func (o *options) taskForm() bool { return !o.evidence.given && (len(o.tasks) > 0 || o.state != "") }

// evidenceForm reports whether the call asks for the evidence form. An empty
// --evidence asks for it, so that it is refused where it stands.
func (o *options) evidenceForm() bool { return o.evidence.given }

// statusForm reports whether the call asks for the form that changes the
// package's lifecycle status. --by alone asks for it, so that it is refused
// where it stands rather than ignored.
func (o *options) statusForm() bool { return o.status.given || o.by.given }

// gateForm reports whether the call asks for the gate form. An empty --gate
// asks for it, so that it is refused where it stands.
func (o *options) gateForm() bool { return o.gate.given }

func (o *options) register(fs *flag.FlagSet) {
	fs.BoolVar(&o.strictHandoffs, "strict-handoffs", false, "with --validate, report each handoff another package addresses to this one that no task takes as an error (handoff_not_taken) instead of a warning; it stores nothing")
	fs.BoolVar(&o.validate, "validate", false, "give the format's full verdict on the package: the schemas, the cross-file rules C1 to C8 and the cross-spec rules that concern it, each finding naming its rule; a package that does not validate is still a successful call. A draft, or a package that requires evidence, whose tests nothing can run (no evidence_tests and no go.mod in --dir) is a low evidence_command_missing warning")
	fs.BoolVar(&o.list, "list", false, "list every package under the spec root, in number order, each with its status, whether it validates, its task counts and its next task; takes no input")
	fs.BoolVar(&o.next, "next", false, "name the package to implement now: the first package under the spec root, in dependency order with ties broken by number, that is open (draft or active, with a task pending or in progress) and whose every dependency is finished; all_done says when nothing is left, waiting what cannot start and why; a dependency cycle fails the call; takes no input")
	fs.BoolVar(&o.all, "all", false, "with --list, also list the packages under archive/, marked archived; with --render or --migrate and no input, also render or migrate them")
	fs.Var(&o.tasks, "task", "the task form: move these tasks to --state, every one or none; a task id, several ids comma-separated or repeated, next (the task to work on now) or all (every task not already in that state and not dropped)")
	fs.StringVar(&o.state, "state", "", "the task form: the state to move the tasks to: pending, in_progress, done or dropped. In a package that requires evidence, done runs each task's tests and closes it only on its evidence (refused at stage evidence otherwise; each exempt entry is a low evidence_no_red warning), and pending removes the task's evidence")
	fs.Var(&o.status, "status", "the status form: move the package to this lifecycle status: active (computes the intent hash), sealed, archived or superseded (which needs --by); archived and superseded packages are moved under archive/. A move the lifecycle does not allow is refused, with the moves that are allowed")
	fs.Var(&o.by, "by", "with --status superseded, the package that replaces this one, named as the input is; it must be a draft, active or sealed package, and when it can be saved its supersedes list gets this package's id")
	fs.BoolVar(&o.force, "force", false, "with the task form and the state done or pending, set a state the task state machine refuses, through the library's own bypasses, and skip the guard on keys the schema does not know; with done in a package that requires evidence, close the task without the checks of its evidence (a high task_evidence_forced warning); with the status form (--status), skip the checks that an activation validates, that no task is open when sealing, and that no key the schema does not know would be dropped; it skips nothing else, and a high task_state_forced, spec_invalid or tasks_open warning says what it skipped")
	fs.BoolVar(&o.dryRun, "dry-run", false, "with the task form, the evidence form (--evidence), the status form (--status), the render form (--render) or the migrate form (--migrate), run every check, and the tests the call runs, and report what would change, and write nothing")
	fs.BoolVar(&o.render, "render", false, "the render form: write the package's spec.md, the render every save writes beside its artifacts, whatever its status, and no other file; each package is reported written, unchanged or failed")
	fs.BoolVar(&o.migrate, "migrate", false, "the migrate form: convert a version 2 package to version 3 in place, whatever its status, by the rules of format v3 §13; a version 3 package is left alone; each package is reported written, unchanged or failed")
	fs.Var(&o.gate, "gate", "the gate form: run the package's linter command, then its all_tests command, from its test_commands, in --dir, each started directly with no shell, and answer with one verdict, pass, fail or not_run; task or full, the gate's level, which is part of the answer; whatever the package's status, and writing nothing under --dir")
	fs.BoolVar(&o.cleanEnv, "clean-env", false, "with --gate, run every command with an empty temporary HOME, no global or system git configuration and GIT_TERMINAL_PROMPT=0, every other variable inherited; the directory is removed when the gate ends")
	fs.Var(&o.base, "base", "with --gate task, the commit the task's change started from, which replaces {base} in the package's affected_tests command (default HEAD)")
	o.timeout.value = gate.DefaultTimeout
	fs.Var(&o.timeout, "timeout", "with --gate, how long one command may run, a Go duration greater than zero; a command still running then is ended with every process it started, and the gate fails; with --evidence, --verify-evidence and --state done, how long the package's tests may run")
	fs.Var(&o.evidence, "evidence", "the evidence form, with --task and one task id: run every test of this evidence file once and record, for a task in progress of a package that requires evidence, the run in which each test failed before the task is implemented; the file is {\"evidence\": [{\"covers\": <criterion id, or path id for the integration task>, \"tests\": [<identifier>...], \"no_red\": <reason, for an exemption>}]}, at most 1 MiB; a refusal is at stage evidence, and a state directory that cannot be written is a low evidence_log_not_written warning")
	fs.BoolVar(&o.verifyEvidence, "verify-evidence", false, "the verify form: run every test recorded as evidence for every done task of the package once, on the tree as it is, and answer with one verdict, pass, fail or not_run, and each test's status; whatever the package's status, and writing nothing to it")
	fs.BoolVar(&o.gateReport, "gate-report", false, "the gate report: sum the package's gate runs recorded since a commit (--since, or the merge base of HEAD and the base branch): the full and task runs, their seconds and how many were reused, the full gate's seconds at the baseline and on the last tree, and the last full gate's slowest units; whatever the package's status, and writing nothing")
	fs.Var(&o.since, "since", "with --gate-report, the commit the runs are counted from: a run at it or at a descendant of it that HEAD reaches, or MERGE_HEAD while a merge is uncommitted, counts; default the merge base of HEAD and the branch refs/remotes/origin/HEAD names, else main")
	fs.StringVar(&o.specsDir, "specs-dir", "", "where NN_name packages live; default <dir>/"+afspec.DefaultSpecDirName+" or $"+afspec.SpecDirEnv)
	fs.StringVar(&o.dir, "dir", ".", "the repository the spec root and a relative reference are resolved against")
	fs.BoolVar(&o.schema, "schema", false, "print the flags, the input and the output's JSON Schema as one JSON object, and exit")
	fs.BoolVar(&o.version, "version", false, "print the build identity and exit")
}

// check refuses the flag combinations that cannot hold, before anything is
// read, as a usage error. ref is the one positional input, or empty.
func (o *options) check(ref string) *cli.ErrorInfo {
	var errs []error
	var hint *cli.FixHint
	if o.list && ref != "" {
		errs = append(errs, fmt.Errorf("--list takes no input, got %q: it lists every package under the spec root", ref))
	}
	if o.validate && o.list {
		errs = append(errs, errors.New("--validate and --list are two operations: give one"))
	}
	if o.strictHandoffs && !o.validate {
		errs = append(errs, errors.New("--strict-handoffs goes with --validate: it makes an untaken inbound handoff an error of the verdict"))
	}
	if o.next {
		errs = append(errs, o.nextErrors(ref)...)
	}
	if o.render {
		errs = append(errs, o.sweepErrors("--render", "the render form writes one derived file in any status and refuses nothing", ref)...)
	}
	if o.migrate {
		errs = append(errs, o.sweepErrors("--migrate", "the migrate form converts a package in any status and refuses nothing it can convert", ref)...)
	}
	if o.render && o.migrate {
		errs = append(errs, errors.New("--render and --migrate are two operations: give one"))
	}
	if o.all && !o.list && !o.render && !o.migrate {
		errs = append(errs, errors.New("--all adds the packages under archive/ to --list, and means nothing without it"))
	}
	if o.taskForm() {
		for _, f := range []struct {
			name  string
			given bool
		}{{"--validate", o.validate}, {"--list", o.list}} {
			if f.given {
				errs = append(errs, fmt.Errorf("the task form (--task, --state) and %s are two operations: give one", f.name))
			}
		}
		if len(o.tasks) == 0 {
			errs = append(errs, errors.New("--state needs --task: say which task to move (an id, several ids, next or all)"))
		} else if _, err := parseTaskSpec(o.tasks); err != nil {
			errs = append(errs, err)
		}
		switch {
		case o.state == "":
			errs = append(errs, errors.New("--task needs --state: say which state to move the tasks to ("+strings.Join(taskStates, ", ")+")"))
		case !slices.Contains(taskStates, o.state):
			errs = append(errs, fmt.Errorf("--state %q is not a task state: the states are %s", o.state, strings.Join(taskStates, ", ")))
			hint = &cli.FixHint{Flag: "--state", Valid: slices.Clone(taskStates)}
		}
	}
	if o.statusForm() {
		more, statusHint := o.statusErrors()
		errs = append(errs, more...)
		if statusHint != nil {
			hint = statusHint
		}
	}
	if o.gateForm() {
		more, gateHint := o.gateErrors(ref)
		errs = append(errs, more...)
		if gateHint != nil {
			hint = gateHint
		}
	} else {
		for _, f := range []struct {
			name  string
			given bool
		}{{"--clean-env", o.cleanEnv}, {"--timeout", o.timeout.given && !o.runsTests()}} {
			if f.given {
				if f.name == "--timeout" {
					errs = append(errs, errors.New("--timeout goes with --gate, --evidence, --verify-evidence or --state done: it limits how long a package's tests may run"))
					continue
				}
				errs = append(errs, fmt.Errorf("%s goes with --gate: it applies to the gate form", f.name))
			}
		}
		if o.base.given {
			errs = append(errs, errors.New("--base goes with --gate task: it names the commit a task's affected tests start from"))
		}
	}
	if o.evidenceForm() {
		errs = append(errs, o.evidenceErrors(ref)...)
	}
	if o.verifyEvidence {
		errs = append(errs, o.verifyErrors(ref)...)
	}
	if o.gateReport {
		errs = append(errs, o.gateReportErrors(ref)...)
	} else if o.since.given {
		errs = append(errs, errors.New("--since goes with --gate-report: it names the commit the gate runs are counted from"))
	}
	if o.timeout.given && o.timeout.value <= 0 {
		errs = append(errs, fmt.Errorf("--timeout %s: a command's time limit must be greater than zero", o.timeout.value))
	}
	if !o.taskForm() && !o.statusForm() && !o.render && !o.migrate && !o.evidenceForm() {
		for _, f := range []struct {
			name  string
			given bool
		}{{"--force", o.force}, {"--dry-run", o.dryRun}} {
			if f.given {
				errs = append(errs, fmt.Errorf("%s has no effect without --task or --status: it applies to the task form and the status form", f.name))
			}
		}
	}
	if len(errs) == 0 {
		return nil
	}
	return usageInfo(errors.Join(errs...), hint)
}

// runsTests reports whether the call may run a package's tests other than
// through the gate, and so takes --timeout: the evidence form, and the task
// form closing tasks, which in a package that requires evidence runs their
// tests (the flags are checked before the package is read).
func (o *options) runsTests() bool {
	return o.evidenceForm() || o.verifyEvidence || (o.taskForm() && o.state == string(afspec.TaskStateDone))
}

// verifyErrors are the faults of a call that asks for the verify form: it
// needs a package and is the only operation of the call.
func (o *options) verifyErrors(ref string) []error {
	var errs []error
	if ref == "" {
		errs = append(errs, errors.New("--verify-evidence needs a package: name the package whose evidence to check"))
	}
	for _, f := range []struct {
		name  string
		given bool
	}{
		{"--validate", o.validate}, {"--list", o.list}, {"--next", o.next}, {"--render", o.render}, {"--migrate", o.migrate},
		{"the task form (--task, --state)", o.taskForm()}, {"the status form (--status, --by)", o.statusForm()},
		{"--evidence", o.evidenceForm()}, {"--gate", o.gateForm()}, {"--gate-report", o.gateReport},
	} {
		if f.given {
			errs = append(errs, fmt.Errorf("--verify-evidence and %s are two operations: give one", f.name))
		}
	}
	return errs
}

// evidenceErrors are the faults of a call that asks for the evidence form: it
// needs a package, a file, and --task with exactly one task id; it moves no
// state, has nothing for --force to override, and is the only operation of
// the call.
func (o *options) evidenceErrors(ref string) []error {
	var errs []error
	if ref == "" {
		errs = append(errs, errors.New("--evidence needs a package: name the package whose task the evidence is for"))
	}
	if strings.TrimSpace(o.evidence.value) == "" {
		errs = append(errs, errors.New("--evidence is empty: name the evidence file"))
	}
	switch spec, err := parseTaskSpec(o.tasks); {
	case len(o.tasks) == 0:
		errs = append(errs, errors.New("--evidence needs --task with one task id: the task the evidence is for"))
	case err != nil:
		errs = append(errs, err)
	case spec.keyword != "" || len(spec.ids) != 1:
		errs = append(errs, fmt.Errorf("--evidence takes exactly one task id with --task, not %s: evidence is recorded for one task at a time", strings.Join(o.tasks, ",")))
	}
	if o.state != "" {
		errs = append(errs, errors.New("--evidence and --state are two operations: record the evidence, then move the task"))
	}
	if o.force {
		errs = append(errs, errors.New("--force has no effect with --evidence: no test's rule can be skipped when its runs are recorded"))
	}
	for _, f := range []struct {
		name  string
		given bool
	}{
		{"--validate", o.validate}, {"--list", o.list}, {"--next", o.next}, {"--render", o.render}, {"--migrate", o.migrate},
		{"the status form (--status, --by)", o.statusForm()}, {"--gate", o.gateForm()}, {"--gate-report", o.gateReport},
	} {
		if f.given {
			errs = append(errs, fmt.Errorf("--evidence and %s are two operations: give one", f.name))
		}
	}
	return errs
}

// nextErrors are the faults of a call that asks for --next: it takes no input
// and is the only operation of the call.
func (o *options) nextErrors(ref string) []error {
	var errs []error
	if ref != "" {
		errs = append(errs, fmt.Errorf("--next takes no input, got %q: it chooses among every package under the spec root", ref))
	}
	for _, f := range []struct {
		name  string
		given bool
	}{
		{"--validate", o.validate}, {"--list", o.list}, {"--render", o.render}, {"--migrate", o.migrate},
		{"the task form (--task, --state)", o.taskForm()}, {"the status form (--status, --by)", o.statusForm()},
	} {
		if f.given {
			errs = append(errs, fmt.Errorf("--next and %s are two operations: give one", f.name))
		}
	}
	return errs
}

// gateErrors are the faults of a call that asks for the gate form: it needs a
// package, is the only operation of the call, and its level is task or full.
// The hint holds the two levels when it is neither.
func (o *options) gateErrors(ref string) ([]error, *cli.FixHint) {
	var errs []error
	var hint *cli.FixHint
	if !slices.Contains(gateLevels, o.gate.value) {
		errs = append(errs, fmt.Errorf("--gate %q is not a gate level: the levels are task and full", o.gate.value))
		hint = &cli.FixHint{Flag: "--gate", Valid: slices.Clone(gateLevels)}
	}
	if ref == "" {
		errs = append(errs, errors.New("--gate needs a package: name the package whose test_commands to run"))
	}
	if o.base.given && o.gate.value != gateLevels[0] {
		errs = append(errs, fmt.Errorf("--base goes with --gate task: the %s level runs all_tests, from no base", o.gate.value))
	}
	for _, f := range []struct {
		name  string
		given bool
	}{
		{"--validate", o.validate}, {"--list", o.list}, {"--next", o.next}, {"--render", o.render}, {"--migrate", o.migrate},
		{"the task form (--task, --state)", o.taskForm()}, {"the status form (--status, --by)", o.statusForm()},
		{"--gate-report", o.gateReport},
	} {
		if f.given {
			errs = append(errs, fmt.Errorf("--gate and %s are two operations: give one", f.name))
		}
	}
	return errs, hint
}

// gateReportErrors are the faults of a call that asks for the gate report: it
// needs a package and is the only operation of the call. Its clash with --gate
// is named by gateErrors.
func (o *options) gateReportErrors(ref string) []error {
	var errs []error
	if ref == "" {
		errs = append(errs, errors.New("--gate-report needs a package: name the package whose gate runs to sum"))
	}
	for _, f := range []struct {
		name  string
		given bool
	}{
		{"--validate", o.validate}, {"--list", o.list}, {"--next", o.next}, {"--render", o.render}, {"--migrate", o.migrate},
		{"the task form (--task, --state)", o.taskForm()}, {"the status form (--status, --by)", o.statusForm()},
	} {
		if f.given {
			errs = append(errs, fmt.Errorf("--gate-report and %s are two operations: give one", f.name))
		}
	}
	return errs
}

// sweepErrors are the faults of a call that asks for the render form or the
// migrate form (flag, with why --force is inert for it): it is the only
// operation of the call, --force has nothing to override, and --all goes with
// the sweep and not with one package.
func (o *options) sweepErrors(flag, forceInert, ref string) []error {
	var errs []error
	for _, f := range []struct {
		name  string
		given bool
	}{
		{"--validate", o.validate}, {"--list", o.list},
		{"the task form (--task, --state)", o.taskForm()}, {"the status form (--status, --by)", o.statusForm()},
	} {
		if f.given {
			errs = append(errs, fmt.Errorf("%s and %s are two operations: give one", flag, f.name))
		}
	}
	if o.force {
		errs = append(errs, fmt.Errorf("--force has no effect with %s: %s", flag, forceInert))
	}
	if o.all && ref != "" {
		errs = append(errs, fmt.Errorf("--all adds the archive to the sweep (speca %s with no package), and means nothing for one package", flag))
	}
	return errs
}

// statusErrors are the faults of a call that asks for the status form, apart
// from the ones of the flags it shares with the others: it is the only
// operation of the call, --status is one of the four targets, and --by goes with
// superseded and with nothing else. The hint holds the targets when --status is
// not one of them.
func (o *options) statusErrors() ([]error, *cli.FixHint) {
	var errs []error
	var hint *cli.FixHint
	if o.taskForm() {
		errs = append(errs, errors.New("the status form (--status) and the task form (--task, --state) are two operations: give one"))
	}
	for _, f := range []struct {
		name  string
		given bool
	}{{"--validate", o.validate}, {"--list", o.list}} {
		if f.given {
			errs = append(errs, fmt.Errorf("the status form (--status) and %s are two operations: give one", f.name))
		}
	}
	status := o.status.value
	switch {
	case !o.status.given:
		errs = append(errs, errors.New("--by goes with --status superseded: it names the package that replaces this one"))
	case status == string(afspec.PrdFrontmatterV3JsonStatusDraft):
		errs = append(errs, errors.New("--status draft: no move leads back to draft; a package whose intent must change is superseded by a new one"))
		hint = &cli.FixHint{Flag: "--status", Valid: slices.Clone(statusTargets)}
	case !slices.Contains(statusTargets, status):
		errs = append(errs, fmt.Errorf("--status %q is not a status to move to: the targets are %s",
			status, "active, sealed, superseded and archived"))
		hint = &cli.FixHint{Flag: "--status", Valid: slices.Clone(statusTargets)}
	case status == string(afspec.PrdFrontmatterV3JsonStatusSuperseded) && !o.by.given:
		errs = append(errs, errors.New("--status superseded needs --by <spec>: the package that replaces this one"))
	case status != string(afspec.PrdFrontmatterV3JsonStatusSuperseded) && o.by.given:
		errs = append(errs, fmt.Errorf("--by goes with --status superseded, not --status %s: it names the package that replaces this one", status))
	}
	if o.by.given && strings.TrimSpace(o.by.value) == "" {
		errs = append(errs, errors.New("--by is empty: name the package that replaces this one"))
	}
	return errs, hint
}

// encodeJSON writes the --schema document. It is a variable so that a test can
// make encoding fail.
var encodeJSON = cli.EncodeJSON

// Main parses argv, reports, and writes one envelope to stdout. It returns the
// exit code.
//
// Two paths are for a person and print text instead: --version, and
// -h/--help or a bare invocation, which print the help to stderr. A bare
// invocation also writes a usage envelope when stdout is not a terminal, so a
// program piping stdout still gets one object to parse.
func Main(argv []string, stdout, stderr io.Writer) int {
	var o options
	fs := flag.NewFlagSet(tool, flag.ContinueOnError)
	fs.Usage = func() {
		fmt.Fprint(fs.Output(), usage)
		fs.PrintDefaults()
	}
	o.register(fs)
	run := cli.NewRun(tool, spec.Identity())

	var parseOut bytes.Buffer
	fs.SetOutput(&parseOut)
	ref, err := cli.SplitArgs(fs, argv)
	fs.SetOutput(stderr)
	if err != nil {
		_, _ = io.Copy(stderr, &parseOut)
		if errors.Is(err, flag.ErrHelp) {
			return cli.ExitOK
		}
		fmt.Fprintf(stderr, "%s: %v\n", tool, err)
		return emit(stdout, run, cli.ExitUsage, nil, usageInfo(err, nil))
	}
	if o.version {
		fmt.Fprintf(stdout, "%s %s\n", tool, spec.Identity())
		return cli.ExitOK
	}
	if o.schema {
		b, err := encodeJSON(describe(fs))
		if err != nil {
			return emit(stdout, run, cli.ExitFailed, nil, &cli.ErrorInfo{Stage: "schema", Category: "internal", Message: err.Error()})
		}
		_, _ = stdout.Write(b)
		return cli.ExitOK
	}
	ref = strings.TrimSpace(ref)
	if ref == "" && !o.list && !o.next && !o.render && !o.migrate && !o.gateForm() && !o.gateReport && !o.verifyEvidence && !o.evidenceForm() {
		fs.Usage()
		if cli.IsTerminal(stdout) {
			return cli.ExitUsage
		}
		return emit(stdout, run, cli.ExitUsage, nil, usageInfo(errors.New(noInputMessage), nil))
	}
	if refusal := o.check(ref); refusal != nil {
		fmt.Fprintf(stderr, "%s: %s\n", tool, refusal.Message)
		return emit(stdout, run, cli.ExitUsage, nil, refusal)
	}

	result, failure := execute(ref, &o, run)
	code := cli.ExitOK
	if failure != nil {
		code = cli.ExitCodeFor(failure.Category)
		if failure.Category == "usage" {
			fmt.Fprintf(stderr, "%s: %s\n", tool, failure.Message)
		}
	}
	return emit(stdout, run, code, result, failure)
}

const noInputMessage = "no input: give a spec package's directory, a file inside it, or its number or name under the spec root, or --list or --next"

func usageInfo(err error, hint *cli.FixHint) *cli.ErrorInfo {
	return &cli.ErrorInfo{Stage: "usage", Category: "usage", Message: err.Error(), FixHint: hint}
}

// emit builds the envelope and writes it. A nil *Result is no result.
func emit(stdout io.Writer, run *cli.Run, code int, result *Result, failure *cli.ErrorInfo) int {
	var r any
	switch {
	case result == nil:
	case failure != nil && !result.resumable:
		r = refused{result}
	default:
		r = result
	}
	return cli.Emit(stdout, run.Envelope(code, r, failure))
}

// selfDescription is the document --schema prints in place of running.
type selfDescription struct {
	Tool        string           `json:"tool"`
	Description string           `json:"description"`
	Input       selfDescribeIn   `json:"input"`
	Flags       cli.SchemaObject `json:"flags"`
	Result      cli.SchemaObject `json:"result"`
	ExitCodes   map[int]string   `json:"exit_codes"`
}

type selfDescribeIn struct {
	Description string   `json:"description"`
	Examples    []string `json:"examples"`
}

// describe builds the self-description from the flag set and the Go types.
// It reads nothing else: no directory, no environment.
func describe(fs *flag.FlagSet) selfDescription {
	return selfDescription{
		Tool:        tool,
		Description: description,
		Input: selfDescribeIn{
			Description: inputDescription,
			Examples:    []string{".specs/16_indexed_code_search", "16", "indexed_code_search", ".specs/16_indexed_code_search/tasks.json"},
		},
		Flags: cli.FlagsDocument(fs, map[string][]string{"state": taskStates, "status": statusTargets, "gate": gateLevels}),
		// speca makes no model call and no forge write, and takes no
		// classified input, so those envelope fields never appear.
		Result: cli.EnvelopeDocument(Result{}, "side_effects", "input", "model", "usage"),
		ExitCodes: map[int]string{
			cli.ExitOK:     "reported: the state of a package, its verdict (a package that does not validate is reported too), the list of packages, the package to implement next (or that every package is done, or that none can start), the tasks moved, the status changed, what --render did to spec.md, what --migrate did to the package, the gate's verdict whatever it is (pass, fail or not_run), the gate report, the evidence recorded by --evidence, or the verdict of --verify-evidence whatever it is",
			cli.ExitFailed: "failed: the package could not be loaded, or the spec root --list reads could not be read; or the task form was refused after the package was read (the task state machine does not allow a move, the package can no longer take a write, a save would delete keys the schema does not know) or its write failed; the stage is named in error, and a refusal's result holds the package as it is on disk; or the status form was refused after the packages were read, with no result (the lifecycle does not allow the move, an activation has no intent or does not validate, a task is open, keys would be dropped, the replacing package is superseded or archived, the archive destination is taken) or its write failed; if only the move under archive/ failed, the status is written, the result says so and the call is resumable; or the render form's package could not be loaded or its spec.md could not be written, or the migrate form's package could not be read, converted or written, or the spec root a sweep reads could not be read, or the dependencies of the packages --next reads form a cycle, or the package the gate or the gate report reads could not be loaded, or the run log could not be read, or the commits since --since could not be listed (stage git); or the evidence form, or a close in a package that requires evidence, was refused (stage evidence: a test broke its rule, a criterion has no entry, the task takes no evidence, the tests could not be run; stage status: the contract files no longer match the contract_hash), writing nothing",
			cli.ExitUsage:  "usage error: the call is refused before anything is written: the reference names no package or two, two operations in one call, a flag that cannot hold, an id the plan does not have, --status draft, or a --by that names the package itself, no package or one with its spec_id, a --gate other than task or full, a --timeout that is not a duration greater than zero, a --base that names no commit or comes without --gate task, a package with affected_tests outside a git work tree at the task level, a --since that names no commit, no --since and no base branch, or a --gate-report outside a git work tree, an --evidence without --task and exactly one task id or with --state, an evidence file that is missing, over 1 MiB or not one, --verify-evidence with another form, or a --timeout with a form that runs no tests; nothing was written",
		},
	}
}

func main() {
	os.Exit(Main(os.Args[1:], os.Stdout, os.Stderr))
}
