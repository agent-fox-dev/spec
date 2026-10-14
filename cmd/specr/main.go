// Command specr renders a version 2 specification package as Markdown: the
// whole package, or the package scoped to one of its tasks.
//
//	specr [flags] <spec>
//
// It takes exactly one input, a reference to a spec package, and writes
// exactly one JSON object to stdout, whose result.markdown is the render. It
// reads the package and changes nothing; the one file it can write is
// --markdown-file.
package main

import (
	"bytes"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime/debug"
	"strings"

	"github.com/agent-fox-dev/spec/afspec"
	"github.com/agent-fox-dev/spec/internal/cli"
)

// version is the build identity. A release build sets it with
// -ldflags "-X main.version=..."; otherwise it is the module version go
// install recorded, or "dev".
var version = ""

func buildVersion() string {
	if version != "" {
		return version
	}
	if info, ok := debug.ReadBuildInfo(); ok && info.Main.Version != "" && info.Main.Version != "(devel)" {
		return info.Main.Version
	}
	return "dev"
}

const tool = "specr"

// description is the one sentence --schema gives.
const description = "Renders a specification package as Markdown: the whole package, or the package " +
	"scoped to one task, with what the task owns in full and everything else by id."

// inputDescription is what --schema says the one positional input may be.
const inputDescription = "Exactly one reference to a spec package: a path to its directory or to a file " +
	"inside it, or a directory name, spec number or spec name under the spec root " +
	"(16_indexed_code_search, 16, indexed_code_search)."

const usage = `specr — render a specification package as Markdown

Usage:
  specr [flags] <spec>

The spec is one reference to a package, tried in this order:
  a path to the package directory, or to a file inside it
  a directory name, number or name under the spec root
      16_indexed_code_search, 16, indexed_code_search

Forms:
  specr <spec>                  the whole package: PRD, architecture,
                                requirements, test specification, tasks
  specr <spec> --task N         scoped to task N: what the task owns in full,
                                every other requirement, test and task by id
  specr <spec> --section tasks  only the named sections, with or without --task
  specr <spec> --max-tokens N   drop detail until the render fits: first the
                                architecture, then each test's given, when,
                                real components and pseudocode

Output is one JSON object on stdout. The Markdown is result.markdown; the rest
of result says what was rendered and lists the plan's tasks, and next names
the task to render after this one. --markdown-file also writes the Markdown to
a file. --schema prints the flags and the shape of the output as JSON Schema.

specr changes nothing: it reads the package, and the only file it writes is
--markdown-file.

Exit codes:
  0  rendered
  1  failed: the package could not be read, or --markdown-file not written
  2  usage error — the reference names no package, or two; an unknown task;
     a flag that cannot hold. Nothing was read

Examples:
  specr .specs/16_indexed_code_search
  specr 16 --task 3
  specr 16 --task 3 --markdown-file /tmp/task-3.md
  specr 16 --section tasks

Flags:
`

// options are specr's flags.
type options struct {
	dir          string
	specsDir     string
	task         int
	sections     sectionFlag
	maxTokens    int
	markdownFile string
	schema       bool
	version      bool
}

// sectionFlag collects --section values, comma-separated or repeated, each
// checked as it is parsed.
type sectionFlag []string

func (f *sectionFlag) String() string { return strings.Join(*f, ",") }

func (f *sectionFlag) Set(raw string) error {
	for _, s := range strings.Split(raw, ",") {
		s = strings.TrimSpace(s)
		if !has(Sections, s) {
			return fmt.Errorf("unknown section %q: the sections are %s", s, strings.Join(Sections, ", "))
		}
		if !has(*f, s) {
			*f = append(*f, s)
		}
	}
	return nil
}

