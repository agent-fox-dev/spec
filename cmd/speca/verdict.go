package main

import (
	"github.com/agent-fox-dev/spec/afspec"
	"github.com/agent-fox-dev/spec/envelope"
	"github.com/agent-fox-dev/spec/internal/authoring"
	"github.com/agent-fox-dev/spec/internal/cli"
	"github.com/agent-fox-dev/spec/internal/project"
	"github.com/agent-fox-dev/spec/internal/specpkg"
)

// verdict is the form of speca <spec> --validate: the format's full verdict on
// the package in dir. That is the package's own validation (the schemas and
// the cross-file rules C1 to C8) and the cross-spec rules that concern it,
// which only a run over the whole spec root can apply, and which spec applies
// to every package it writes (specpkg.Verdict). A package that does not
// validate is a successful call: the answer is in valid.
//
// Beside the verdict it reports the authoring hints (internal/authoring):
// the checks spec makes on a package as it generates one, which are not the
// format's rules and so never change valid (ADR 20).
func verdict(c *call, dir string) (*Result, *cli.ErrorInfo) {
	spec, failure := c.loadPackage(dir)
	if failure != nil {
		return nil, failure
	}
	r := &Result{AdminResult: c.identityOf(dir, spec)}

	// The verdict reads the steering's version, so it says when the steering's
	// record is malformed; the full gate says so too, since it reads its budget.
	if msg := project.ReadSteering(c.specsDir).Malformed(c.specsDir); msg != "" {
		c.run.Warn(cli.WarnSteeringMalformed, "high", "%s", msg)
	}

	// A spec root that cannot be read leaves the verdict the package's own, with
	// a warning.
	combined, err := specpkg.Verdict(c.specsDir, spec)
	if err != nil {
		c.warnUnreadable("the spec root", c.specsDir, err, "the cross-spec rules are skipped, and the verdict is the package's own")
	}

	if c.opts.strictHandoffs {
		combined = afspec.StrictHandoffs(combined)
	}
	validation := specpkg.ReportOf(combined)
	r.Validation = &validation
	valid := validation.Valid
	r.Valid = &valid
	traceability := specpkg.TraceOf(spec)
	r.Traceability = &traceability
	r.Hints = []envelope.ValidationItem{}
	for _, h := range authoring.Hints(spec, project.DetectProfile(c.root)) {
		r.Hints = append(r.Hints, specpkg.ItemOf(h))
	}
	return r, nil
}
