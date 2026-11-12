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
// and the cross-file rules C1 to C8), then the cross-spec rules (format v3
// §10.3) that concern it, which only a run over the whole spec root can apply,
// then the warning-only rule steering_version, which reads the spec root's
// steering. spec and speca give this one verdict, whichever of them asks.
//
// siblings are packages that exist in memory only and belong to the spec root
// all the same: the packages of a run that writes nothing, which may include
// spec itself. Each is in the run of the cross-spec rules beside the packages on
// disk, and replaces the one at its directory if there is one. A run that
// writes nothing has not created the spec root either, so when siblings are
// given, a specsDir that does not exist holds no other package.
//
// When specsDir cannot be read the rules across specs and steering_version are
// not applied. The verdict is then spec's own, and err says why.
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
	combined.Warnings = append(combined.Warnings, steeringVersionRule(specsDir, spec)...)
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

	return afspec.CrossSpecFindings(specs, spec), nil
}

// SameDir reports whether two paths are one directory, however they are spelled.
// A path that does not exist is none.
func SameDir(a, b string) bool {
	ia, erra := os.Stat(a)
	ib, errb := os.Stat(b)
	return erra == nil && errb == nil && os.SameFile(ia, ib)
}