func (o *options) register(fs *flag.FlagSet) {
	fs.IntVar(&o.task, "task", 0, "render the package scoped to this task id: what the task owns in full, every other requirement, test and task by id and title; 0 renders the whole package")
	fs.Var(&o.sections, "section", "render only these sections, in the package's order: "+strings.Join(Sections, ", ")+"; comma-separated or repeated (default: every section)")
	fs.IntVar(&o.maxTokens, "max-tokens", 0, "drop detail until the render's estimated tokens fit: first the architecture, then each test's given, when, real components and pseudocode; 0 means no budget")
	fs.StringVar(&o.markdownFile, "markdown-file", "", "also write the Markdown to this file, atomically; relative to the working directory")
	fs.StringVar(&o.specsDir, "specs-dir", "", "where NN_name packages live; default <dir>/"+afspec.DefaultSpecDirName+" or $"+afspec.SpecDirEnv)
	fs.StringVar(&o.dir, "dir", ".", "the repository the spec root and a relative reference are resolved against")
	fs.BoolVar(&o.schema, "schema", false, "print the flags, the input and the output's JSON Schema as one JSON object, and exit")
	fs.BoolVar(&o.version, "version", false, "print the build identity and exit")
}

// validate refuses the flag values that cannot hold, before anything is
// read.
func (o *options) validate() error {
	var errs []error
	if o.task < 0 {
		errs = append(errs, fmt.Errorf("--task must be a task id, 1 or more, got %d", o.task))
	}
	if o.maxTokens < 0 {
		errs = append(errs, errors.New("--max-tokens cannot be negative"))
	}
	if o.maxTokens > 0 && o.sections != nil {
		errs = append(errs, errors.New("--max-tokens and --section cannot be combined: the budget drops detail across the whole package, and --section already chooses what is rendered"))
	}
	if o.markdownFile == "-" {
		errs = append(errs, errors.New("--markdown-file cannot be -: the Markdown is already in result.markdown on stdout"))
	} else if o.markdownFile != "" {
		if info, err := os.Stat(o.markdownFile); err == nil && info.IsDir() {
			errs = append(errs, fmt.Errorf("--markdown-file %s is a directory, not a file", o.markdownFile))
		}
	}
	return errors.Join(errs...)
}

// Main parses argv, renders, and writes one envelope to stdout. It returns
// the exit code.
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
	run := cli.NewRun(tool, buildVersion())

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
		fmt.Fprintf(stdout, "%s %s\n", tool, buildVersion())
		return cli.ExitOK
	}
	if o.schema {
		b, err := cli.EncodeJSON(describe(fs))
		if err != nil {
			return emit(stdout, run, cli.ExitFailed, nil, &cli.ErrorInfo{Stage: "schema", Category: "internal", Message: err.Error()})
		}
		_, _ = stdout.Write(b)
		return cli.ExitOK
	}
	if verr := o.validate(); verr != nil {
		fmt.Fprintf(stderr, "%s: %v\n", tool, verr)
		return emit(stdout, run, cli.ExitUsage, nil, usageInfo(verr, nil))
	}
	if strings.TrimSpace(ref) == "" {
		fs.Usage()
		if cli.IsTerminal(stdout) {
			return cli.ExitUsage
		}
		return emit(stdout, run, cli.ExitUsage, nil, usageInfo(errors.New(noInputMessage), nil))
	}

	code, result, failure := execute(ref, &o, run)
	if failure != nil && failure.Category == "usage" {
		fmt.Fprintf(stderr, "%s: %s\n", tool, failure.Message)
	}
	return emit(stdout, run, code, result, failure)
}

const noInputMessage = "no input: give a spec package's directory, a file inside it, or its number or name under the spec root"

