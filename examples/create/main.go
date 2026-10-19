// Command create shows how to build a spec package with the afspec library:
// start from a scaffold, add requirements, an execution path, tests and
// tasks with the builders and the Add… functions, validate the result, save
// it and activate it.
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
	// cross-file rules C1 to C11. Warnings never make a spec invalid.
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
	fmt.Fprintf(stderr, "wrote %s_%s (%s): %d requirements, %d tests, %d tasks\n",
		active.SpecID, active.SpecName, active.Status,
		len(active.Requirements.Requirements), len(active.TestSpec.Tests), len(active.Tasks.Tasks))
	return nil
}

// build assembles the spec in memory. CreateSpec gives a draft scaffold whose
// three artifacts already carry their $schema, spec_id, spec_name and
// schema_version; everything else is added here.
func build() (*afspec.Spec, error) {
	spec := afspec.CreateSpec(specID, specName)
	now := time.Now().UTC().Format(time.RFC3339)
	spec.Title = "Greet command"
	spec.CreatedAt = now
	spec.UpdatedAt = now
	spec.PRDBody = prdBody

	req, err := buildRequirements(*spec.Requirements)
	if err != nil {
		return nil, err
	}
	spec.Requirements = &req

	ts, err := buildTests(*spec.TestSpec)
	if err != nil {
		return nil, err
	}
	spec.TestSpec = &ts

	tasks, err := buildTasks(*spec.Tasks)
	if err != nil {
		return nil, err
	}
	spec.Tasks = &tasks

	return spec, nil
}

// buildRequirements fills requirements.json. Every Add… function returns an
// updated copy and leaves its input alone, and the Next…ID functions hand
// out the next free ID of each format.
func buildRequirements(req afspec.RequirementsV2Json) (afspec.RequirementsV2Json, error) {
	req.Introduction = "The greet command prints a greeting for a name given on the command line."
	req = afspec.SetGlossaryEntry(req, "greeting", "The line `Hello, <name>!` followed by a newline.")

	r := afspec.Requirement{Id: afspec.NextRequirementID(req), Title: "Greet a name"}
	criteria := []func(id string) afspec.Criterion{
		func(id string) afspec.Criterion {
			return afspec.EventDrivenCriterion(id,
				"greet is run with one name argument", "greet command",
				"write the greeting for that name to stdout and exit 0").
				WithContract("stdout is exactly `Hello, <name>!\\n`; exit code 0")
		},
		func(id string) afspec.Criterion {
			return afspec.UnwantedCriterion(id,
				"greet is run without a name argument", "greet command",
				"write a usage message to stderr and exit",
				"exit code 2; stdout is empty")
		},
		func(id string) afspec.Criterion {
			return afspec.UbiquitousCriterion(id, "greet command",
				"for any non-empty name, write a greeting that contains the name unchanged")
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

	return afspec.AddExecutionPath(req, afspec.ExecutionPath{
		Id:    afspec.NextExecutionPathID(req),
		Title: "A user greets Ada",
		Steps: []afspec.PathStep{
			{Actor: "user", Action: "runs `greet Ada`"},
			{Actor: "greet command", Action: "reads the name from its arguments"},
			{Actor: "greet command", Action: "writes `Hello, Ada!` to stdout and exits 0"},
		},
	})
}

// buildTests fills test_spec.json: every criterion is verified by a test
// (rule C4) and the execution path by a smoke test (rule C5).
func buildTests(ts afspec.TestSpecV2Json) (afspec.TestSpecV2Json, error) {
	tests := []afspec.Test{
		{
			Kind:     afspec.TestKindUnit,
			Verifies: []string{"01-REQ-1.1"},
			Title:    "A name is greeted on stdout",
			Given:    []string{"the greet command"},
			When:     "it is run with the argument Ada",
			Then:     []string{"stdout is `Hello, Ada!\\n`", "the exit code is 0"},
		},
		{
			Kind:     afspec.TestKindUnit,
			Verifies: []string{"01-REQ-1.2"},
			Title:    "A missing name is a usage error",
			Given:    []string{"the greet command"},
			When:     "it is run without arguments",
			Then:     []string{"the exit code is 2", "stdout is empty", "stderr holds a usage message"},
		},
		{
			Kind:     afspec.TestKindProperty,
			Verifies: []string{"01-REQ-1.3"},
			Title:    "Any name appears unchanged in its greeting",
			Given:    []string{"the greet command"},
			When:     "for any non-empty name it is run with that name",
			Then:     []string{"stdout contains the name unchanged"},
		},
		{
			Kind:           afspec.TestKindSmoke,
			Verifies:       []string{"01-PATH-1", "01-REQ-1.1"},
			Title:          "The built binary greets Ada",
			Given:          []string{"the greet binary is built"},
			When:           "`greet Ada` is run",
			Then:           []string{"stdout is `Hello, Ada!\\n`", "the exit code is 0"},
			RealComponents: []string{"greet binary"},
		},
	}
	for _, t := range tests {
		t.Id = afspec.NextTestID(ts)
		var err error
		if ts, err = afspec.AddTest(ts, t); err != nil {
			return ts, err
		}
	}
	return ts, nil
}

// buildTasks fills tasks.json: every test is owned by a task (C7), every
// criterion by an implement task (C8), and exactly one integration task
// comes last and owns every smoke test (C9).
func buildTasks(tasks afspec.TasksV2Json) (afspec.TasksV2Json, error) {
	tasks.TestCommands = afspec.TestCommands{
		AllTests: "go test ./... -count=1",
		Linter:   "go vet ./...",
	}

	plan := []afspec.Task{
		{
			Kind:     afspec.TaskKindImplement,
			Title:    "Implement the greet command",
			Criteria: []string{"01-REQ-1"},
			Tests:    []string{"TS-01-1", "TS-01-2", "TS-01-3"},
			Steps: []string{
				"Write TS-01-1, TS-01-2 and TS-01-3 and confirm they fail",
				"Read the name from the arguments and print the greeting",
				"Exit 2 with a usage message when no name is given",
			},
			Touches: []string{"cmd/greet/main.go"},
			State:   afspec.TaskStatePending,
		},
		{
			Kind:      afspec.TaskKindIntegration,
			Title:     "Verify the greet command end to end",
			Tests:     []string{"TS-01-4"},
			Steps:     []string{"Write TS-01-4 against the built binary and confirm it passes"},
			DependsOn: []int{1},
			State:     afspec.TaskStatePending,
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
