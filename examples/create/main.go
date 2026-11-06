// Command create shows how to build a spec package with the afspec library:
// start from a scaffold, add requirements with their checks, an execution
// path with its smoke check and tasks with the builders and the Add…
// functions, validate the result, save it and activate it.
//
//	go run ./examples/create [-root DIR]
//
// The package is written to DIR/01_greet_command; without -root, DIR is a
// new temporary directory. The path of the package is printed on stdout.
package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/agent-fox-dev/spec/afspec"
)

const (
	specID   = "01"
	specName = "greet_command"
)

// prdBody is the Markdown after the PRD's frontmatter. Activation hashes the
// ## Intent section and refuses a body without one.
const prdBody = `# Greet command

## Intent

A ` + "`greet`" + ` command prints a greeting for the name it is given, so that
scripts can greet a user by name with one call.

## Goals

- ` + "`greet Ada`" + ` prints ` + "`Hello, Ada!`" + `.
- Running it without a name is a usage error, not an empty greeting.

## Non-goals

- Localised greetings.
`

func main() {
	if err := run(os.Args[1:], os.Stdout, os.Stderr); err != nil {
		fmt.Fprintln(os.Stderr, "create:", err)
		os.Exit(1)
	}
}

func run(args []string, stdout, stderr io.Writer) error {
	fs := flag.NewFlagSet("create", flag.ContinueOnError)
	fs.SetOutput(stderr)
	root := fs.String("root", "", "the spec root to write the package under (default: a new temporary directory)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *root == "" {
		dir, err := os.MkdirTemp("", "afspec-example-")
		if err != nil {
			return err
		}
		*root = dir
	}

	spec, err := build()
	if err != nil {
		return err
	}

	// The directory name must agree with spec_id and spec_name (rule C1).
	// Setting Dir before validating lets Validate check that too.
	dir := filepath.Join(*root, specID+"_"+specName)
	spec.Dir = dir

	// Validate before writing anything: schema validation first, then the
	// cross-file rules C1 to C8. Warnings never make a spec invalid.
	result := spec.Validate()
	for _, w := range result.Warnings {
		fmt.Fprintf(stderr, "warning [%s] %s: %s\n", w.Check, w.Artifact, w.Message)
	}
	if !result.Valid {
		for _, e := range result.Errors {
			fmt.Fprintf(stderr, "error [%s] %s: %s\n", e.Check, e.Artifact, e.Message)
		}
		return errors.New("the spec is not valid; nothing written")
	}

	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	if err := spec.Save(dir); err != nil {
		return err
	}

	// draft → active hashes the ## Intent section into intent_hash and
	// writes the package again. From now on Save refuses a changed intent.
	active, err := spec.Transition("active", dir)
	if err != nil {
		return err
	}

	fmt.Fprintf(stdout, "%s\n", dir)
	// The plan is the stored tasks plus the integration task the package
	// derives from its paths (format v3 §8.4); Plan lists them all.
	fmt.Fprintf(stderr, "wrote %s_%s (%s): %d requirements, %d paths, %d tasks\n",
		active.SpecID, active.SpecName, active.Status,
		len(active.Requirements.Requirements), len(active.Requirements.ExecutionPaths), len(active.Plan()))
	return nil
}

// build assembles the spec in memory. CreateSpec gives a draft scaffold whose
// two artifacts already carry their $schema, spec_id, spec_name and
// schema_version; everything else is added here.
func build() (*afspec.Spec, error) {
	spec := afspec.CreateSpec(specID, specName)
	spec.Title = "Greet command"
	spec.CreatedAt = time.Now().UTC().Format(time.RFC3339)
	spec.PRDBody = prdBody

	req, err := buildRequirements(*spec.Requirements)
	if err != nil {
		return nil, err
	}
	spec.Requirements = &req

	tasks, err := buildTasks(*spec.Tasks)
	if err != nil {
		return nil, err
	}
	spec.Tasks = &tasks

	return spec, nil
}