// execute resolves the reference, loads the package and renders it.
func execute(ref string, o *options, run *cli.Run) (int, *Result, *cli.ErrorInfo) {
	root, err := filepath.Abs(o.dir)
	if err != nil {
		return cli.ExitUsage, nil, usageInfo(fmt.Errorf("resolving --dir %q: %w", o.dir, err), nil)
	}
	if info, serr := os.Stat(root); serr != nil || !info.IsDir() {
		return cli.ExitUsage, nil, usageInfo(fmt.Errorf("--dir %s is not a directory", root), nil)
	}
	specsDir := afspec.ResolveSpecsDir(o.specsDir, root)

	dir, err := locate(ref, root, specsDir)
	if err != nil {
		return cli.ExitUsage, nil, usageInfo(err, nil)
	}
	spec, err := afspec.LoadSpec(dir)
	if err != nil {
		return cli.ExitFailed, nil, &cli.ErrorInfo{Stage: "load", Category: "input",
			Message: fmt.Sprintf("the package %s could not be read: %v", dir, err)}
	}

	res := &Result{
		SpecDir:  displayPath(dir, root),
		SpecID:   spec.SpecID,
		SpecName: spec.SpecName,
		Title:    spec.Title,
		Status:   spec.Status,
		Tasks:    taskInfos(spec),
	}
	if o.task > 0 {
		t, ok := findTask(spec, o.task)
		if !ok {
			ids := make([]string, len(res.Tasks))
			for i, ti := range res.Tasks {
				ids[i] = fmt.Sprint(ti.ID)
			}
			return cli.ExitUsage, nil, usageInfo(
				fmt.Errorf("%s has no task %d; its tasks are %s", res.SpecDir, o.task, strings.Join(ids, ", ")),
				&cli.FixHint{Flag: "--task", Valid: ids})
		}
		info := taskInfo(t)
		res.Task = &info
	}

	if v := spec.Validate(); v.Valid {
		res.Valid = true
	} else {
		run.Warn(cli.WarnSpecInvalid, "low", "the package does not validate (%d errors: %s); it is rendered as it is",
			len(v.Errors), rules(v.Errors))
	}

	render(spec, renderOptions{task: o.task, sections: o.sections, maxTokens: o.maxTokens}, res, run)

	if o.markdownFile != "" {
		path, err := filepath.Abs(o.markdownFile)
		if err == nil {
			err = cli.WriteAtomic(path, []byte(res.Markdown))
		}
		if err != nil {
			return cli.ExitFailed, res, &cli.ErrorInfo{Stage: "write", Category: "disk",
				Message: fmt.Sprintf("the Markdown could not be written to %s: %v", o.markdownFile, err)}
		}
		res.MarkdownFile = path
	}
	return cli.ExitOK, res, nil
}

func findTask(spec *afspec.Spec, id int) (afspec.Task, bool) {
	if spec.Tasks == nil {
		return afspec.Task{}, false
	}
	for _, t := range spec.Tasks.Tasks {
		if t.Id == id {
			return t, true
		}
	}
	return afspec.Task{}, false
}

// rules names the distinct rules the validation errors break, in order.
func rules(entries []afspec.ValidationEntry) string {
	var out []string
	for _, e := range entries {
		name := e.Check
		if name == "" {
			name = e.Category
		}
		if !has(out, name) {
			out = append(out, name)
		}
	}
	return strings.Join(out, ", ")
}

// displayPath is dir relative to root when it is inside it, so a next[]
// entry stays short and runnable from the repository; absolute otherwise.
func displayPath(dir, root string) string {
	rel, err := filepath.Rel(root, dir)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return dir
	}
	return rel
}

func usageInfo(err error, hint *cli.FixHint) *cli.ErrorInfo {
	return &cli.ErrorInfo{Stage: "usage", Category: "usage", Message: err.Error(), FixHint: hint}
}

// emit builds the envelope and writes it. A nil *Result is no result.
func emit(stdout io.Writer, run *cli.Run, code int, result *Result, failure *cli.ErrorInfo) int {
	var r any
	if result != nil {
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
		// specr makes no model call and no forge write, and takes no
		// classified input, so those envelope fields never appear.
		Result: cli.EnvelopeDocument(Result{}, "side_effects", "input", "model", "usage"),
		ExitCodes: map[int]string{
			cli.ExitOK:     "rendered",
			cli.ExitFailed: "failed: the package could not be read, or --markdown-file could not be written; the stage is named in error",
			cli.ExitUsage:  "usage error: the reference names no package or two, an unknown task, or a flag that cannot hold; nothing was read",
		},
	}
}

func main() {
	os.Exit(Main(os.Args[1:], os.Stdout, os.Stderr))
}
