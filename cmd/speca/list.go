package main

import (
	"errors"
	"fmt"
	"io/fs"
	"path/filepath"

	"github.com/agent-fox-dev/spec/afspec"
	"github.com/agent-fox-dev/spec/envelope"
	"github.com/agent-fox-dev/spec/internal/cli"
	"github.com/agent-fox-dev/spec/internal/specpkg"
)

// list is the form of speca --list: every package under the spec root, in the
// order of their numbers, each with its status, whether it validates, its task
// counts and its next task. With --all the packages under archive/ follow.
//
// It lists the directories itself and loads each one, because the library's
// discovery drops a package whose prd.md cannot be read, and such a package
// must still be listed, with the reason. A package that does not load is an
// entry, not a failure of the call.
func list(c *call) (*Result, *cli.ErrorInfo) {
	names, err := specpkg.List(c.specsDir)
	if err != nil {
		// An empty list would hide a mistyped --specs-dir.
		return nil, &cli.ErrorInfo{Stage: "load", Category: "input",
			Message: fmt.Sprintf("the spec root %s could not be read: %v", specpkg.Display(c.specsDir, c.root), err),
			FixHint: &cli.FixHint{Flag: "--specs-dir", Env: []string{afspec.SpecDirEnv}}}
	}
	packages := []envelope.PackageInfo{}
	writable := map[string]bool{}
	add := func(path string, archived bool) {
		info, canWrite := c.entry(path, archived)
		packages = append(packages, info)
		writable[info.SpecDir] = canWrite
	}
	for _, name := range names {
		add(filepath.Join(c.specsDir, name), false)
	}

	if c.opts.all {
		archive := filepath.Join(c.specsDir, "archive")
		archived, err := specpkg.List(archive)
		switch {
		case errors.Is(err, fs.ErrNotExist):
			// A root without an archive is the usual one.
		case err != nil:
			c.warnUnreadable("the archive", archive, err, "the packages under archive/ are not listed")
		default:
			for _, name := range archived {
				add(filepath.Join(archive, name), true)
			}
		}
	}
	return &Result{AdminResult: envelope.AdminResult{Packages: packages}, writableDirs: writable}, nil
}

// entry describes the package in path, and says whether it can take a write,
// which an entry has no field for. One that fails to load is described by its
// directory and the library's reason alone, whether or not it is in the
// archive: where it was is in its path.
func (c *call) entry(path string, archived bool) (info envelope.PackageInfo, writable bool) {
	info = envelope.PackageInfo{SpecDir: specpkg.Display(path, c.root)}
	spec, err := afspec.LoadSpec(path)
	if err != nil {
		info.LoadError = err.Error()
		return info, false
	}
	valid := spec.Validate().Valid
	info.SpecID, info.SpecName, info.Title, info.Status = spec.SpecID, spec.SpecName, spec.Title, spec.Status
	info.Valid = &valid
	info.TaskCounts = countTasks(spec.Tasks)
	if next, ok := spec.Tasks.NextTask(); ok {
		task := specpkg.TaskInfo(spec.Tasks, *next)
		info.NextTask = &task
	}
	info.Archived = archived
	return info, specpkg.Writable(spec)
}
