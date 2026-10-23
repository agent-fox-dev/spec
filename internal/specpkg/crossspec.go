package specpkg

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"slices"

	"github.com/agent-fox-dev/spec/afspec"
)

// Verdict is the format's full verdict on spec: its own validation (the schemas
// and the cross-file rules C1 to C11), then the cross-spec rules (format v2
// §10.4) that concern it, which only a run over the whole spec root can apply.
// spec and speca give this one verdict, whichever of them asks.
//
// siblings are packages that exist in memory only and belong to the spec root
// all the same: the packages of a run that writes nothing, which may include
// spec itself. Each is in the run of the cross-spec rules beside the packages on
// disk, and replaces the one at its directory if there is one. A run that
// writes nothing has not created the spec root either, so when siblings are
// given, a specsDir that does not exist holds no other package.
//
// When specsDir cannot be read the rules across specs are not applied. The
// verdict is then spec's own, and err says why.
func Verdict(specsDir string, spec *afspec.Spec, siblings ...*afspec.Spec) (afspec.ValidationResult, error) {
	own := spec.Validate()
	combined := afspec.ValidationResult{Errors: slices.Clone(own.Errors), Warnings: slices.Clone(own.Warnings)}
	cross, err := crossSpec(specsDir, spec, siblings)
	if err != nil {
		combined.Valid = len(combined.Errors) == 0
		return combined, err
	}
	combined.Errors = append(combined.Errors, cross.Errors...)
	combined.Warnings = append(combined.Warnings, cross.Warnings...)
	combined.Valid = len(combined.Errors) == 0
	return combined, nil
}

// crossSpec runs the cross-spec rules over every package of specsDir that loads,
// with spec in the place of the package at its directory, or after them when
// there is none (an archived package is not in the spec root), and the
// siblings likewise. It returns the findings that concern spec. A package that
// fails to load is left out. The graph is made of the dependencies the packages
// hold, not of the tasks.json on disk, so a package that is not on disk has its
// edges all the same.
func crossSpec(specsDir string, spec *afspec.Spec, siblings []*afspec.Spec) (afspec.ValidationResult, error) {
	names, err := List(specsDir)
	if errors.Is(err, fs.ErrNotExist) && len(siblings) > 0 {
		names, err = nil, nil
	}
	if err != nil {
		return afspec.ValidationResult{}, err
	}
	held := []*afspec.Spec{spec}
	for _, s := range siblings {
		if s != spec {
			held = append(held, s)
		}
	}
	placed := make([]bool, len(held))
	var specs []*afspec.Spec
	for _, name := range names {
		path := filepath.Join(specsDir, name)
		if i := slices.IndexFunc(held, func(h *afspec.Spec) bool { return SameDir(path, h.Dir) }); i >= 0 {
			specs, placed[i] = append(specs, held[i]), true
		} else if other, err := afspec.LoadSpec(path); err == nil {
			specs = append(specs, other)
		}
	}
	for i, h := range held {
		if !placed[i] {
			specs = append(specs, h)
		}
	}

	graph := &afspec.DependencyGraph{Edges: []afspec.DependencyEdge{}}
	for _, s := range specs {
		if s.Tasks == nil {
			continue
		}
		for _, d := range s.Tasks.Dependencies {
			graph.Edges = append(graph.Edges, afspec.DependencyEdge{FromSpec: s.SpecID, ToSpec: d.Spec, Reason: d.Reason})
		}
	}
	return concern(spec, graph, afspec.ValidateCrossSpec(specs, graph)), nil
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

// SameDir reports whether two paths are one directory, however they are spelled.
// A path that does not exist is none.
func SameDir(a, b string) bool {
	ia, erra := os.Stat(a)
	ib, errb := os.Stat(b)
	return erra == nil && errb == nil && os.SameFile(ia, ib)
}
