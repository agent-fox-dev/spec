package main

import (
	"os"
	"path/filepath"
	"slices"

	"github.com/agent-fox-dev/spec/afspec"
	"github.com/agent-fox-dev/spec/internal/cli"
	"github.com/agent-fox-dev/spec/internal/specpkg"
)

// verdict is the form of speca <spec> --validate: the format's full verdict on
// the package in dir. That is the package's own validation (the schemas and
// the cross-file rules C1 to C11) and the cross-spec rules that concern it,
// which only a run over the whole spec root can apply. A package that does not
// validate is a successful call: the answer is in valid.
func verdict(c *call, dir string) (*Result, *cli.ErrorInfo) {
	spec, failure := c.loadPackage(dir)
	if failure != nil {
		return nil, failure
	}
	r := &Result{AdminResult: c.identityOf(dir, spec)}

	own := spec.Validate()
	combined := afspec.ValidationResult{Errors: slices.Clone(own.Errors), Warnings: slices.Clone(own.Warnings)}
	if cross, ok := c.crossSpec(spec, dir); ok {
		combined.Errors = append(combined.Errors, cross.Errors...)
		combined.Warnings = append(combined.Warnings, cross.Warnings...)
	}
	combined.Valid = len(combined.Errors) == 0

	validation := specpkg.ReportOf(combined)
	r.Validation = &validation
	valid := validation.Valid
	r.Valid = &valid
	traceability := specpkg.TraceOf(spec)
	r.Traceability = &traceability
	return r, nil
}

// crossSpec runs the cross-spec rules (format v2 §10.4) over every package of
// the spec root that loads, and the package in dir when it is not one of them
// (an archived package is not), and returns the findings that concern spec. A
// package that fails to load is left out. When the spec root cannot be read
// the rules are skipped, with a warning, and ok is false.
func (c *call) crossSpec(spec *afspec.Spec, dir string) (concerning afspec.ValidationResult, ok bool) {
	names, err := specpkg.List(c.specsDir)
	if err != nil {
		c.warnUnreadable("the spec root", c.specsDir, err, "the cross-spec rules are skipped, and the verdict is the package's own")
		return afspec.ValidationResult{}, false
	}
	var specs []*afspec.Spec
	included := false
	for _, name := range names {
		path := filepath.Join(c.specsDir, name)
		if sameDir(path, dir) {
			specs, included = append(specs, spec), true
		} else if other, err := afspec.LoadSpec(path); err == nil {
			specs = append(specs, other)
		}
	}
	if !included {
		specs = append(specs, spec)
	}

	metas := make([]afspec.SpecMeta, len(specs))
	for i, s := range specs {
		metas[i] = afspec.SpecMeta{SpecID: s.SpecID, SpecName: s.SpecName, Status: s.Status, Dir: s.Dir}
	}
	// ValidateCrossSpec reports a dependency on a package that is not there
	// itself, so the graph's error, which says the same, adds nothing.
	graph, _ := afspec.BuildDependencyGraph(metas, c.specsDir)
	return concern(spec, graph, afspec.ValidateCrossSpec(specs, graph)), true
}

// concern keeps, of the findings over a whole spec root, the ones about spec.
// The library tags an unknown dependency and a missing shared actor with the
// downstream package, which decides them, so those are kept for it. The cycle
// names no package, and a package with no edge cannot be on one, so it is kept
// for any package with an edge. A glossary conflict is tagged with the term and
// is kept for a package that defines the term.
func concern(spec *afspec.Spec, graph *afspec.DependencyGraph, all afspec.ValidationResult) afspec.ValidationResult {
	linked := len(graph.Dependencies(spec.SpecID)) > 0 || len(graph.Dependents(spec.SpecID)) > 0
	var kept afspec.ValidationResult
	for _, e := range all.Errors {
		if e.EntityID == spec.SpecID || (e.EntityID == "" && linked) {
			kept.Errors = append(kept.Errors, named(e))
		}
	}
	if spec.Requirements != nil {
		for _, w := range all.Warnings {
			if _, defined := spec.Requirements.Glossary[w.EntityID]; defined {
				kept.Warnings = append(kept.Warnings, named(w))
			}
		}
	}
	return kept
}

// glossaryCheck is the rule a glossary conflict is reported under. The library
// gives such a warning no rule of its own, and a verdict names the rule of
// every finding.
const glossaryCheck = "cross_spec_glossary"

// named gives a cross-spec finding the rule and the artifact the library leaves
// off: a glossary conflict has neither, and the dependency cycle, which is made
// of the dependencies that tasks.json declares, has no artifact.
func named(e afspec.ValidationEntry) afspec.ValidationEntry {
	if e.Check == "" && e.Category == "warning" {
		e.Check = glossaryCheck
	}
	if e.Artifact == "" {
		switch e.Check {
		case glossaryCheck:
			e.Artifact = "requirements.json"
		case "cross_spec_cycle":
			e.Artifact = "tasks.json"
		}
	}
	return e
}

// sameDir reports whether two paths are one directory, however they are spelled.
func sameDir(a, b string) bool {
	ia, erra := os.Stat(a)
	ib, errb := os.Stat(b)
	return erra == nil && errb == nil && os.SameFile(ia, ib)
}
