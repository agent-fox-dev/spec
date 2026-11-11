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
	dirs, failure := c.packageDirs("listed")
	if failure != nil {
		return nil, failure
	}
	packages := []envelope.PackageInfo{}
	writable := map[string]bool{}
	for _, d := range dirs {
		info, canWrite := c.entry(d.path, d.archived)
		packages = append(packages, info)
		writable[info.SpecDir] = canWrite
	}
	return &Result{AdminResult: envelope.AdminResult{Packages: packages}, writableDirs: writable}, nil
}

// packageDir is a package directory a form over the spec root goes through,
// and whether it is under archive/.
type packageDir struct {
	path     string
	archived bool
}

// packageDirs is every package directory of the spec root in number order,
// then, with --all, those under archive/. A spec root that cannot be read fails
// the call, since an empty list would hide a mistyped --specs-dir; an archive
// that cannot be read is left out with a warning, saying the form left its
// packages unprocessed (what: "listed", "rendered").
func (c *call) packageDirs(what string) ([]packageDir, *cli.ErrorInfo) {
	names, err := specpkg.List(c.specsDir)
	if err != nil {
		return nil, &cli.ErrorInfo{Stage: "load", Category: "input",
			Message: fmt.Sprintf("the spec root %s could not be read: %v", specpkg.Display(c.specsDir, c.root), err),
			FixHint: &cli.FixHint{Flag: "--specs-dir", Env: []string{afspec.SpecDirEnv}}}
	}
	var dirs []packageDir
	for _, name := range names {
		dirs = append(dirs, packageDir{filepath.Join(c.specsDir, name), false})
	}
	if c.opts.all {
		dirs = append(dirs, c.archiveDirs(what)...)
	}
	return dirs, nil
}

// archiveDirs is every package directory under archive/ in number order. An
// archive that cannot be read is left out with a warning, saying the form left
// its packages unprocessed (what); a root without one is the usual root.
func (c *call) archiveDirs(what string) []packageDir {
	archive := filepath.Join(c.specsDir, "archive")
	archived, err := specpkg.List(archive)
	switch {
	case errors.Is(err, fs.ErrNotExist):
		return nil
	case err != nil:
		c.warnUnreadable("the archive", archive, err, "the packages under archive/ are not "+what)
		return nil
	}
	dirs := make([]packageDir, 0, len(archived))
	for _, name := range archived {
		dirs = append(dirs, packageDir{filepath.Join(archive, name), true})
	}
	return dirs
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
	return packageInfo(info.SpecDir, spec, archived), specpkg.Writable(spec)
}

// packageInfo is the entry of a package that loaded, shown as dir.
func packageInfo(dir string, spec *afspec.Spec, archived bool) envelope.PackageInfo {
	info := envelope.PackageInfo{SpecDir: dir}
	valid := spec.Validate().Valid
	info.SpecID, info.SpecName, info.Title, info.Status = spec.SpecID, spec.SpecName, spec.Title, spec.Status
	info.Valid = &valid
	info.TaskCounts = countTasks(spec)
	if next, ok := spec.NextTask(); ok {
		task := specpkg.TaskInfo(spec, *next)
		info.NextTask = &task
	}
	info.Archived = archived
	return info
}
