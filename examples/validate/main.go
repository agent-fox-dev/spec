// Command validate shows how to read a spec package with the afspec library
// and check it: load the artifacts, run schema and cross-file validation,
// and report the coverage and traceability the format derives from them.
//
//	go run ./examples/validate DIR
//
// DIR is a spec package directory, e.g. .specs/01_greet_command. The exit
// code is 0 for a valid package, 1 for an invalid one or one that cannot be
// read, 2 for a usage error.
package main

import (
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/agent-fox-dev/spec/afspec"
)

func main() {
	if len(os.Args) != 2 {
		fmt.Fprintln(os.Stderr, "usage: validate DIR")
		os.Exit(2)
	}
	valid, err := run(os.Args[1], os.Stdout)
	if err != nil {
		fmt.Fprintln(os.Stderr, "validate:", err)
		os.Exit(1)
	}
	if !valid {
		os.Exit(1)
	}
}

// run reports on the package in dir and whether it is valid. An error means
// the package could not be read at all.
func run(dir string, w io.Writer) (bool, error) {
	// LoadSpec reads prd.md, the three JSON artifacts and, when present,
	// architecture.md. It fails only on what it cannot read or parse: a
	// package that loads can still be invalid.
	spec, err := afspec.LoadSpec(dir)
	if err != nil {
		var loadErr *afspec.LoadError
		if errors.As(err, &loadErr) {
			return false, fmt.Errorf("cannot read %s: %s", loadErr.File, loadErr.Msg)
		}
		return false, err
	}

	fmt.Fprintf(w, "%s_%s — %s (%s)\n", spec.SpecID, spec.SpecName, spec.Title, spec.Status)
	fmt.Fprintf(w, "  %d requirements, %d execution paths, %d tests, %d tasks\n",
		len(spec.Requirements.Requirements), len(spec.Requirements.ExecutionPaths),
		len(spec.TestSpec.Tests), len(spec.Tasks.Tasks))

	// Validate runs the four JSON Schemas, then the cross-file rules C1 to
	// C11 (IDs resolve, every criterion is tested, every test is owned by a
	// task, …). ValidateSchema and ValidateCrossFile run either layer alone.
	result := spec.Validate()
	if result.Valid {
		fmt.Fprintln(w, "\nvalid")
	} else {
		fmt.Fprintf(w, "\ninvalid: %d errors\n", len(result.Errors))
	}
	for _, e := range result.Errors {
		fmt.Fprintf(w, "  error   %s\n", describe(e))
	}
	// Warnings (scope limits, vague language, …) never make a spec invalid.
	for _, e := range result.Warnings {
		fmt.Fprintf(w, "  warning %s\n", describe(e))
	}

	// Coverage and traceability are derived from the artifacts, never stored.
	// On an invalid spec they show where the gaps are.
	coverage := spec.TestSpec.ComputeCoverage(spec.Requirements)
	fmt.Fprintf(w, "\ncoverage: %d of %d criteria and paths verified\n",
		len(coverage.Covered), len(coverage.Covered)+len(coverage.Uncovered))
	for _, id := range coverage.Uncovered {
		fmt.Fprintf(w, "  not verified: %s\n", id)
	}

	fmt.Fprintln(w, "\ntraceability:")
	for _, link := range spec.ComputeTraceability().Links {
		fmt.Fprintf(w, "  %-12s tests %-24s tasks %v\n", link.ID, strings.Join(link.Tests, ","), link.Tasks)
	}

	return result.Valid, nil
}

// describe renders one validation entry as "[check] artifact entity: message".
func describe(e afspec.ValidationEntry) string {
	where := e.Artifact
	if e.EntityID != "" {
		where += " " + e.EntityID
	}
	if e.Path != "" {
		where += " " + e.Path
	}
	return fmt.Sprintf("[%s] %s: %s", e.Check, where, e.Message)
}
