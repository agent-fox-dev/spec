package main

import (
	"github.com/agent-fox-dev/spec/internal/cli"
	"github.com/agent-fox-dev/spec/internal/specpkg"
)

// verdict is the form of speca <spec> --validate: the format's full verdict on
// the package in dir. That is the package's own validation (the schemas and
// the cross-file rules C1 to C11) and the cross-spec rules that concern it,
// which only a run over the whole spec root can apply, and which spec applies
// to every package it writes (specpkg.Verdict). A package that does not
// validate is a successful call: the answer is in valid.
func verdict(c *call, dir string) (*Result, *cli.ErrorInfo) {
	spec, failure := c.loadPackage(dir)
	if failure != nil {
		return nil, failure
	}
	r := &Result{AdminResult: c.identityOf(dir, spec)}

	// A spec root that cannot be read leaves the verdict the package's own, with
	// a warning.
	combined, err := specpkg.Verdict(c.specsDir, spec)
	if err != nil {
		c.warnUnreadable("the spec root", c.specsDir, err, "the cross-spec rules are skipped, and the verdict is the package's own")
	}

	validation := specpkg.ReportOf(combined)
	r.Validation = &validation
	valid := validation.Valid
	r.Valid = &valid
	traceability := specpkg.TraceOf(spec)
	r.Traceability = &traceability
	return r, nil
}
