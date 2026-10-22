// Command speca reports the state of a version 2 specification package, moves
// the state of its tasks, and moves the package through its lifecycle.
//
//	speca [flags] <spec>
//	speca <spec> --validate
//	speca --list [--all]
//	speca <spec> --task T --state S [--force] [--dry-run]
//	speca <spec> --status S [--by <spec>] [--force] [--dry-run]
//
// It takes one input, a reference to a spec package, and writes exactly one
// JSON object to stdout: the package's state, its verdict under --validate,
// with --list and no input, every package under the spec root, or, in the task
// and status forms, the package's state after the change and the changes made.
// The report, --validate and --list read the packages and change nothing: they
// write no file. The task form and the status form write the package through
// the library, which rewrites every artifact of it, and the status form moves
// an archived or superseded package under archive/. No form makes a model or
// network call or reads a credential. Its siblings are spec, which writes a
// package from an idea, and specr, which renders one as Markdown.
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
)

const tool = "speca"

// description is the one sentence --schema gives, with the sentence naming the
// three commands.
const description = "Reports the state of a specification package: its lifecycle status, whether it " +
	"validates, every task with its state and whether it is ready, the next task to work on, the " +
	"lifecycle moves allowed from its status, and the packages it depends on and that depend on it. " +
	"With --validate, the format's full verdict on the package; with --list, every package under the " +
	"spec root. With --task and --state it moves one or more tasks through the task state machine, " +
	"every one or none, and saves the package through the library, which rewrites all of its " +
	"artifacts. With --status it moves the package through its lifecycle: activates, seals, archives " +
	"or supersedes it (with --by, the package that replaces it), and moves an archived or superseded " +
	"package under archive/. Without these it changes nothing. " + cli.SiblingCommands

// inputDescription is what --schema says the one positional input may be.
const inputDescription = "Exactly one reference to a spec package: a path to its directory or to a file " +
	"inside it, or a directory name, spec number or spec name under the spec root " +
	"(16_indexed_code_search, 16, indexed_code_search). None with --list."

const usage = `speca — report the state of a specification package

` + cli.SiblingCommands + `

Usage:
  speca [flags] <spec>
  speca --list [--all] [flags]
  speca <spec> --task T --state S [flags]
  speca <spec> --status S [--by <spec>] [flags]

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
  speca <spec> --validate  the format's full verdict: the schemas, the
                           cross-file rules C1 to C11 and the cross-spec rules
                           that concern the package, each finding naming its
                           rule, and the derived traceability
  speca --list             every package under the spec root, in number order,
                           each with its status, whether it validates, its task
                           counts and its next task; --all adds archive/
  speca <spec> --task T --state S
                           the task form: move tasks T to state S. T is a task
                           id, several ids (--task 1,2 or --task 1 --task 2),
                           next (the task to work on now) or all (every task
                           not already in S and not dropped); S is pending,
                           in_progress, done or dropped. Every move must be one
                           the task state machine (format v2 §8.3.1) allows, or
                           none is made, and the result lists the changes.
                           Claiming a task that is in_progress is refused, which
                           is how a second agent learns it is taken (calls made
                           at the same moment are not locked against each other)
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
    --dry-run              with the task or status form, run every check and
                           report what would change; write nothing
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
                           in_progress or dropped

Output is one JSON object on stdout; next names the call to make after this
one. --schema prints the flags and the shape of the output as JSON Schema.

The report, --validate and --list change nothing. The task form changes the
state of the tasks and the updated_at of prd.md, and the status form the
lifecycle status, and each saves the package through the library, which
rewrites every artifact of it (prd.md, requirements.json, test_spec.json,
tasks.json and architecture.md if there is one) in its canonical encoding,
through temporary files it renames, so that formatting the encoding does not
keep (a YAML comment in the frontmatter of prd.md, say) is lost and the files
are left with mode 0600. The status form also moves the package directory under
archive/ and, when superseding, adds an id to the replacing package's
supersedes. With --dry-run it writes nothing. speca makes no model call and no
network call, and reads no credential.

Exit codes:
  0  reported — a package that does not validate is reported too, in valid
     and validation; or the tasks were moved; or the status was changed
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
     written, the result says so and the call is resumable
  2  usage error — the reference names no package, or two; two operations in
     one call; a flag that cannot hold; an id the plan does not have; --status
     draft, or a --by that names the package itself, no package or one with
     its spec_id. Nothing was written

Examples:
  speca 16
  speca .specs/16_indexed_code_search --validate
  speca --list
  speca --list --all
  speca 16 --task next --state in_progress
  speca 16 --task 4 --state done
  speca 16 --task 1,2 --state in_progress --dry-run
  speca 16 --status active
  speca 16 --status sealed --dry-run
  speca 03 --status superseded --by 12

Flags:
`

// options are speca's flags.
type options struct {
	dir      string
	specsDir string
	validate bool
	list     bool
	all      bool
	tasks    taskFlag
	state    string
	status   givenFlag
	by       givenFlag
	force    bool
	dryRun   bool
	schema   bool
	version  bool
}

