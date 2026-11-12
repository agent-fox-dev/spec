package afspec

import "path/filepath"

// CrossSpecFindings runs the cross-spec rules over specs, the packages of
// one spec root, spec among them, and keeps the findings that concern spec.
// The graph is made of the dependencies the packages hold. The library tags
// an unknown dependency, a missing shared actor and the handoff rules with
// the package that decides them, so those are kept for it. The cycle names no
// package, and a package with no edge cannot be on one, so it is kept for any
// package with an edge. A warning tagged with the package's id is kept for
// it.
func CrossSpecFindings(specs []*Spec, spec *Spec) ValidationResult {
	graph := &DependencyGraph{Edges: []DependencyEdge{}}
	for _, s := range specs {
		if s.Tasks == nil {
			continue
		}
		for _, d := range s.Tasks.Dependencies {
			graph.Edges = append(graph.Edges, DependencyEdge{FromSpec: s.SpecID, ToSpec: d.Spec, Reason: d.Reason})
		}
	}
	all := ValidateCrossSpec(specs, graph)
	linked := len(graph.Dependencies(spec.SpecID)) > 0 || len(graph.Dependents(spec.SpecID)) > 0
	var kept ValidationResult
	for _, e := range all.Errors {
		if e.EntityID == spec.SpecID || (e.EntityID == "" && linked) {
			// The dependency cycle is made of the dependencies tasks.json
			// declares, and the rule names no artifact for it.
			if e.Artifact == "" && e.Check == "cross_spec_cycle" {
				e.Artifact = TasksFileName
			}
			kept.Errors = append(kept.Errors, e)
		}
	}
	for _, w := range all.Warnings {
		if w.EntityID == spec.SpecID {
			kept.Warnings = append(kept.Warnings, w)
		}
	}
	kept.Valid = len(kept.Errors) == 0
	return kept
}

// ValidateInRoot is spec's own validation and the cross-spec rules that
// concern it, over the packages under specsDir that load (spec in place of
// the one at its directory), for a caller that needs the verdict without
// the commands that give it. It fails only when specsDir cannot be read.
func ValidateInRoot(specsDir string, spec *Spec) (ValidationResult, error) {
	names, err := ListSpecDirs(specsDir)
	if err != nil {
		return ValidationResult{}, err
	}
	specs := []*Spec{spec}
	for _, name := range names {
		dir := filepath.Join(specsDir, name)
		if spec.Dir != "" && sameDir(dir, spec.Dir) {
			continue
		}
		if other, err := LoadSpec(dir); err == nil {
			specs = append(specs, other)
		}
	}
	own := spec.Validate()
	cross := CrossSpecFindings(specs, spec)
	v := ValidationResult{Errors: append(own.Errors, cross.Errors...), Warnings: append(own.Warnings, cross.Warnings...)}
	v.Valid = len(v.Errors) == 0
	return v, nil
}
