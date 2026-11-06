package main

import (
	"fmt"

	"github.com/agent-fox-dev/spec/afspec"
	"github.com/agent-fox-dev/spec/envelope"
	"github.com/agent-fox-dev/spec/internal/cli"
	"github.com/agent-fox-dev/spec/internal/specpkg"
)

// The three outcomes of migrating one package.
const (
	migratedWritten   = "written"
	migratedUnchanged = "unchanged"
	migratedFailed    = "failed"
)

// migrateOne is the form of speca <spec> --migrate: it converts a version 2
// package to version 3 in place, by the rules of format v3 §13, whatever its
// status, and leaves a version 3 package alone. A package that cannot be read
// as either, that has a gap the conversion refuses, or that cannot be written
// fails the call.
func migrateOne(c *call, dir string) (*Result, *cli.ErrorInfo) {
	info := c.migrateEntry(packageDir{path: dir})
	if info.Migrated == migratedFailed {
		return nil, &cli.ErrorInfo{Stage: "migrate", Category: envelope.CategoryInput, Message: info.Reason}
	}
	return &Result{AdminResult: envelope.AdminResult{Migrated: []envelope.MigratedInfo{info}, DryRun: c.opts.dryRun}, migrateForm: true}, nil
}

// migrateAll is the form of speca --migrate with no package, the sweep: every
// package of the spec root in number order, then with --all those under
// archive/, each converted as the one-package form converts it. A package that
// cannot be converted is an entry with its reason, not a failure of the call,
// so that one broken package does not keep the others from being converted.
func migrateAll(c *call) (*Result, *cli.ErrorInfo) {
	dirs, failure := c.packageDirs("migrated")
	if failure != nil {
		return nil, failure
	}
	migrated := []envelope.MigratedInfo{}
	for _, d := range dirs {
		migrated = append(migrated, c.migrateEntry(d))
	}
	return &Result{AdminResult: envelope.AdminResult{Migrated: migrated, DryRun: c.opts.dryRun}, migrateForm: true}, nil
}

// migrateEntry converts the package in d, or says why it could not, and
// describes the outcome. The package's version decides what is done: a
// version 3 package is unchanged, a version 2 package is converted (under
// --dry-run, the conversion is made in memory and nothing is written), and
// anything else fails with the library's reason.
func (c *call) migrateEntry(d packageDir) envelope.MigratedInfo {
	info := envelope.MigratedInfo{SpecDir: specpkg.Display(d.path, c.root), Archived: d.archived}
	// The library loads a package of any version it can parse; the version
	// 3 types read what the versions share, which is the identity needed here.
	spec, err := afspec.LoadSpec(d.path)
	if err != nil {
		info.Migrated, info.Reason = migratedFailed, err.Error()
		return info
	}
	info.SpecID, info.SpecName, info.Status, info.FromVersion = spec.SpecID, spec.SpecName, spec.Status, spec.SchemaVersion
	if spec.SchemaVersion == afspec.SchemaVersion {
		info.Migrated = migratedUnchanged
		return info
	}
	if c.opts.dryRun {
		if _, err := afspec.LoadV2(d.path); err != nil {
			info.Migrated, info.Reason = migratedFailed, err.Error()
			return info
		}
		info.Migrated = migratedWritten
		return info
	}
	if err := afspec.MigrateV2(d.path); err != nil {
		info.Migrated, info.Reason = migratedFailed, fmt.Sprintf("%s could not be migrated: %v", info.SpecDir, err)
		return info
	}
	info.Migrated = migratedWritten
	return info
}