// taskForm reports whether the call asks for the form that changes tasks.
func (o *options) taskForm() bool { return len(o.tasks) > 0 || o.state != "" }

// statusForm reports whether the call asks for the form that changes the
// package's lifecycle status. --by alone asks for it, so that it is refused
// where it stands rather than ignored.
func (o *options) statusForm() bool { return o.status.given || o.by.given }

func (o *options) register(fs *flag.FlagSet) {
	fs.BoolVar(&o.validate, "validate", false, "give the format's full verdict on the package: the schemas, the cross-file rules C1 to C11 and the cross-spec rules that concern it, each finding naming its rule; a package that does not validate is still a successful call")
	fs.BoolVar(&o.list, "list", false, "list every package under the spec root, in number order, each with its status, whether it validates, its task counts and its next task; takes no input")
	fs.BoolVar(&o.all, "all", false, "with --list, also list the packages under archive/, marked archived")
	fs.Var(&o.tasks, "task", "the task form: move these tasks to --state, every one or none; a task id, several ids comma-separated or repeated, next (the task to work on now) or all (every task not already in that state and not dropped)")
	fs.StringVar(&o.state, "state", "", "the task form: the state to move the tasks to: pending, in_progress, done or dropped")
	fs.Var(&o.status, "status", "the status form: move the package to this lifecycle status: active (computes the intent hash), sealed, archived or superseded (which needs --by); archived and superseded packages are moved under archive/. A move the lifecycle does not allow is refused, with the moves that are allowed")
	fs.Var(&o.by, "by", "with --status superseded, the package that replaces this one, named as the input is; it must be a draft, active or sealed package, and when it can be saved its supersedes list gets this package's id")
	fs.BoolVar(&o.force, "force", false, "with the task form and the state done or pending, set a state the task state machine refuses, through the library's own bypasses, and skip the guard on keys the schema does not know; with the status form (--status), skip the checks that an activation validates, that no task is open when sealing, and that no key the schema does not know would be dropped; it skips nothing else, and a high task_state_forced, spec_invalid or tasks_open warning says what it skipped")
	fs.BoolVar(&o.dryRun, "dry-run", false, "with the task form or the status form (--status), run every check and report what would change, and write nothing")
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
	if o.all && !o.list {
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
	if !o.taskForm() && !o.statusForm() {
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
	case status == string(afspec.PrdFrontmatterV2JsonStatusDraft):
		errs = append(errs, errors.New("--status draft: no move leads back to draft; a package whose intent must change is superseded by a new one"))
		hint = &cli.FixHint{Flag: "--status", Valid: slices.Clone(statusTargets)}
	case !slices.Contains(statusTargets, status):
		errs = append(errs, fmt.Errorf("--status %q is not a status to move to: the targets are %s",
			status, "active, sealed, superseded and archived"))
		hint = &cli.FixHint{Flag: "--status", Valid: slices.Clone(statusTargets)}
	case status == string(afspec.PrdFrontmatterV2JsonStatusSuperseded) && !o.by.given:
		errs = append(errs, errors.New("--status superseded needs --by <spec>: the package that replaces this one"))
	case status != string(afspec.PrdFrontmatterV2JsonStatusSuperseded) && o.by.given:
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
	if ref == "" && !o.list {
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

const noInputMessage = "no input: give a spec package's directory, a file inside it, or its number or name under the spec root, or --list"

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
		Flags: cli.FlagsDocument(fs, map[string][]string{"state": taskStates, "status": statusTargets}),
		// speca makes no model call and no forge write, and takes no
		// classified input, so those envelope fields never appear.
		Result: cli.EnvelopeDocument(Result{}, "side_effects", "input", "model", "usage"),
		ExitCodes: map[int]string{
			cli.ExitOK:     "reported: the state of a package, its verdict (a package that does not validate is reported too), the list of packages, the tasks moved, or the status changed",
			cli.ExitFailed: "failed: the package could not be loaded, or the spec root --list reads could not be read; or the task form was refused after the package was read (the task state machine does not allow a move, the package can no longer take a write, a save would delete keys the schema does not know) or its write failed; the stage is named in error, and a refusal's result holds the package as it is on disk; or the status form was refused after the packages were read, with no result (the lifecycle does not allow the move, an activation has no intent or does not validate, a task is open, keys would be dropped, the replacing package is superseded or archived, the archive destination is taken) or its write failed; if only the move under archive/ failed, the status is written, the result says so and the call is resumable",
			cli.ExitUsage:  "usage error: the call is refused before anything is written: the reference names no package or two, two operations in one call, a flag that cannot hold, an id the plan does not have, --status draft, or a --by that names the package itself, no package or one with its spec_id; nothing was written",
		},
	}
}

func main() {
	os.Exit(Main(os.Args[1:], os.Stdout, os.Stderr))
}
