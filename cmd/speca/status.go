package main

import (
	"fmt"
	"strings"

	"github.com/agent-fox-dev/spec/afspec"
	"github.com/agent-fox-dev/spec/envelope"
	"github.com/agent-fox-dev/spec/internal/cli"
	"github.com/agent-fox-dev/spec/internal/specpkg"
)

// statusTargets are the statuses --status moves a package to, in the order of
// its life. draft is not one: no move leads back to it.
var statusTargets = []string{
	string(afspec.PrdFrontmatterV2JsonStatusActive),
	string(afspec.PrdFrontmatterV2JsonStatusSealed),
	string(afspec.PrdFrontmatterV2JsonStatusSuperseded),
	string(afspec.PrdFrontmatterV2JsonStatusArchived),
}

// givenFlag is a string flag that remembers it was given. --status "" must be
// refused as a status that is none of the four, not taken for no --status at
// all, which would report the package where a move was asked for.
type givenFlag struct {
	value string
	given bool
}

func (f *givenFlag) String() string { return f.value }

func (f *givenFlag) Set(v string) error {
	f.value, f.given = v, true
	return nil
}

// executeStatus is the status form: speca <spec> --status S [--by <spec>].
//
// It resolves and loads the packages and refuses a call that cannot name two
// different packages, as a usage error, before anything else is looked at.
func executeStatus(c *call, dir string) (*Result, *cli.ErrorInfo) {
	byDir, refusal := c.locateBy(dir)
	if refusal != nil {
		return nil, refusal
	}
	spec, failure := load(dir)
	if failure != nil {
		return nil, failure
	}
	if byDir != "" {
		if refusal := c.checkBy(spec, byDir); refusal != nil {
			return nil, refusal
		}
	}
	return nil, &cli.ErrorInfo{Stage: "status", Category: envelope.CategoryInternal,
		Message: "the --status form is not implemented yet"}
}

// locateBy resolves --by as the input is resolved, and refuses a --by that names
// the package itself. Its refusals are usage errors that begin --by:. It returns
// "" when --by was not given.
func (c *call) locateBy(dir string) (string, *cli.ErrorInfo) {
	ref := strings.TrimSpace(c.opts.by.value)
	if !c.opts.by.given || ref == "" {
		return "", nil
	}
	byDir, err := specpkg.Locate(tool, ref, c.root, c.specsDir)
	if err != nil {
		return "", usageInfo(fmt.Errorf("--by: %w", err), nil)
	}
	if sameDir(dir, byDir) {
		return "", usageInfo(fmt.Errorf("--by: %s is the package itself: name the package that replaces it", specpkg.Display(dir, c.root)), nil)
	}
	return byDir, nil
}

// checkBy loads the replacing package and refuses one that has the spec_id of
// old, the package it would replace: a package is superseded by another one.
// Its load failure is at stage load, naming --by.
func (c *call) checkBy(old *afspec.Spec, byDir string) *cli.ErrorInfo {
	replacing, err := afspec.LoadSpec(byDir)
	if err != nil {
		return &cli.ErrorInfo{Stage: "load", Category: envelope.CategoryInput,
			Message: fmt.Sprintf("the --by package %s could not be read: %v", specpkg.Display(byDir, c.root), err)}
	}
	if replacing.SpecID == old.SpecID {
		return usageInfo(fmt.Errorf("--by: %s has spec_id %s, the same as %s: a package is superseded by another one",
			specpkg.Display(byDir, c.root), replacing.SpecID, specpkg.Display(old.Dir, c.root)), nil)
	}
	return nil
}
