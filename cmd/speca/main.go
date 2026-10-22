// Command speca reports the state of a version 2 specification package.
//
//	speca [flags] <spec>
//	speca --list [--all]
//
// It takes one input, a reference to a spec package, and writes exactly one
// JSON object to stdout: the package's state, its verdict under --validate, or,
// with --list and no input, every package under the spec root. It reads the
// packages and changes nothing: it writes no file, makes no model or network
// call and reads no credential.
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

// siblings is the sentence the help and --schema each carry, so that an agent
// that finds one of the three commands learns what the others are for.
const siblings = "spec writes a package from an idea, specr renders one, speca reports its state"

// description is the one sentence --schema gives, with the sentence naming the
// three commands.
const description = "Reports the state of a specification package: its lifecycle status, whether it " +
	"validates, every task with its state and whether it is ready, the next task to work on, the " +
	"lifecycle moves allowed from its status, and the packages it depends on and that depend on it. " +
	"With --validate, the format's full verdict on the package; with --list, every package under the " +
	"spec root. It changes nothing. " + siblings + "."

// inputDescription is what --schema says the one positional input may be.
const inputDescription = "Exactly one reference to a spec package: a path to its directory or to a file " +
	"inside it, or a directory name, spec number or spec name under the spec root " +
	"(16_indexed_code_search, 16, indexed_code_search). None with --list."

const usage = `speca — report the state of a specification package

` + siblings + `.

Usage:
  speca [flags] <spec>
  speca --list [--all] [flags]

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

Output is one JSON object on stdout; next names the call to make after this
one. --schema prints the flags and the shape of the output as JSON Schema.

speca changes nothing: it writes no file, makes no model call and no network
call, and reads no credential.

Exit codes:
  0  reported — a package that does not validate is reported too, in valid
     and validation
  1  failed: the package could not be loaded, or the spec root --list reads
     could not be read
  2  usage error — the reference names no package, or two; two operations in
     one call; a flag that cannot hold. Nothing was read

Examples:
  speca 16
  speca .specs/16_indexed_code_search --validate
  speca --list
  speca --list --all

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
	force    bool
	dryRun   bool
	schema   bool
	version  bool
}

// taskForm reports whether the call asks for the form that changes tasks.
func (o *options) taskForm() bool { return len(o.tasks) > 0 || o.state != "" }

func (o *options) register(fs *flag.FlagSet) {
	fs.BoolVar(&o.validate, "validate", false, "give the format's full verdict on the package: the schemas, the cross-file rules C1 to C11 and the cross-spec rules that concern it, each finding naming its rule; a package that does not validate is still a successful call")
	fs.BoolVar(&o.list, "list", false, "list every package under the spec root, in number order, each with its status, whether it validates, its task counts and its next task; takes no input")
	fs.BoolVar(&o.all, "all", false, "with --list, also list the packages under archive/, marked archived")
	fs.Var(&o.tasks, "task", "the task form: move these tasks to --state, every one or none; a task id, several ids comma-separated or repeated, next (the task to work on now) or all (every task not already in that state and not dropped)")
	fs.StringVar(&o.state, "state", "", "the task form: the state to move the tasks to: pending, in_progress, done or dropped")
	fs.BoolVar(&o.force, "force", false, "with the task form and the state done or pending, set a state the task state machine refuses, through the library's own bypasses, and skip the guard on keys the schema does not know; it skips nothing else, and a high task_state_forced warning names each task set that way")
	fs.BoolVar(&o.dryRun, "dry-run", false, "with the task form, run every check and report what would change, and write nothing")
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
		for flagName, given := range map[string]bool{"--validate": o.validate, "--list": o.list} {
			if given {
				errs = append(errs, fmt.Errorf("the task form (--task, --state) and %s are two operations: give one", flagName))
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
	} else {
		for flagName, given := range map[string]bool{"--force": o.force, "--dry-run": o.dryRun} {
			if given {
				errs = append(errs, fmt.Errorf("%s has no effect without --task: it applies to the task form", flagName))
			}
		}
	}
	if len(errs) == 0 {
		return nil
	}
	return usageInfo(errors.Join(errs...), hint)
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
	case failure != nil:
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
		Flags: cli.FlagsDocument(fs, nil),
		// speca makes no model call and no forge write, and takes no
		// classified input, so those envelope fields never appear.
		Result: cli.EnvelopeDocument(Result{}, "side_effects", "input", "model", "usage"),
		ExitCodes: map[int]string{
			cli.ExitOK:     "reported: the state of a package, its verdict (a package that does not validate is reported too), or the list of packages",
			cli.ExitFailed: "failed: the package could not be loaded, or the spec root --list reads could not be read; the stage is named in error",
			cli.ExitUsage:  "usage error: the reference names no package or two, two operations in one call, or a flag that cannot hold; nothing was read",
		},
	}
}

func main() {
	os.Exit(Main(os.Args[1:], os.Stdout, os.Stderr))
}