// buildRequirements fills requirements.json. Every Add… function returns an
// updated copy and leaves its input alone, and the Next…ID functions hand
// out the next free ID of each format. Every criterion carries its check
// (rule C3): its own, or the same as another criterion's or a path's.
func buildRequirements(req afspec.RequirementsV3Json) (afspec.RequirementsV3Json, error) {
	r := afspec.Requirement{Id: afspec.NextRequirementID(req), Title: "Greet a name"}
	criteria := []func(id string) afspec.Criterion{
		func(id string) afspec.Criterion {
			return afspec.EventDrivenCriterion(id,
				"greet is run with one name argument", "greet command",
				"write the greeting for that name to stdout and exit 0").
				WithContract("stdout is exactly `Hello, <name>!\\n`; exit code 0").
				WithCheck(afspec.CheckKindUnit, []string{"the greet command"},
					"in cmd/greet/main_test.go: out, code = run(\"Ada\"); assert out == \"Hello, Ada!\\n\"; assert code == 0")
		},
		func(id string) afspec.Criterion {
			return afspec.UnwantedCriterion(id,
				"greet is run without a name argument", "greet command",
				"write a usage message to stderr and exit",
				"exit code 2; stdout is empty").
				WithCheck(afspec.CheckKindUnit, nil,
					"in cmd/greet/main_test.go: out, errOut, code = run(); assert code == 2; assert out == \"\"; assert errOut contains \"usage\"")
		},
		func(id string) afspec.Criterion {
			return afspec.UbiquitousCriterion(id, "greet command",
				"for any non-empty name, write a greeting that contains the name unchanged").
				WithCheck(afspec.CheckKindProperty, nil,
					"in cmd/greet/main_test.go: for any non-empty name: out, _ = run(name); assert out contains name")
		},
	}
	for _, criterion := range criteria {
		var err error
		if r, err = afspec.AddCriterion(r, criterion(afspec.NextCriterionID(r))); err != nil {
			return req, err
		}
	}

	req, err := afspec.AddRequirement(req, r)
	if err != nil {
		return req, err
	}

	// The path carries its smoke check: the run of the path with real
	// components and the end state it asserts. The package's integration
	// task, which the library derives, runs it.
	return afspec.AddExecutionPath(req, afspec.ExecutionPath{
		Id:    afspec.NextExecutionPathID(req),
		Title: "A user greets Ada",
		Steps: []afspec.PathStep{
			{Actor: "user", Action: "runs `greet Ada`"},
			{Actor: "greet command", Action: "reads the name from its arguments"},
			{Actor: "greet command", Action: "writes `Hello, Ada!` to stdout and exits 0"},
		},
		Smoke: afspec.Smoke{
			RealComponents: []string{"greet binary"},
			Setup:          []string{"the greet binary is built"},
			EndState:       []string{"stdout is `Hello, Ada!\\n`", "the exit code is 0"},
		},
	})
}

// buildTasks fills tasks.json: every criterion is owned by a task (rule C5).
// There is no integration task to write: a package with a path derives one
// after the last stored task, and no state to write either, which lives in
// progress.json.
func buildTasks(tasks afspec.TasksV3Json) (afspec.TasksV3Json, error) {
	tasks.TestCommands = afspec.TestCommands{
		AllTests: "go test ./... -count=1",
		Linter:   "go vet ./...",
	}

	plan := []afspec.Task{
		{
			Title:    "Implement the greet command",
			Criteria: []string{"REQ-1"},
			Steps: []string{
				"Add the greeting, the usage error and the any-name property to cmd/greet/main_test.go and confirm they fail",
				"Read the name from the arguments and print the greeting",
				"Exit 2 with a usage message when no name is given",
			},
			Touches: []string{"cmd/greet/main.go", "cmd/greet/main_test.go"},
		},
	}
	for _, task := range plan {
		task.Id = afspec.NextTaskID(tasks)
		var err error
		if tasks, err = afspec.AddTask(tasks, task); err != nil {
			return tasks, err
		}
	}
	return tasks, nil
}
